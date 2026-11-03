package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Project setup and push mirrors, as new-project.sh and mirror.sh do them, for the UI.

var repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// projectInput is what the UI's new-project form sends: a new empty repo, an import from a clone URL,
// or an existing Gitea repo to adopt.
type projectInput struct {
	Mode     string `json:"mode"` // create, import, adopt
	Name     string `json:"name"` // the new repo's name, or owner/repo to adopt
	CloneURL string `json:"cloneURL"`
	User     string `json:"user"`
	Token    string `json:"token"` // read access to CloneURL, if private: used once by Gitea, never stored
}

// addProject creates or finds the repository, then sets it up as a forge project.
func (f *forge) addProject(in projectInput) (string, error) {
	in.Name = strings.TrimSpace(in.Name)
	var repo repoInfo
	switch in.Mode {
	case "create":
		if !repoName.MatchString(in.Name) {
			return "", refuse("a project name has letters, digits, '.', '_' or '-' (at most 100)")
		}
		// Needs the write:user scope on GITEA_TOKEN (README, step 3).
		if err := f.root.call("POST", "/user/repos", map[string]any{"name": in.Name, "auto_init": true, "default_branch": "main"}, &repo); err != nil {
			return "", err
		}
	case "import":
		u, err := url.Parse(strings.TrimSpace(in.CloneURL))
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return "", refuse("the clone URL must be https://host/owner/repo(.git), without credentials: put a token in the token field")
		}
		if !repoName.MatchString(in.Name) {
			return "", refuse("a project name has letters, digits, '.', '_' or '-' (at most 100)")
		}
		body := map[string]any{"clone_addr": u.String(), "repo_name": in.Name, "service": "git"}
		if in.Token != "" {
			body["auth_username"], body["auth_password"] = orDefault(in.User, "oauth2"), in.Token
		}
		// Gitea clones before it answers: a big repository takes longer than the client's usual 30 s.
		slow := f.root.api
		slow.http = &http.Client{Timeout: 10 * time.Minute}
		if err := slow.call("POST", "/repos/migrate", body, &repo); err != nil {
			return "", err
		}
	case "adopt":
		if err := f.root.call("GET", "/repos/"+in.Name, nil, &repo); err != nil {
			return "", err
		}
	default:
		return "", refuse("unknown mode %q", in.Mode)
	}
	if err := f.setUpProject(repo.FullName); err != nil {
		return "", fmt.Errorf("%s exists but its setup failed, adopt it to retry: %w", repo.FullName, err)
	}
	return fmt.Sprintf("%s is a forge project: it shows up within %s", repo.FullName, f.settings().pollEvery()), nil
}

// setUpProject gives a repository the agents' deploy key and the forge CI, then the `forge` topic. Each step is
// skipped when already done, so a failed setup can simply run again.
func (f *forge) setUpProject(fullName string) error {
	g := newGitea(f.cfg, "/repos/"+fullName)
	pub, err := os.ReadFile(f.cfg.DeployKeyPubFile)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(string(pub))
	var keys []struct {
		Key string `json:"key"`
	}
	if err := g.call("GET", "/keys?limit=50", nil, &keys); err != nil {
		return err
	}
	if !slices.ContainsFunc(keys, func(k struct {
		Key string `json:"key"`
	}) bool {
		return sameKey(k.Key, key)
	}) {
		if err := g.call("POST", "/keys", map[string]any{"title": "agents", "key": key, "read_only": false}, nil); err != nil {
			return fmt.Errorf("deploy key: %w", err)
		}
	}
	ci, err := os.ReadFile(f.cfg.CITemplate)
	if err != nil {
		return err
	}
	if err := g.putFile(".gitea/workflows/ci.yml", "ci: add the forge workflow", ci, true); err != nil {
		return fmt.Errorf("CI workflow: %w", err)
	}
	memory, err := os.ReadFile(f.cfg.MemoryTemplate)
	if err != nil {
		return err
	}
	// The agents' memory, which they keep current: an existing one is the project's own, never replaced.
	if err := g.putFile("CLAUDE.md", "docs: add the agents' CLAUDE.md", memory, false); err != nil {
		return fmt.Errorf("CLAUDE.md: %w", err)
	}
	// Last: once the orchestrator sees the topic, it protects the default branch against every push.
	return g.call("PUT", "/topics/"+projectTopic, nil, nil)
}

// sameKey compares two OpenSSH public keys by type and key, ignoring the comment.
func sameKey(a, b string) bool {
	fa, fb := strings.Fields(a), strings.Fields(b)
	return len(fa) >= 2 && len(fb) >= 2 && fa[0] == fb[0] && fa[1] == fb[1]
}

