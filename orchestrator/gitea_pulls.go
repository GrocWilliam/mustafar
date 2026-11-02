package main

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

type pullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
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

// openPR opens a pull request from branch to base.
func (g *gitea) openPR(title, body, branch, base string) (*pullRequest, error) {
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

func (g *gitea) closePR(number int) error {
	return g.call("PATCH", fmt.Sprintf("/pulls/%d", number), map[string]string{"state": "closed"}, nil)
}
