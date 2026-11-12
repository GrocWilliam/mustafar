package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestChanged(t *testing.T) {
	before := map[string]string{"a.go": "1", "tests/acceptance/us-1/run.sh": "2", "tests/acceptance/us-1/old.py": "3"}
	after := map[string]string{"a.go": "9", "tests/acceptance/us-1/run.sh": "2", "tests/acceptance/us-1/new.py": "4"}

	if got, want := changed(before, after, ""), []string{"a.go", "tests/acceptance/us-1/new.py", "tests/acceptance/us-1/old.py"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed all = %q, want %q", got, want)
	}
	if got, want := changed(before, after, "tests/acceptance/"), []string{"tests/acceptance/us-1/new.py", "tests/acceptance/us-1/old.py"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed acceptance = %q, want %q", got, want)
	}
	if got := changed(before, before, ""); got != nil {
		t.Fatalf("identical trees: %q", got)
	}
}

func TestDrifted(t *testing.T) {
	tester := map[string]string{"tests/acceptance/us-2/run.sh": "t", "tests/acceptance/us-1/run.sh": "1"}
	main := map[string]string{"tests/acceptance/us-1/run.sh": "1b", "tests/acceptance/us-3/run.sh": "3"}
	// Merging main into the branch brings its tests along: each file matches the tester's or main's version.
	merged := map[string]string{"tests/acceptance/us-2/run.sh": "t", "tests/acceptance/us-1/run.sh": "1b", "tests/acceptance/us-3/run.sh": "3"}
	if got := drifted(merged, "tests/acceptance/", tester, main); got != nil {
		t.Fatalf("merged main: %q", got)
	}
	edited := map[string]string{"tests/acceptance/us-1/run.sh": "x", "src.go": "y"}
	if got, want := drifted(edited, "tests/acceptance/", tester, main), []string{"tests/acceptance/us-1/run.sh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("edited = %q, want %q", got, want)
	}
	// The story's own tests must match the tester's commit only: deleting them matches main, which lacks them.
	if got, want := drifted(map[string]string{}, "tests/acceptance/us-2/", tester), []string{"tests/acceptance/us-2/run.sh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("own tests deleted = %q, want %q", got, want)
	}
}

func TestFrozen(t *testing.T) {
	base := map[string]string{"tests/acceptance/us-7/run.sh": "en-fr"}
	// The tester of us-9 wrote its own tests and updated us-7's, which us-9 supersedes.
	tester := map[string]string{"tests/acceptance/us-7/run.sh": "en-es-fr", "tests/acceptance/us-9/run.sh": "es"}
	main := map[string]string{"tests/acceptance/us-7/run.sh": "en-fr", "tests/acceptance/us-8/run.sh": "err"}
	cases := map[string]struct {
		head map[string]string
		want []string
	}{
		"untouched":            {tester, nil},
		"main merged in":       {map[string]string{"tests/acceptance/us-7/run.sh": "en-es-fr", "tests/acceptance/us-9/run.sh": "es", "tests/acceptance/us-8/run.sh": "err"}, nil},
		"tester update undone": {map[string]string{"tests/acceptance/us-7/run.sh": "en-fr", "tests/acceptance/us-9/run.sh": "es"}, []string{"tests/acceptance/us-7/run.sh"}},
		"own test deleted":     {map[string]string{"tests/acceptance/us-7/run.sh": "en-es-fr"}, []string{"tests/acceptance/us-9/run.sh"}},
		"support layer edited": {map[string]string{"tests/acceptance/us-7/run.sh": "en-es-fr", "tests/acceptance/us-9/run.sh": "es", "tests/acceptance/support/sheet.mjs": "x"}, nil},
		"new test file":        {map[string]string{"tests/acceptance/us-7/run.sh": "en-es-fr", "tests/acceptance/us-9/run.sh": "es", "tests/acceptance/us-9/skip.py": "x"}, []string{"tests/acceptance/us-9/skip.py"}},
	}
	for name, c := range cases {
		if got := frozen(base, tester, main, c.head); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: frozen = %q, want %q", name, got, c.want)
		}
	}
}