// orDefault returns s trimmed, or fallback when s is blank.
func orDefault(s, fallback string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return fallback
}

// putFile commits content at path on the default branch, creating the file or, with replace, replacing a different one.
func (g *gitea) putFile(path, message string, content []byte, replace bool) error {
	var existing struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	err := g.call("GET", "/contents/"+path, nil, &existing)
	body := map[string]string{"message": message, "content": base64.StdEncoding.EncodeToString(content)}
	switch {
	case hasStatus(err, http.StatusNotFound):
		return g.call("POST", "/contents/"+path, body, nil)
	case err != nil || !replace:
		return err
	}
	if old, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(existing.Content, "\n", "")); string(old) == string(content) {
		return nil
	}
	body["sha"] = existing.SHA
	return g.call("PUT", "/contents/"+path, body, nil)
}

// adoptable lists the repositories the token administers that are not forge projects yet.
func (f *forge) adoptable() ([]string, error) {
	// shortcut: first 50 repositories only, paginate if the Gitea ever holds more
	var found struct {
		Data []struct {
			FullName    string          `json:"full_name"`
			Topics      []string        `json:"topics"`
			Permissions map[string]bool `json:"permissions"`
			Mirror      bool            `json:"mirror"`
		} `json:"data"`
	}
	if err := f.root.call("GET", "/repos/search?limit=50", nil, &found); err != nil {
		return nil, err
	}
	names := []string{}
	for _, r := range found.Data {
		if r.Permissions["admin"] && !r.Mirror && !slices.Contains(r.Topics, projectTopic) {
			names = append(names, r.FullName)
		}
	}
	return names, nil
}

type pushMirror struct {
	RemoteName    string `json:"remote_name"`
	RemoteAddress string `json:"remote_address"`
	LastUpdate    string `json:"last_update"`
	LastError     string `json:"last_error"`
	Interval      string `json:"interval"`
	SyncOnCommit  bool   `json:"sync_on_commit"`
}

func (g *gitea) pushMirrors() ([]pushMirror, error) {
	mirrors := []pushMirror{}
	err := g.call("GET", "/push_mirrors?limit=50", nil, &mirrors)
	return mirrors, err
}

// mirrorInput is what the UI's mirror forms send.
type mirrorInput struct {
	Project string `json:"project"`
	Action  string `json:"action"` // add, sync, remove
	URL     string `json:"url"`
	User    string `json:"user"`
	Token   string `json:"token"` // write access to URL: Gitea keeps it, the forge never logs or returns it
	Remote  string `json:"remote"`
}

// mirror adds, syncs or removes a project's push mirrors: one-way, the forge stays the source of truth.
func (f *forge) mirror(in mirrorInput) (string, error) {
	p := f.readyProject(in.Project)
	if p == nil {
		return "", fmt.Errorf("unknown project %s: %w", in.Project, errNotFound)
	}
	switch in.Action {
	case "add":
		u, err := url.Parse(strings.TrimSpace(in.URL))
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return "", refuse("the remote URL must be https://host/owner/repo(.git), without credentials")
		}
		if strings.TrimSpace(in.Token) == "" {
			return "", refuse("a mirror needs a token with write access to %s", u.Host)
		}
		body := map[string]any{"remote_address": u.String(), "remote_username": orDefault(in.User, "forge"),
			"remote_password": strings.TrimSpace(in.Token), "interval": "8h0m0s", "sync_on_commit": true}
		if err := p.gitea.call("POST", "/push_mirrors", body, nil); err != nil {
			return "", err
		}
		if err := p.gitea.call("POST", "/push_mirrors-sync", nil, nil); err != nil {
			return "", fmt.Errorf("mirror added, but its first push failed: %w", err)
		}
		return fmt.Sprintf("%s now mirrors to %s: the first push runs in the background", p.name, u.Host), nil
	case "sync":
		if err := p.gitea.call("POST", "/push_mirrors-sync", nil, nil); err != nil {
			return "", err
		}
		return p.name + ": mirrors syncing in the background", nil
	case "remove":
		if in.Remote == "" || strings.ContainsAny(in.Remote, "/?#") {
			return "", refuse("which mirror?")
		}
		if err := p.gitea.call("DELETE", "/push_mirrors/"+url.PathEscape(in.Remote), nil, nil); err != nil {
			return "", err
		}
		return p.name + ": mirror removed, the remote keeps what it has", nil
	}
	return "", refuse("unknown mirror action %q", in.Action)
}
