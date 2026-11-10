package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

var (
	sha  = strings.Repeat("a", 40)
	head = reply(map[string]any{"commit": map[string]string{"id": sha}})
)

func TestTargetOf(t *testing.T) {
	f := &fakes{routes: map[string]func(http.ResponseWriter){
		"GET /issues/3":           reply(issue{Number: 3, State: "open"}),
		"GET /issues/4":           reply(issue{Number: 4, State: "closed"}),
		"GET /branch_protections": reply([]any{}),
	}}
	p := newFakeForge(t, f)
	for body, want := range map[string]string{
		"no spec":    "main",
		"Part of #3": "spec/3",
		"Part of #4": "main",
	} {
		if got, err := p.targetOf(issue{Body: body}); err != nil || got != want {
			t.Errorf("targetOf(%q) = %q, %v, want %q", body, got, err, want)
		}
	}
	if !slices.Contains(f.calls, "POST /branch_protections") || strings.Count(strings.Join(f.calls, "\n"), "POST /branches") != 1 {
		t.Fatalf("the spec branch must be created once and protected: %q", f.calls)
	}
}

// A story of a spec merges into the spec branch: main and staging do not move until the spec lands.
func TestLandStoryOnSpecBranch(t *testing.T) {
	f := &fakes{routes: map[string]func(http.ResponseWriter){"GET /branches/spec/3": head}}
	p := newFakeForge(t, f)
	feedback, err := p.land(issue{Number: 7, base: "spec/3"}, &pullRequest{Number: 5}, "agent/us-7", "h")
	if err != nil || feedback != "" {
		t.Fatalf("land = %q, %v", feedback, err)
	}
	if !slices.Contains(f.calls, "GET /compare/agent/us-7...spec/3") || !slices.Contains(f.calls, "POST /pulls/5/merge") || !slices.Contains(f.calls, "PATCH /issues/7") {
		t.Fatalf("calls = %q", f.calls)
	}
	if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "GET /archive") || strings.Contains(c, "/containers") }) {
		t.Fatalf("a story's merge deployed: %q", f.calls)
	}
}

// sequence answers each call with the next reply, then keeps repeating the last one.
func sequence(replies ...func(http.ResponseWriter)) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		replies[0](w)
		if len(replies) > 1 {
			replies = replies[1:]
		}
	}
}

func specRoutes(ahead, behind, integratorExit int) map[string]func(http.ResponseWriter) {
	return map[string]func(http.ResponseWriter){
		"GET /branches/spec/3":             head,
		"GET /branches/agent/spec-3":       head,
		"GET /branches/main":               head,
		"GET /compare/main...spec/3":       reply(map[string]int{"total_commits": ahead}),
		"GET /compare/agent/spec-3...main": reply(map[string]int{"total_commits": behind}),
		"GET /commits/" + sha + "/status":  reply(map[string]string{"state": "success"}),
		"POST /pulls":                      reply(map[string]any{"number": 9}),
		"GET /pulls":                       reply([]any{}),
		"GET /issues/3/comments":           reply([]any{}),
		"POST /containers/create":          reply(map[string]string{"Id": "c1"}),
		"POST /containers/c1/wait":         reply(map[string]int{"StatusCode": integratorExit}),
		"GET /containers/c1/logs":          func(http.ResponseWriter) {},
	}
}

func TestLandSpecMergesThenDeletesBranch(t *testing.T) {
	prSettle = 0
	f := &fakes{routes: specRoutes(2, 0, 0)}
	p := newFakeForge(t, f)
	if err := p.landSpec(issue{Number: 3, Title: "Spec"}, 2); err != nil {
		t.Fatal(err)
	}
	merged, closed := slices.Index(f.calls, "POST /pulls/9/merge"), slices.Index(f.calls, "PATCH /issues/3")
	unprotected, deleted := slices.Index(f.calls, "DELETE /branch_protections/spec/3"), slices.Index(f.calls, "DELETE /branches/spec/3")
	if merged < 0 || closed < merged || unprotected < closed || deleted < unprotected {
		t.Fatalf("want merge, close, unprotect, delete in order: %q", f.calls)
	}
	if slices.Contains(f.calls, "POST /containers/create") {
		t.Fatalf("the integrator ran with nothing to fix: %q", f.calls)
	}
}

