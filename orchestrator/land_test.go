package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestLoadConfigProtectedPaths(t *testing.T) {
	t.Setenv("GITEA_TOKEN", "x")
	t.Setenv("PROTECTED_PATHS", ".gitea/, Makefile ,,")
	c, err := loadConfig()
	if err != nil || !slices.Equal(c.Protected, []string{".gitea/", "Makefile"}) {
		t.Fatalf("Protected = %q, %v", c.Protected, err)
	}
}

// fakes serves canned Gitea and Docker answers and records every call as "METHOD /path".
type fakes struct {
	mu     sync.Mutex
	calls  []string
	routes map[string]func(w http.ResponseWriter) // by "METHOD /path"; unknown calls answer 200 {}
}

func (f *fakes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1/repos/o/r")
	f.mu.Lock()
	f.calls = append(f.calls, key)
	route := f.routes[key]
	f.mu.Unlock()
	if route == nil {
		w.Write([]byte("{}"))
		return
	}
	route(w)
}

func reply(v any) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { json.NewEncoder(w).Encode(v) }
}

// newFakeForge returns a project wired to a fake Gitea and a fake Docker Engine on a Unix socket.
func newFakeForge(t *testing.T, f *fakes) *project {
	gitea := httptest.NewServer(f)
	t.Cleanup(gitea.Close)
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	docker := httptest.NewUnstartedServer(f)
	docker.Listener = listener
	docker.Start()
	t.Cleanup(docker.Close)
	key := filepath.Join(dir, "deploy_key")
	os.WriteFile(key, []byte("key"), 0o600)

	cfg := config{GiteaURL: gitea.URL, GiteaToken: "t", DeployKeyFile: key}
	fg := &forge{cfg: cfg, docker: newDocker(listener.Addr().String()), running: map[string]bool{}}
	p := newProject(fg, repoInfo{ID: 1, FullName: "o/r"})
	p.base = "main"
	return p
}

// A merge whose deployment fails is reverted through a pull request (merged once Gitea stops answering 405),
// the previous main is redeployed, and the US is reopened.
func TestLandRollsBackFailedDeploy(t *testing.T) {
	mergeRetry = 0
	previous, merged := strings.Repeat("a", 40), strings.Repeat("b", 40)
	mainHeads := []string{previous, merged}
	revertTries := 0
	f := &fakes{}
	f.routes = map[string]func(http.ResponseWriter){
		"GET /compare/agent/us-1...main": reply(map[string]int{"total_commits": 0}),
		"GET /branches/main": func(w http.ResponseWriter) {
			reply(map[string]any{"commit": map[string]string{"id": mainHeads[0]}})(w)
			mainHeads = mainHeads[1:]
		},
		"GET /git/trees/" + merged:           reply(map[string]any{"tree": []map[string]string{{"path": "Dockerfile", "type": "blob", "sha": "d"}}}),
		"GET /archive/" + merged + ".tar.gz": func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) },
		"GET /git/trees/" + previous:         reply(map[string]any{"tree": []map[string]string{}}), // no Dockerfile: nothing to redeploy
		"POST /containers/create":            reply(map[string]string{"Id": "c1"}),
		"POST /containers/c1/wait":           reply(map[string]int{"StatusCode": 0}),
		"POST /pulls":                        reply(map[string]any{"number": 6, "head": map[string]string{"sha": "r"}}),
		"POST /pulls/6/merge": func(w http.ResponseWriter) {
			if revertTries++; revertTries == 1 {
				w.WriteHeader(http.StatusMethodNotAllowed) // Gitea still checking the new pull request
			}
		},
	}
	p := newFakeForge(t, f)

	_, err := p.land(issue{Number: 1, Title: "Feature"}, &pullRequest{Number: 5}, "agent/us-1", "h")
	if err == nil || !strings.Contains(err.Error(), "The merge was reverted on main") {
		t.Fatalf("land error = %v", err)
	}
	want := []string{
		"POST /pulls/5/merge",
		"GET /archive/" + merged + ".tar.gz",
		"POST /containers/create",
		"POST /pulls",
		"POST /pulls/6/merge",
		"POST /pulls/6/merge",
		"DELETE /containers/forge-staging-o-r",
		"PATCH /issues/1",
	}
	i := 0
	for _, call := range f.calls {
		if i < len(want) && call == want[i] {
			i++
		}
	}
	if i < len(want) {
		t.Fatalf("missing %q in order; calls: %q", want[i], f.calls)
	}
}

func TestRemoveAgents(t *testing.T) {
	f := &fakes{routes: map[string]func(http.ResponseWriter){
		"GET /containers/json": reply([]map[string]string{{"Id": "x"}, {"Id": "y"}}),
	}}
	p := newFakeForge(t, f)
	if n, err := p.docker.removeAgents(); err != nil || n != 2 {
		t.Fatalf("removeAgents = %d, %v", n, err)
	}
	if !slices.Contains(f.calls, "DELETE /containers/x") || !slices.Contains(f.calls, "DELETE /containers/y") {
		t.Fatalf("calls = %q", f.calls)
	}
}
