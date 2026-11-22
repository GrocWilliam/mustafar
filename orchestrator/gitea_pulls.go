package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

type pullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// findPR returns the open pull request from branch, or nil when there is none.
func (g *gitea) findPR(branch string) (*pullRequest, error) {
	// shortcut: first 50 open PRs only, paginate if agents ever keep more open at once
	var prs []pullRequest
	if err := g.call("GET", "/pulls?state=open&limit=50", nil, &prs); err != nil {
		return nil, err
	}
	for i := range prs {
		if prs[i].Head.Ref == branch {
			return &prs[i], nil
		}
	}
	return nil, nil
}

// prSettle is how long openPR waits after a push: Gitea 1.22 handles pushes in a queue, and if the pull request
// already exists when it gets to a new branch's first push, it fires a `synchronize` run on top of the `opened` one.
// shortcut: a fixed wait, not a signal that Gitea handled the push; drop it with Gitea 1.26 `concurrency` in ci.yml.
var prSettle = 10 * time.Second

// openPR opens a pull request from branch to base.
func (g *gitea) openPR(title, body, branch, base string) (*pullRequest, error) {
	time.Sleep(prSettle)
	var pr pullRequest
	err := g.call("POST", "/pulls", map[string]string{
		"head":  branch,
		"base":  base,
		"title": title,
		"body":  body,
	}, &pr)
	return &pr, err
}

// ciState returns the combined commit status of sha: "success", "pending", "failure", "error"... or "" before any run.
func (g *gitea) ciState(sha string) (string, error) {
	var status struct {
		State string `json:"state"`
	}
	err := g.call("GET", "/commits/"+sha+"/status", nil, &status)
	return status.State, err
}

var (
	logStamp = regexp.MustCompile(`(?m)^\S+Z `)
	ansi     = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

// failedCILog returns the end of each failed CI job's log on sha, up to its runner's `exitcode` line. Gitea 1.22's API
// has no job logs, so it reads the web UI's log route, which takes the same token.
func (g *gitea) failedCILog(sha string, lines int) (string, error) {
	var statuses []struct {
		Status    string `json:"status"`
		Context   string `json:"context"`
		TargetURL string `json:"target_url"`
	}
	if err := g.call("GET", "/commits/"+sha+"/statuses?limit=50", nil, &statuses); err != nil {
		return "", err
	}
	root, _, _ := strings.Cut(g.base, "/api/v1")
	seen := map[string]bool{}
	var out strings.Builder
	for _, s := range statuses {
		if (s.Status != "failure" && s.Status != "error") || s.TargetURL == "" || seen[s.TargetURL] {
			continue
		}
		seen[s.TargetURL] = true
		req, err := http.NewRequest("GET", root+s.TargetURL+"/logs", nil)
		if err != nil {
			return "", err
		}
		maps.Copy(req.Header, g.header)
		resp, err := g.http.Do(req)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("gitea GET %s/logs: %s", s.TargetURL, resp.Status)
		}
		fmt.Fprintf(&out, "%s:\n%s\n", s.Context, tailBeforeExit(string(data), lines))
	}
	return out.String(), nil
}

// failContext is how many lines before a bun `(fail)` line hold its error (`error:`, Expected, Received, the source
// line); maxFailLines caps the failures quoted ahead of the tail.
const (
	failContext  = 12
	maxFailLines = 120
)

// tailBeforeExit cleans a job log and keeps its last lines before the runner's `exitcode` line (the post-step noise).
// The CI job runs every script even after one fails, so a failure is usually far above the tail: each failing test's
// lines, under its script's `==` header, come first.
func tailBeforeExit(log string, lines int) string {
	log = ansi.ReplaceAllString(logStamp.ReplaceAllString(log, ""), "")
	if i := strings.Index(log, "exitcode '"); i >= 0 {
		log = log[:i]
	}
	all := strings.Split(strings.TrimRight(log, "\n"), "\n")
	start := max(0, len(all)-lines)
	tail := strings.Join(all[start:], "\n")
	var fails []string
	header, next := "", 0 // next: the first line not quoted yet
	for i, line := range all[:start] {
		if strings.HasPrefix(line, "== ") {
			header = line
		}
		if !strings.HasPrefix(line, "(fail) ") {
			continue
		}
		from := max(next, i-failContext)
		if header != "" && !slices.Contains(all[from:i], header) {
			fails = append(fails, header)
		}
		header = ""
		fails = append(fails, all[from:i+1]...)
		next = i + 1
	}
	if len(fails) == 0 {
		return tail
	}
	if len(fails) > maxFailLines {
		fails = append(fails[:maxFailLines], "[more failures cut]")
	}
	return "Failures:\n" + strings.Join(fails, "\n") + "\n\nEnd of the log:\n" + tail
}

// merge squash-merges pr, only if its branch is still at head: what was checked is what gets merged.
// Gitea answers 405 until its background check of a new or updated pull request is done: retry for a while.
func (g *gitea) merge(pr int, title, head string) error {
	body := map[string]any{
		"Do":                        "squash",
		"MergeTitleField":           title,
		"head_commit_id":            head,
		"delete_branch_after_merge": true,
	}
	for tries := 1; ; tries++ {
		err := g.call("POST", fmt.Sprintf("/pulls/%d/merge", pr), body, nil)
		if err == nil || tries >= mergeTries || !hasStatus(err, http.StatusMethodNotAllowed) {
			return err
		}
		time.Sleep(mergeRetry)
	}
}

var mergeTries, mergeRetry = 30, time.Second // variables so tests need not wait

func (g *gitea) closeIssue(number int) error {
	return g.call("PATCH", fmt.Sprintf("/issues/%d", number), map[string]string{"state": "closed"}, nil)
}

func (g *gitea) reopen(number int) error {
	return g.call("PATCH", fmt.Sprintf("/issues/%d", number), map[string]string{"state": "open"}, nil)
}

// behind counts the commits on base that branch does not contain.
func (g *gitea) behind(branch, base string) (int, error) {
	var compare struct {
		TotalCommits int `json:"total_commits"`
	}
	err := g.call("GET", "/compare/"+branch+"..."+base, nil, &compare)
	return compare.TotalCommits, err
}

var errConflict = errors.New("merge conflict")

// updatePR merges the base branch into the pull request's branch, server side. Gitea answers 409 on conflicts.
func (g *gitea) updatePR(pr int) error {
	err := g.call("POST", fmt.Sprintf("/pulls/%d/update?style=merge", pr), nil, nil)
	if hasStatus(err, http.StatusConflict) {
		return errConflict
	}
	return err
}

// retarget makes pr merge into base.
func (g *gitea) retarget(pr int, base string) error {
	return g.call("PATCH", fmt.Sprintf("/pulls/%d", pr), map[string]string{"base": base}, nil)
}

func (g *gitea) closePR(number int) error {
	return g.call("PATCH", fmt.Sprintf("/pulls/%d", number), map[string]string{"state": "closed"}, nil)
}