// A red full suite sends the integrator, and the spec lands once CI is green on its work.
func TestLandSpecIntegratorFixesRedSuite(t *testing.T) {
	prSettle = 0
	routes := specRoutes(2, 0, 0)
	routes["GET /commits/"+sha+"/status"] = sequence(reply(map[string]string{"state": "failure"}), reply(map[string]string{"state": "success"}))
	routes["GET /commits/"+sha+"/statuses"] = reply([]any{})
	f := &fakes{routes: routes}
	p := newFakeForge(t, f)
	if err := p.landSpec(issue{Number: 3, Title: "Spec"}, 2); err != nil {
		t.Fatal(err)
	}
	integrator, merged := slices.Index(f.calls, "POST /containers/create"), slices.Index(f.calls, "POST /pulls/9/merge")
	if integrator < 0 || merged < integrator {
		t.Fatalf("want the integrator, then the merge: %q", f.calls)
	}
}

// A conflict the integrator cannot resolve blocks the spec and keeps its branch for the retry.
func TestLandSpecConflictSendsIntegratorThenBlocks(t *testing.T) {
	prSettle = 0
	routes := specRoutes(2, 1, 1)
	routes["POST /pulls/9/update"] = func(w http.ResponseWriter) { w.WriteHeader(http.StatusConflict) }
	f := &fakes{routes: routes}
	p := newFakeForge(t, f)
	if err := p.landSpec(issue{Number: 3, Title: "Spec"}, 2); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.calls, "POST /containers/create") {
		t.Fatalf("no integrator: %q", f.calls)
	}
	if slices.Contains(f.calls, "POST /pulls/9/merge") || slices.Contains(f.calls, "PATCH /issues/3") || slices.Contains(f.calls, "DELETE /branches/spec/3") {
		t.Fatalf("a conflicting spec landed: %q", f.calls)
	}
	if !slices.Contains(f.calls, "POST /issues/3/labels") {
		t.Fatalf("not blocked: %q", f.calls)
	}
}

// An integrator that makes a test pass by rewriting it blocks the spec.
func TestIntegrateRejectsEditedTests(t *testing.T) {
	test := func(blob string) func(http.ResponseWriter) {
		return reply(map[string]any{"tree": []map[string]string{{"path": "tests/acceptance/us-1/run.sh", "type": "blob", "sha": blob}}})
	}
	routes := specRoutes(2, 0, 0)
	routes["GET /git/trees/"+sha] = sequence(test("old"), test("edited"), test("old"), test("old"))
	f := &fakes{routes: routes}
	p := newFakeForge(t, f)
	err := p.integrate(issue{Number: 3, base: "main"}, "spec/3", "agent/spec-3", "red")
	if err == nil || !strings.Contains(err.Error(), "frozen or protected") {
		t.Fatalf("integrate = %v", err)
	}
}

// A spec with nothing on its branch has nothing to merge: Gitea refuses a pull request without commits.
func TestLandSpecWithoutCommitsJustCloses(t *testing.T) {
	f := &fakes{routes: specRoutes(0, 0, 0)}
	p := newFakeForge(t, f)
	if err := p.landSpec(issue{Number: 3}, 0); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.calls, "POST /pulls") || !slices.Contains(f.calls, "PATCH /issues/3") {
		t.Fatalf("calls = %q", f.calls)
	}
}

// A story added to a spec keeps its branch alive until the story is done.
func TestSpecWaitsForAddedStory(t *testing.T) {
	f := &fakes{routes: map[string]func(http.ResponseWriter){"GET /issues/3/comments": reply([]any{})}}
	f.routes["GET /issues"] = reply([]issue{{Number: 8, Body: "Part of #3"}})
	p := newFakeForge(t, f)
	if finished, _, err := p.specFinished(3); err != nil || finished {
		t.Fatalf("specFinished = %v, %v", finished, err)
	}
	f.routes["GET /issues"] = reply([]issue{{Number: 8, Body: "Part of #4"}})
	if finished, _, err := p.specFinished(3); err != nil || !finished {
		t.Fatalf("another spec's story: specFinished = %v, %v", finished, err)
	}
}
