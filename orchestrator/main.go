// Command orchestrator polls Gitea for specs and `us-todo` issues in every repository tagged with the `forge`
// topic, and runs up to MAX_AGENTS of them at once across all those projects.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"
)

// projectTopic opts a repository in: the forge works on every repository carrying it that its token can see.
const projectTopic = "forge"

// forge holds what all projects share: the agent slots and the Docker engine.
type forge struct {
	cfg    config
	live   settings // under mu: read it with settings()
	root   *gitea   // API root, to discover the projects
	docker *dockerEngine
	// projects, by full name, are added by the poll loop and read by the UI handlers under pmu.
	pmu      sync.RWMutex
	projects map[string]*project
	// running holds the issues being worked on, as `owner/repo#n`: the only lock, rebuilt empty on startup.
	// activity holds what their agent does now, for the UI. Both under mu.
	mu       sync.Mutex
	running  map[string]bool
	activity map[string]*agentActivity
	comments commentCache // for the UI
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	live, err := loadSettings(cfg.SettingsFile, cfg.Defaults)
	if err != nil {
		log.Fatalf("settings: %v", err)
	}
	f := &forge{
		cfg:      cfg,
		live:     live,
		root:     newGitea(cfg, ""),
		docker:   newDocker("/var/run/docker.sock"),
		projects: map[string]*project{},
		running:  map[string]bool{},
		activity: map[string]*agentActivity{},
		comments: commentCache{entries: map[string]cachedComments{}},
	}
	// Agents of a previous run would keep pushing to the branches of the US that recover() restarts.
	if n, err := f.docker.removeAgents(); err != nil {
		log.Fatalf("removing leftover agent containers: %v", err)
	} else if n > 0 {
		log.Printf("removed %d leftover agent container(s)", n)
	}
	go f.loop()

	f.serveUI()
	http.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		running := []string{}
		for key := range f.running {
			running = append(running, key)
		}
		f.mu.Unlock()
		slices.Sort(running)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "running": running})
	})
	log.Fatal(http.ListenAndServe(":8000", nil))
}

func (f *forge) loop() {
	for {
		f.poll()
		time.Sleep(f.settings().pollEvery())
	}
}

// poll discovers the projects, then lets each one fill the free agent slots, in name order.
// A failing project is logged and skipped: it must not stall the others.
func (f *forge) poll() {
	repos, err := f.root.forgeRepos(projectTopic)
	if err != nil {
		log.Printf("listing projects failed: %v", err)
		return
	}
	for _, repo := range repos {
		f.pmu.RLock()
		p := f.projects[repo.FullName]
		f.pmu.RUnlock()
		if p == nil {
			p = newProject(f, repo)
			f.pmu.Lock()
			f.projects[repo.FullName] = p
			f.pmu.Unlock()
			log.Printf("%s: new project", repo.FullName)
		}
		if err := p.poll(); err != nil {
			log.Printf("%s: poll failed: %v", repo.FullName, err)
		}
	}
}

// start runs work for key in the background unless it is already running.
// It returns false when every agent slot is taken.
func (f *forge) start(key string, work func() error) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running[key] {
		return true
	}
	if len(f.running) >= f.live.MaxAgents {
		return false
	}
	f.running[key] = true
	go func() {
		if err := work(); err != nil {
			log.Printf("%s: %v", key, err)
		}
		f.mu.Lock()
		delete(f.running, key)
		f.mu.Unlock()
	}()
	return true
}

func (f *forge) isRunning(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[key]
}
