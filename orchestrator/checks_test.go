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
		"new test file":        {map[string]string{"tests/acceptance/us-7/run.sh": "en-es-fr", "tests/acceptance/us-9/run.sh": "es", "tests/acceptance/us-9/skip.py": "x"}, []string{"tests/acceptance/us-9/skip.py"}},
	}
	for name, c := range cases {
		if got := frozen(base, tester, main, c.head); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: frozen = %q, want %q", name, got, c.want)
		}
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