// A story merged meanwhile edited a test this story's tester had edited too: merging main brings both edits in.
func TestFrozenBothEdited(t *testing.T) {
	base := map[string]string{"tests/acceptance/us-7/run.sh": "a"}
	tester := map[string]string{"tests/acceptance/us-7/run.sh": "a-tester"}
	main := map[string]string{"tests/acceptance/us-7/run.sh": "a-main"}
	if got := frozen(base, tester, main, map[string]string{"tests/acceptance/us-7/run.sh": "a-tester-main"}); got != nil {
		t.Errorf("merged both edits: frozen = %q, want none", got)
	}
	if got := frozen(base, tester, main, map[string]string{}); !reflect.DeepEqual(got, []string{"tests/acceptance/us-7/run.sh"}) {
		t.Errorf("deleted: frozen = %q, want it flagged", got)
	}
	if got := frozen(base, tester, base, map[string]string{"tests/acceptance/us-7/run.sh": "a-dev"}); !reflect.DeepEqual(got, []string{"tests/acceptance/us-7/run.sh"}) {
		t.Errorf("main untouched, dev edit: frozen = %q, want it flagged", got)
	}
}

func TestOutside(t *testing.T) {
	got := outside([]string{"tests/acceptance/us-2/run.sh", "tests/acceptance/us-1/run.sh", "Makefile"}, "tests/acceptance/us-2/")
	if want := []string{"tests/acceptance/us-1/run.sh", "Makefile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("outside = %q, want %q", got, want)
	}
}

func TestVerdict(t *testing.T) {
	cases := []struct {
		review string
		want   bool
	}{
		{"Looks good.\n\nVERDICT: approve", true},
		{"- fix x\n**VERDICT: changes**", false},
		{"VERDICT: approve\nwait, no.\nVERDICT: changes", false}, // the last verdict wins
		{"I would approve this.", false},                         // no explicit verdict line
		{"verdict: APPROVE", true},
	}
	for _, c := range cases {
		if got, _ := verdict(c.review); got != c.want {
			t.Errorf("verdict(%q) = %v, want %v", c.review, got, c.want)
		}
	}
}

func TestConflictReport(t *testing.T) {
	final := "Done.\n\nTEST CONFLICT: tests/acceptance/us-50/a.mjs: selector too broad\n- **TEST CONFLICT:** us-4: SHAPE lacks domains\nNot a TEST CONFLICT: line"
	got := conflictReport(final)
	want := "TEST CONFLICT: tests/acceptance/us-50/a.mjs: selector too broad\n- **TEST CONFLICT:** us-4: SHAPE lacks domains\n\nThe dev's summary:"
	if !strings.HasPrefix(got, want) {
		t.Errorf("conflictReport = %q, want prefix %q", got, want)
	}
	if conflictLine.MatchString("I saw no TEST CONFLICT: here") {
		t.Error("a mid-line mention must not count as a report")
	}
}

func TestNext(t *testing.T) {
	ci := ciFailed + "failure` on your last commit"
	cases := []struct {
		name, final, feedback string
		committed             bool
		revisions, want       int
	}{
		{"first run, nothing to add", "done", "", false, 0, goOn},
		{"fixed after CI", "fixed", ci, true, 0, goOn},
		{"reported conflict", "TEST CONFLICT: us-4: keys", "", true, 0, revise},
		{"stuck without the line", "us-4 pins the keys, I cannot edit it", ci, false, 0, revise},
		{"stuck, revisions left", "flaky", ci, false, maxRevisions - 1, revise},
		{"stuck, revisions used", "flaky", ci, false, maxRevisions, stuck},
		{"reported, revisions used, committed", "TEST CONFLICT: x", ci, true, maxRevisions, goOn},
		{"no commit after a review", "nothing to change", "The reviewer requested changes", false, 0, goOn},
	}
	for _, c := range cases {
		if got := next(c.final, c.feedback, c.committed, c.revisions); got != c.want {
			t.Errorf("%s: next = %d, want %d", c.name, got, c.want)
		}
	}
}
