package main

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	defaults := settings{MaxAgents: 2, MaxCostPerSpec: 20, PollSeconds: 30, ModelLow: "haiku", ModelMedium: "sonnet", ModelHigh: "opus"}
	if s, err := loadSettings(path, defaults); err != nil || s != defaults {
		t.Fatalf("no file: got %+v, %v; want the defaults", s, err)
	}
	f := &forge{cfg: config{SettingsFile: path}, live: defaults}
	if err := f.updateSettings(settings{MaxAgents: 0, MaxCostPerSpec: 5, PollSeconds: 10}); !errors.Is(err, errBadRequest) {
		t.Fatalf("0 agents: got %v, want a refusal", err)
	}
	if f.settings() != defaults {
		t.Fatal("a refused change was applied")
	}
	if err := f.updateSettings(settings{MaxAgents: 2, MaxCostPerSpec: 5, PollSeconds: 10, ModelLow: "haiku", ModelMedium: "sonnet; rm -rf /", ModelHigh: "opus"}); !errors.Is(err, errBadRequest) {
		t.Fatalf("bad model: got %v, want a refusal", err)
	}
	want := settings{MaxAgents: 4, MaxCostPerSpec: 7.5, PollSeconds: 10, ModelLow: "claude-haiku-5-5", ModelMedium: "sonnet", ModelHigh: "opus[1m]"}
	if err := f.updateSettings(want); err != nil {
		t.Fatal(err)
	}
	if s, err := loadSettings(path, defaults); err != nil || s != want {
		t.Fatalf("after save: got %+v, %v; want %+v", s, err, want)
	}
	os.WriteFile(path, []byte(`{"maxAgents": 3}`), 0o600)
	if s, _ := loadSettings(path, defaults); s != (settings{MaxAgents: 3, MaxCostPerSpec: 20, PollSeconds: 30, ModelLow: "haiku", ModelMedium: "sonnet", ModelHigh: "opus"}) {
		t.Fatalf("partial file: got %+v, want the missing fields from the defaults", s)
	}
}

// A second setup of an already set-up project changes nothing: no second key, no commit, only the topic again.
func TestSetUpProjectIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	pub, ci := filepath.Join(dir, "key.pub"), filepath.Join(dir, "ci.yml")
	os.WriteFile(pub, []byte("ssh-ed25519 AAAA agents@host\n"), 0o600)
	os.WriteFile(ci, []byte("on: push\n"), 0o600)
	memory := filepath.Join(dir, "CLAUDE.md")
	os.WriteFile(memory, []byte("# Project memory\n"), 0o600)
	var writes []string
	done := false // whether the repo already has the key and the workflow
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/keys"):
			if done {
				w.Write([]byte(`[{"key": "ssh-ed25519 AAAA other-comment"}]`))
			} else {
				w.Write([]byte(`[]`))
			}
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/contents/"):
			if !done {
				http.NotFound(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, "CLAUDE.md") { // the project's own, different from the template: kept
				w.Write([]byte(`{"sha": "def", "content": "` + base64.StdEncoding.EncodeToString([]byte("# Mine\n")) + `"}`))
				return
			}
			w.Write([]byte(`{"sha": "abc", "content": "` + base64.StdEncoding.EncodeToString([]byte("on: push\n")) + `"}`))
		default:
			writes = append(writes, r.Method+" "+r.URL.Path[strings.Index(r.URL.Path, "/repos/"):])
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	f := &forge{cfg: config{GiteaURL: srv.URL, GiteaToken: "t", DeployKeyPubFile: pub, CITemplate: ci, MemoryTemplate: memory}}
	if err := f.setUpProject("forge/x"); err != nil {
		t.Fatal(err)
	}
	want := "POST /repos/forge/x/keys,POST /repos/forge/x/contents/.gitea/workflows/ci.yml,POST /repos/forge/x/contents/CLAUDE.md,PUT /repos/forge/x/topics/forge"
	if got := strings.Join(writes, ","); got != want {
		t.Fatalf("first setup wrote %s, want %s", got, want)
	}
	writes, done = nil, true
	if err := f.setUpProject("forge/x"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(writes, ","); got != "PUT /repos/forge/x/topics/forge" {
		t.Fatalf("second setup wrote %s, want only the topic", got)
	}
}

func TestAddProjectAndMirrorRefuseBadInput(t *testing.T) {
	f := &forge{projects: map[string]*project{"forge/x": {name: "forge/x", ready: true}}}
	for _, in := range []projectInput{
		{Mode: "create", Name: "../etc"},
		{Mode: "create", Name: ""},
		{Mode: "import", Name: "x", CloneURL: "http://github.com/a/b.git"},
		{Mode: "import", Name: "x", CloneURL: "https://user:secret@github.com/a/b.git"},
		{Mode: "delete", Name: "x"},
	} {
		if _, err := f.addProject(in); !errors.Is(err, errBadRequest) {
			t.Errorf("%+v: got %v, want a refusal", in, err)
		}
	}
	for _, in := range []mirrorInput{
		{Project: "forge/x", Action: "add", URL: "https://u:p@github.com/a/b.git", Token: "t"},
		{Project: "forge/x", Action: "add", URL: "https://github.com/a/b.git"},
		{Project: "forge/x", Action: "remove", Remote: "../../x"},
	} {
		if _, err := f.mirror(in); !errors.Is(err, errBadRequest) {
			t.Errorf("%+v: got %v, want a refusal", in, err)
		}
	}
}
