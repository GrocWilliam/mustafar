package main

import (
	"fmt"
	"net/http"
	"net/url"
)

// testerAuthor and retesterAuthor are the git authors of the tester's commits and of its revisions after a dev's
// conflict report (agents/agent.sh: `forge-agent-${ROLE}`).
const (
	testerAuthor   = "forge-agent-tester"
	retesterAuthor = "forge-agent-retester"
)

// branchCommit is one commit of a branch, as Gitea lists them, newest first.
type branchCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Name string `json:"name"`
		} `json:"author"`
	} `json:"commit"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
}

// commits lists the last 50 commits of branch, newest first.
func (g *gitea) commits(branch string) ([]branchCommit, error) {
	var list []branchCommit
	err := g.call("GET", "/commits?sha="+url.QueryEscape(branch)+"&limit=50&stat=false&verification=false&files=false", nil, &list)
	return list, err
}

// testerRun finds, in a branch's commits (newest first), the commit holding the tester's tests (its last commit,
// or its newest revision) and the commit its first run started from; revised tells whether a revision exists.
// ok is false unless later work follows the tester's commits: a branch the dev never touched starts over.
func testerRun(commits []branchCommit) (tester, base string, revised, ok bool) {
	i, revision := 0, -1
	for i < len(commits) && commits[i].Commit.Author.Name != testerAuthor {
		if revision < 0 && commits[i].Commit.Author.Name == retesterAuthor {
			revision = i
		}
		i++
	}
	if i == 0 || i == len(commits) {
		return "", "", false, false
	}
	j := i
	for j+1 < len(commits) && commits[j+1].Commit.Author.Name == testerAuthor {
		j++
	}
	if len(commits[j].Parents) == 0 {
		return "", "", false, false
	}
	if revision >= 0 {
		return commits[revision].SHA, commits[j].Parents[0].SHA, true, true
	}
	return commits[i].SHA, commits[j].Parents[0].SHA, false, true
}

// resume lets a retried US continue its branch instead of losing the dev's work: when the branch holds the tester's
// commits and later work, it returns the trees the checks compare against (the tester's base, and its tests) and
// skips the tester. Deleting the branch makes the US start over from the tester.
func (o *project) resume(is issue, branch string) (c contract, ok bool, err error) {
	if _, err := o.gitea.branchHead(branch); hasStatus(err, http.StatusNotFound) {
		return c, false, nil
	} else if err != nil {
		return c, false, err
	}
	commits, err := o.gitea.commits(branch)
	if err != nil {
		return c, false, err
	}
	testerSHA, baseSHA, revised, ok := testerRun(commits)
	if !ok {
		return c, false, nil
	}
	if revised {
		c.revisions = 1 // a human retry gets the revisions back but one
	}
	if c.base, err = o.gitea.tree(baseSHA); err != nil {
		return c, false, err
	}
	if c.tests, err = o.gitea.tree(testerSHA); err != nil {
		return c, false, err
	}
	msg := fmt.Sprintf("♻️ Resuming `%s`: the tester's acceptance tests (%.12s) and the dev's earlier work are kept; its last commit goes through CI and review again, and the dev continues only if they reject it. Delete the branch to start over from the tester.", branch, testerSHA)
	_, err = o.gitea.comment(is.Number, msg)
	return c, true, err
}
