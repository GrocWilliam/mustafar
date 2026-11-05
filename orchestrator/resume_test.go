package main

import (
	"encoding/json"
	"testing"
)

func TestTesterRun(t *testing.T) {
	// author:sha:parent, newest first
	branch := func(spec ...[3]string) []branchCommit {
		var commits []branchCommit
		for _, c := range spec {
			raw := `{"sha":"` + c[1] + `","commit":{"author":{"name":"` + c[0] + `"}},"parents":[{"sha":"` + c[2] + `"}]}`
			var bc branchCommit
			if err := json.Unmarshal([]byte(raw), &bc); err != nil {
				t.Fatal(err)
			}
			commits = append(commits, bc)
		}
		return commits
	}
	dev, tester, retester, main := "forge-agent-dev", testerAuthor, retesterAuthor, "someone"
	cases := []struct {
		name         string
		commits      []branchCommit
		tester, base string
		revised, ok  bool
	}{
		{"dev work after two tester commits", branch([3]string{dev, "d2", "d1"}, [3]string{dev, "d1", "t2"}, [3]string{tester, "t2", "t1"}, [3]string{tester, "t1", "m1"}, [3]string{main, "m1", "m0"}), "t2", "m1", false, true},
		{"revision after dev work: its tests, the first run's base", branch([3]string{dev, "d2", "r1"}, [3]string{retester, "r1", "d1"}, [3]string{dev, "d1", "t1"}, [3]string{tester, "t1", "m1"}, [3]string{main, "m1", "m0"}), "r1", "m1", true, true},
		{"tester only: start over", branch([3]string{tester, "t1", "m1"}, [3]string{main, "m1", "m0"}), "", "", false, false},
		{"no tester commit", branch([3]string{dev, "d1", "m1"}, [3]string{main, "m1", "m0"}), "", "", false, false},
		{"empty branch", nil, "", "", false, false},
	}
	for _, c := range cases {
		gotTester, gotBase, revised, ok := testerRun(c.commits)
		if gotTester != c.tester || gotBase != c.base || revised != c.revised || ok != c.ok {
			t.Errorf("%s: testerRun = %q, %q, %v, %v; want %q, %q, %v, %v", c.name, gotTester, gotBase, revised, ok, c.tester, c.base, c.revised, c.ok)
		}
	}
}
