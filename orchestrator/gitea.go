package main

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"
)

var lifecycleLabels = map[string]string{
	"spec":           "#5319e7", // to plan
	"spec-active":    "#8a63d2", // planned, stories in progress
	"us-todo":        "#1d76db",
	"us-in-progress": "#fbca04",
	"us-review":      "#0e8a16",
	"us-blocked":     "#d93f0b",
}

type issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (is issue) hasLabel(name string) bool {
	for _, label := range is.Labels {
		if label.Name == name {
			return true
		}
	}
	return false
}

// gitea is a minimal Gitea API client, scoped to one repository (path `/repos/owner/repo`) or to the API root (path "").
type gitea struct {
	api
	labelIDs map[string]int64
}

func newGitea(c config, path string) *gitea {
	return &gitea{
		api: api{
			name:   "gitea",
			base:   c.GiteaURL + "/api/v1" + path,
			http:   &http.Client{Timeout: 30 * time.Second},
			header: http.Header{"Authorization": {"token " + c.GiteaToken}},
		},
		labelIDs: map[string]int64{},
	}
}

// ensureLabels creates the lifecycle labels if missing and caches their ids.
func (g *gitea) ensureLabels() error {
	var existing []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := g.call("GET", "/labels?limit=50", nil, &existing); err != nil {
		return err
	}
	for _, label := range existing {
		g.labelIDs[label.Name] = label.ID
	}
	for name, color := range lifecycleLabels {
		if _, ok := g.labelIDs[name]; ok {
			continue
		}
		var created struct {
			ID int64 `json:"id"`
		}
		if err := g.call("POST", "/labels", map[string]string{"name": name, "color": color}, &created); err != nil {
			return err
		}
		g.labelIDs[name] = created.ID
	}
	return nil
}

// issuesWith returns the open issues carrying label, oldest first.
func (g *gitea) issuesWith(label string) ([]issue, error) {
	// shortcut: first 50 matches only, paginate if the queue ever grows beyond that
	var issues []issue
	err := g.call("GET", "/issues?state=open&type=issues&limit=50&labels="+url.QueryEscape(label), nil, &issues)
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
	return issues, err
}

// move swaps one lifecycle label for another, keeping the issue's other labels.
func (g *gitea) move(number int, from, to string) error {
	if err := g.call("DELETE", fmt.Sprintf("/issues/%d/labels/%d", number, g.labelIDs[from]), nil, nil); err != nil {
		return err
	}
	return g.label(number, to)
}

func (g *gitea) label(number int, name string) error {
	return g.call("POST", fmt.Sprintf("/issues/%d/labels", number), map[string][]int64{"labels": {g.labelIDs[name]}}, nil)
}

func (g *gitea) getIssue(number int) (issue, error) {
	var is issue
	err := g.call("GET", fmt.Sprintf("/issues/%d", number), nil, &is)
	return is, err
}

func (g *gitea) createIssue(title, body string) (int, error) {
	var created issue
	err := g.call("POST", "/issues", map[string]string{"title": title, "body": body}, &created)
	return created.Number, err
}

// comments returns the bodies of all comments on an issue, oldest first.
func (g *gitea) comments(number int) ([]string, error) {
	var list []struct {
		Body string `json:"body"`
	}
	if err := g.call("GET", fmt.Sprintf("/issues/%d/comments", number), nil, &list); err != nil {
		return nil, err
	}
	bodies := make([]string, len(list))
	for i, c := range list {
		bodies[i] = c.Body
	}
	return bodies, nil
}

func (g *gitea) comment(number int, body string) (int64, error) {
	var created struct {
		ID int64 `json:"id"`
	}
	err := g.call("POST", fmt.Sprintf("/issues/%d/comments", number), map[string]string{"body": body}, &created)
	return created.ID, err
}

func (g *gitea) editComment(id int64, body string) error {
	return g.call("PATCH", fmt.Sprintf("/issues/comments/%d", id), map[string]string{"body": body}, nil)
}
