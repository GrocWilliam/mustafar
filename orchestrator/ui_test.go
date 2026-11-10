package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProgressOf(t *testing.T) {
	tester := "🤖 Agent `tester`: ✅ finished\n\nSteps: 6"
	devOK := "🤖 Agent `dev`: ✅ finished\n\nSteps: 5"
	devFail := "🤖 Agent `dev`: ❌ failed (exit code 1)"
	cases := []struct {
		name     string
		comments []string
		state    string
		labels   []string
		want     storyProgress
	}{
		{"queued, never started", nil, "open", []string{"us-todo"}, storyProgress{Step: 0, Attempt: 1}},
		{"tester working", []string{"🤖 Agent `tester` working on #3 (2 steps)"}, "open", []string{"us-in-progress"}, storyProgress{Step: 1, Attempt: 1}},
		{"dev starts after the tester", []string{tester}, "open", []string{"us-in-progress"}, storyProgress{Step: 2, Attempt: 1}},
		{"waiting for CI", []string{tester, devOK, "🔀 Pull request opened: x"}, "open", []string{"us-review"}, storyProgress{Step: 3, Attempt: 1}},
		{"second attempt", []string{tester, devOK, "🔀 Pull request opened: x", "🔁 Attempt 2/3. Feedback"}, "open", []string{"us-review"}, storyProgress{Step: 2, Attempt: 2}},
		{"review", []string{tester, devOK, "🤖 Agent `reviewer` working on #3"}, "open", []string{"us-review"}, storyProgress{Step: 4, Attempt: 1}},
		{"merging", []string{tester, devOK, "🤖 Agent `reviewer`: ✅ finished", "🔄 `main` moved"}, "open", []string{"us-review"}, storyProgress{Step: 5, Attempt: 1}},
		{"blocked", []string{tester, devFail, "🔁 Attempt 3/3. x", devFail, "⛔ Blocked: still rejected after 3 attempts. Last feedback:\n\nmore"}, "open", []string{"us-blocked"},
			storyProgress{Step: 2, Attempt: 3, Blocked: "still rejected after 3 attempts. Last feedback:"}},
		{"retried: back in the queue", []string{tester, "⛔ Blocked: x"}, "open", []string{"us-todo"}, storyProgress{Step: 0, Attempt: 1}},
		{"resumed after a retry", []string{tester, "⛔ Blocked: x", "♻️ Resuming `agent/us-3`"}, "open", []string{"us-in-progress"}, storyProgress{Step: 2, Attempt: 1}},
		{"merged", []string{tester, devOK, "✅ CI green and review approved"}, "closed", nil, storyProgress{Step: 6, Attempt: 1}},
	}
	for _, c := range cases {
		if got := progressOf(c.comments, c.state, c.labels); got != c.want {
			t.Errorf("%s: progressOf = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestDeliveryOf(t *testing.T) {
	started := "🔀 All stories are merged, running every acceptance test before `main`: x"
	cases := []struct {
		name        string
		comments    []string
		state       string
		step, fixes int
	}{
		{"stories running", []string{"📋 Plan"}, "open", 0, 0},
		{"full suite", []string{"📋 Plan", started}, "open", 1, 0},
		{"integrator", []string{started, "🔁 Integrator run 1/3, for:", "🔁 Integrator run 2/3, for:"}, "open", 1, 2},
		{"main moved", []string{started, "🔄 `main` moved by 2 commit(s)"}, "open", 2, 0},
		{"retried: fresh delivery", []string{started, "🔁 Integrator run 1/3", "⛔ Blocked: x", "🔁 Retried from the forge UI", started}, "open", 1, 0},
		{"merged", []string{started, "✅ CI green and review approved"}, "open", 3, 0},
		{"closed", []string{"📋 Plan"}, "closed", 3, 0},
	}
	for _, c := range cases {
		if step, fixes := deliveryOf(c.comments, c.state); step != c.step || fixes != c.fixes {
			t.Errorf("%s: deliveryOf = %d, %d, want %d, %d", c.name, step, fixes, c.step, c.fixes)
		}
	}
}

func TestSpecBudget(t *testing.T) {
	comments := []string{"📋 Plan", "💰 Budget set to $30.00 " + budgetMarkerFor(30), "💰 " + budgetMarkerFor(45.5), "🤖 cost <!-- forge:cost=1.2 -->"}
	if got := specBudget(comments, 20); got != 45.5 {
		t.Errorf("specBudget = %v, want 45.5 (the last marker)", got)
	}
	if got := specBudget(comments[:1], 20); got != 20 {
		t.Errorf("specBudget without marker = %v, want the default 20", got)
	}
}

func TestActionNeedsHeader(t *testing.T) {
	f := &forge{projects: map[string]*project{}}
	called := false
	h := f.action(func(*project, actionInput) (string, error) { called = true; return "", nil })
	for _, header := range []string{"", "1"} {
		req := httptest.NewRequest("POST", "/api/retry", strings.NewReader(`{"project":"x/y","number":1}`))
		if header != "" {
			req.Header.Set("X-Forge-UI", header)
		}
		rec := httptest.NewRecorder()
		h(rec, req)
		want := http.StatusForbidden // a cross-site form post cannot set the header
		if header != "" {
			want = http.StatusNotFound // header accepted; the project is unknown
		}
		if rec.Code != want {
			t.Errorf("X-Forge-UI=%q: status %d, want %d", header, rec.Code, want)
		}
	}
	if called {
		t.Error("the action ran without a known project")
	}
}

func TestStoryBody(t *testing.T) {
	ok := "Show traits.\n\nAcceptance criteria:\n- GET returns traits"
	got, err := storyBody(" Traits ", ok+"\n", 91)
	if err != nil || got != ok+"\n\n---\nPart of #91" {
		t.Fatalf("storyBody = %q, %v", got, err)
	}
	for name, c := range map[string][2]string{
		"no title":      {"", ok},
		"no criteria":   {"Traits", "Show traits."},
		"own Part of":   {"Traits", ok + "\n\nPart of #3"},
		"title too big": {strings.Repeat("x", 201), ok},
	} {
		if _, err := storyBody(c[0], c[1], 91); !errors.Is(err, errBadRequest) {
			t.Errorf("%s: err = %v, want a refusal", name, err)
		}
	}
}
