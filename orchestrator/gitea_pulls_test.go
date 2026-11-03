package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailedCILog(t *testing.T) {
	log := "2026-11-03T08:03:42.6928503Z == tests/acceptance/us-15/run.sh\n" +
		"2026-11-03T08:03:42.6984197Z \x1b[31mError: listen EADDRINUSE\x1b[0m\n" +
		"2026-11-03T08:03:42.7122288Z exitcode '1': failure\n" +
		"2026-11-03T08:03:42.9348984Z   ✅  Success - Post actions/checkout@v4\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token t" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/repos/o/r/commits/abc/statuses":
			// Gitea keeps every status update: the failed job appears twice, the green one is skipped.
			fmt.Fprint(w, `[{"status":"failure","context":"ci / acceptance (1)","target_url":"/o/r/actions/runs/7/jobs/2"},
				{"status":"pending","context":"ci / acceptance (1)","target_url":"/o/r/actions/runs/7/jobs/2"},
				{"status":"failure","context":"ci / acceptance (1)","target_url":"/o/r/actions/runs/7/jobs/2"},
				{"status":"success","context":"ci / test","target_url":"/o/r/actions/runs/7/jobs/0"}]`)
		case "/o/r/actions/runs/7/jobs/2/logs":
			fmt.Fprint(w, log)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := newGitea(config{GiteaURL: srv.URL, GiteaToken: "t"}, "/repos/o/r")
	got, err := g.failedCILog("abc", 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := "ci / acceptance (1):\nError: listen EADDRINUSE\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, _ := g.failedCILog("abc", 60); !strings.Contains(got, "== tests/acceptance/us-15/run.sh") || strings.Contains(got, "Post actions") {
		t.Errorf("60 lines: got %q", got)
	}
}
