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
	dev, tester, main := "forge-agent-dev", testerAuthor, "someone"
	cases := []struct {
		name         string
		commits      []branchCommit
		tester, base string
		ok           bool
	}{
		{"dev work after two tester commits", branch([3]string{dev, "d2", "d1"}, [3]string{dev, "d1", "t2"}, [3]string{tester, "t2", "t1"}, [3]string{tester, "t1", "m1"}, [3]string{main, "m1", "m0"}), "t2", "m1", true},
		{"tester only: start over", branch([3]string{tester, "t1", "m1"}, [3]string{main, "m1", "m0"}), "", "", false},
		{"no tester commit", branch([3]string{dev, "d1", "m1"}, [3]string{main, "m1", "m0"}), "", "", false},
		{"empty branch", nil, "", "", false},
	}
	for _, c := range cases {
		gotTester, gotBase, ok := testerRun(c.commits)
		if gotTester != c.tester || gotBase != c.base || ok != c.ok {
			t.Errorf("%s: testerRun = %q, %q, %v; want %q, %q, %v", c.name, gotTester, gotBase, ok, c.tester, c.base, c.ok)
		}
	}
}
