package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// The forge UI: one page served on :8000 that shows every project, spec and story with its pipeline step and
// its agent's live activity, and offers the human interventions (retry, budget, new spec). Gitea stays the
// source of truth: the page reads the labels and the orchestrator's own comments, and its buttons change labels
// and post comments, as a human would in Gitea.

//go:embed ui.html
var uiPage []byte

// storySteps are the pipeline steps a story goes through, in order; progressOf returns an index into it.
var storySteps = []string{"Queued", "Tests", "Code", "CI", "Review", "Merge", "Done"}

type storyProgress struct {
	Step    int    `json:"step"`
	Attempt int    `json:"attempt"`
	Blocked string `json:"blocked,omitempty"` // first line of the last ⛔ reason, while blocked
}

// progressOf reads where a story stands from the orchestrator's comments on it (oldest first) and its labels.
func progressOf(comments []string, state string, labels []string) storyProgress {
	p := storyProgress{Attempt: 1}
	for _, c := range comments {
		first, _, _ := strings.Cut(c, "\n")
		done := strings.Contains(first, "✅")
		switch {
		case strings.HasPrefix(c, "🤖 Agent `tester`"):
			p = storyProgress{Step: 1, Attempt: 1}
			if done {
				p.Step = 2
			}
		case strings.HasPrefix(c, "🤖 Agent `dev`"):
			p.Step, p.Blocked = 2, ""
			if done {
				p.Step = 3
			}
		case strings.HasPrefix(c, "🤖 Agent `reviewer`"):
			p.Step = 4
			if done {
				p.Step = 5
			}
		case strings.HasPrefix(c, "🔀 Pull request opened"):
			p.Step = 3
		case strings.HasPrefix(c, "🔁 Attempt"):
			p.Step = 2
			fmt.Sscanf(c, "🔁 Attempt %d/", &p.Attempt)
		case strings.HasPrefix(c, "🔄"):
			p.Step = 5
		case strings.HasPrefix(c, "✅ CI green"):
			p.Step = 6
		case strings.HasPrefix(c, "♻️ Resuming"):
			p = storyProgress{Step: 2, Attempt: 1}
		case strings.HasPrefix(c, "♻️"):
			p = storyProgress{Attempt: 1}
		case strings.HasPrefix(c, "⛔ Blocked: "):
			p.Blocked = strings.TrimPrefix(first, "⛔ Blocked: ")
		}
	}
	switch {
	case state == "closed":
		return storyProgress{Step: len(storySteps) - 1, Attempt: p.Attempt}
	case slices.Contains(labels, "us-blocked"):
		if p.Blocked == "" {
			p.Blocked = "blocked"
		}
	case slices.Contains(labels, "us-todo"):
		return storyProgress{Attempt: 1} // queued, or back in the queue after a retry
	default:
		p.Blocked = ""
	}
	return p
}

// commentCache keeps the comments the UI reads, keyed by `owner/repo#n` and the issue's updated_at,
// which Gitea bumps on every new comment. Comments of open issues expire anyway: agents edit theirs.
type commentCache struct {
	mu      sync.Mutex
	entries map[string]cachedComments
}

type cachedComments struct {
	updated string
	at      time.Time
	bodies  []string
}

const commentTTL = 10 * time.Second

func (c *commentCache) get(g *gitea, key string, is uiIssue) ([]string, error) {
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok && e.updated == is.Updated && (is.State == "closed" || time.Since(e.at) < commentTTL) {
		return e.bodies, nil
	}
	bodies, err := g.comments(is.Number)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[key] = cachedComments{is.Updated, time.Now(), bodies}
	c.mu.Unlock()
	return bodies, nil
}

type uiIssue struct {
	issue
	Updated string `json:"updated_at"`
}

func (is uiIssue) labelNames() []string {
	names := make([]string, len(is.Labels))
	for i, l := range is.Labels {
		names[i] = l.Name
	}
	return names
}

// allIssues lists every issue of the repository, open and closed.
func (g *gitea) allIssues() ([]uiIssue, error) {
	var all []uiIssue
	for page := 1; ; page++ {
		var batch []uiIssue
		if err := g.call("GET", fmt.Sprintf("/issues?state=all&type=issues&limit=50&page=%d", page), nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 50 {
			return all, nil
		}
	}
}

type uiStory struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`
	storyProgress
	Deps  []int          `json:"deps"`
	Agent *agentActivity `json:"agent,omitempty"`
	Cost  float64        `json:"cost"`
}

type uiSpec struct {
	Number  int            `json:"number"`
	Title   string         `json:"title"`
	URL     string         `json:"url"`
	State   string         `json:"state"`
	Phase   string         `json:"phase"` // planning, active, blocked, done
	Blocked string         `json:"blocked,omitempty"`
	Agent   *agentActivity `json:"agent,omitempty"`
	Cost    float64        `json:"cost"`
	Budget  float64        `json:"budget"`
	Done    int            `json:"done"`
	Stories []uiStory      `json:"stories"`
}

type uiProject struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Staging string   `json:"staging"`
	Specs   []uiSpec `json:"specs"`
	Error   string   `json:"error,omitempty"`
}

type uiState struct {
	MaxAgents     int         `json:"maxAgents"`
	Running       []string    `json:"running"`
	DefaultBudget float64     `json:"defaultBudget"`
	Projects      []uiProject `json:"projects"`
}

func (f *forge) readyProjects() []*project {
	f.pmu.RLock()
	defer f.pmu.RUnlock()
	var list []*project
	for _, p := range f.projects {
		if p.ready {
			list = append(list, p)
		}
	}
	slices.SortFunc(list, func(a, b *project) int { return strings.Compare(a.name, b.name) })
	return list
}

func (f *forge) readyProject(name string) *project {
	f.pmu.RLock()
	defer f.pmu.RUnlock()
	if p := f.projects[name]; p != nil && p.ready {
		return p
	}
	return nil
}

func (f *forge) state() uiState {
	f.mu.Lock()
	s := uiState{MaxAgents: f.cfg.MaxAgents, DefaultBudget: f.cfg.MaxCostPerSpec, Running: []string{}, Projects: []uiProject{}}
	for key := range f.running {
		s.Running = append(s.Running, key)
	}
	activity := map[string]agentActivity{}
	for key, a := range f.activity {
		activity[key] = *a // a copy: the agent keeps updating the original
	}
	f.mu.Unlock()
	slices.Sort(s.Running)
	for _, p := range f.readyProjects() {
		up, err := p.uiProject(activity)
		if err != nil {
			up.Error = err.Error()
		}
		s.Projects = append(s.Projects, up)
	}
	return s
}

func (o *project) uiProject(activity map[string]agentActivity) (uiProject, error) {
	up := uiProject{Name: o.name, URL: o.cfg.GiteaURL + "/" + o.name, Staging: "http://localhost:" + o.stagingPort(), Specs: []uiSpec{}}
	issues, err := o.gitea.allIssues()
	if err != nil {
		return up, err
	}
	byNumber := map[int]uiIssue{}
	partOf := map[int][]int{} // spec → its stories, by "Part of" line
	for _, is := range issues {
		byNumber[is.Number] = is
		if spec := refs(partOfLine, is.Body); len(spec) > 0 {
			partOf[spec[0]] = append(partOf[spec[0]], is.Number)
		}
	}
	agent := func(n int) *agentActivity {
		if a, ok := activity[fmt.Sprintf("%s#%d", o.name, n)]; ok {
			return &a
		}
		return nil
	}
	comments := func(is uiIssue) ([]string, error) {
		return o.comments.get(o.gitea, fmt.Sprintf("%s#%d", o.name, is.Number), is)
	}
	for _, is := range issues {
		labels := is.labelNames()
		if partOf[is.Number] == nil && !slices.Contains(labels, "spec") && !slices.Contains(labels, "spec-active") {
			continue // not a spec
		}
		if is.State == "closed" && partOf[is.Number] == nil {
			continue // a closed issue nothing refers to: not the forge's
		}
		specComments, err := comments(is)
		if err != nil {
			return up, err
		}
		spec := uiSpec{Number: is.Number, Title: is.Title, URL: up.URL + fmt.Sprintf("/issues/%d", is.Number), State: is.State,
			Cost: sumCosts(specComments), Budget: specBudget(specComments, o.cfg.MaxCostPerSpec), Agent: agent(is.Number), Stories: []uiStory{}}
		switch {
		case is.State == "closed":
			spec.Phase = "done"
		case slices.Contains(labels, "us-blocked"):
			spec.Phase, spec.Blocked = "blocked", progressOf(specComments, is.State, labels).Blocked
		case slices.Contains(labels, "spec"):
			spec.Phase = "planning"
		default:
			spec.Phase = "active"
		}
		// The plan's order first, then stories added to the spec by hand.
		order := planStories(specComments)
		for _, n := range partOf[is.Number] {
			if !slices.Contains(order, n) {
				order = append(order, n)
			}
		}
		for _, n := range order {
			st, ok := byNumber[n]
			if !ok {
				continue
			}
			stComments, err := comments(st)
			if err != nil {
				return up, err
			}
			story := uiStory{Number: n, Title: st.Title, URL: up.URL + fmt.Sprintf("/issues/%d", n), State: st.State,
				storyProgress: progressOf(stComments, st.State, st.labelNames()), Deps: refs(dependsLine, st.Body),
				Agent: agent(n), Cost: sumCosts(stComments)}
			if story.Deps == nil {
				story.Deps = []int{}
			}
			spec.Cost += story.Cost
			if st.State == "closed" {
				spec.Done++
			}
			spec.Stories = append(spec.Stories, story)
		}
		up.Specs = append(up.Specs, spec)
	}
	return up, nil
}

// serveUI registers the page, its state endpoint, and the actions. The actions start paid agent runs: they only
// accept requests carrying the X-Forge-UI header, which a page on another origin cannot send without a CORS
// preflight this server never answers, and docker-compose publishes :8000 on localhost only.
func (f *forge) serveUI() {
	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiPage)
	})
	http.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, f.state())
	})
	http.HandleFunc("POST /api/retry", f.action(f.retry))
	http.HandleFunc("POST /api/budget", f.action(f.setBudget))
	http.HandleFunc("POST /api/spec", f.action(f.openSpec))
}

type actionInput struct {
	Project string  `json:"project"`
	Number  int     `json:"number"`
	Fresh   bool    `json:"fresh"`
	Budget  float64 `json:"budget"`
	Title   string  `json:"title"`
	Body    string  `json:"body"`
}

// errBadRequest marks an action refused because of its input or the issue's state, not a failure.
var errBadRequest = errors.New("bad request")

func (f *forge) action(do func(*project, actionInput) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forge-UI") != "1" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "missing X-Forge-UI header"})
			return
		}
		var in actionInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		p := f.readyProject(in.Project)
		if p == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown project " + in.Project})
			return
		}
		msg, err := do(p, in)
		switch {
		case errors.Is(err, errBadRequest):
			writeJSON(w, http.StatusConflict, map[string]string{"error": strings.TrimSuffix(err.Error(), ": "+errBadRequest.Error())})
		case err != nil:
			log.Printf("%s: UI action: %v", p.name, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		default:
			log.Printf("%s: UI action: %s", p.name, msg)
			writeJSON(w, http.StatusOK, map[string]string{"message": msg})
		}
	}
}

func refuse(format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, errBadRequest)...)
}

// retry puts a blocked story back in the queue, or a blocked spec back to planning or to work.
func (f *forge) retry(p *project, in actionInput) (string, error) {
	is, err := p.gitea.getIssue(in.Number)
	if err != nil {
		return "", err
	}
	if !is.hasLabel("us-blocked") || is.State != "open" {
		return "", refuse("#%d is not blocked", in.Number)
	}
	if len(refs(partOfLine, is.Body)) > 0 { // a story
		note := "it resumes its branch if the dev had started"
		if in.Fresh {
			branch := fmt.Sprintf("agent/us-%d", in.Number)
			if pr, err := p.gitea.findPR(branch); err != nil {
				return "", err
			} else if pr != nil {
				if err := p.gitea.closePR(pr.Number); err != nil {
					return "", err
				}
			}
			if err := p.gitea.deleteBranch(branch); err != nil {
				return "", err
			}
			note = "from scratch: its branch and pull request are gone, the tester starts over"
		}
		if err := p.gitea.move(in.Number, "us-blocked", "us-todo"); err != nil {
			return "", err
		}
		_, err := p.gitea.comment(in.Number, "🔁 Retried from the forge UI, "+note+".")
		return fmt.Sprintf("#%d is back in the queue, %s", in.Number, note), err
	}
	comments, err := p.gitea.comments(in.Number)
	if err != nil {
		return "", err
	}
	stories := planStories(comments)
	if len(stories) == 0 { // planning failed: plan again
		if err := p.gitea.move(in.Number, "us-blocked", "spec"); err != nil {
			return "", err
		}
		_, err := p.gitea.comment(in.Number, "🔁 Retried from the forge UI: the planner runs again.")
		return fmt.Sprintf("spec #%d goes back to planning", in.Number), err
	}
	cost, budget, err := p.specCost(in.Number)
	if err != nil {
		return "", err
	}
	if cost > budget {
		return "", refuse("spec #%d has spent $%.2f of its $%.2f budget: raise the budget first", in.Number, cost, budget)
	}
	// Over budget earlier: the spec and the stories it stopped resume together.
	var resumed []string
	for _, n := range stories {
		story, err := p.gitea.getIssue(n)
		if err != nil {
			return "", err
		}
		if story.State == "open" && story.hasLabel("us-blocked") {
			if err := p.gitea.move(n, "us-blocked", "us-todo"); err != nil {
				return "", err
			}
			resumed = append(resumed, fmt.Sprintf("#%d", n))
		}
	}
	if err := p.gitea.move(in.Number, "us-blocked", "spec-active"); err != nil {
		return "", err
	}
	msg := "🔁 Retried from the forge UI: the spec is active again"
	if len(resumed) > 0 {
		msg += ", and its blocked stories are back in the queue: " + strings.Join(resumed, ", ")
	}
	_, err = p.gitea.comment(in.Number, msg+".")
	return fmt.Sprintf("spec #%d is active again (%d stories requeued)", in.Number, len(resumed)), err
}

// setBudget sets one spec's budget with a marker comment, which specCost reads.
func (f *forge) setBudget(p *project, in actionInput) (string, error) {
	if in.Budget <= 0 || in.Budget > 1000 {
		return "", refuse("the budget must be between $0 and $1000")
	}
	is, err := p.gitea.getIssue(in.Number)
	if err != nil {
		return "", err
	}
	if len(refs(partOfLine, is.Body)) > 0 {
		return "", refuse("#%d is a story: budgets are per spec", in.Number)
	}
	msg := fmt.Sprintf("💰 Budget set to $%.2f from the forge UI (default $%.2f). %s", in.Budget, f.cfg.MaxCostPerSpec, budgetMarkerFor(in.Budget))
	if _, err := p.gitea.comment(in.Number, msg); err != nil {
		return "", err
	}
	return fmt.Sprintf("spec #%d budget is now $%.2f", in.Number, in.Budget), nil
}

// openSpec opens a new spec issue: the planner picks it up on the next poll.
func (f *forge) openSpec(p *project, in actionInput) (string, error) {
	title, body := strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	if title == "" || len(title) > 200 || body == "" {
		return "", refuse("a spec needs a title (at most 200 characters) and a body")
	}
	number, err := p.gitea.createIssue(title, body)
	if err != nil {
		return "", err
	}
	if err := p.gitea.label(number, "spec"); err != nil {
		return "", err
	}
	return fmt.Sprintf("spec #%d opened: the planner picks it up within %s", number, f.cfg.PollEvery), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
