package main

import (
	"fmt"
	"net/http"
	"net/url"
)

// testerAuthor is the git author of the tester's commits (agents/agent.sh: `forge-agent-${ROLE}`).
const testerAuthor = "forge-agent-tester"

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

// testerRun finds, in a branch's commits (newest first), the tester's last commit and the commit its run started
// from. ok is false unless later work follows the tester's commits: a branch the dev never touched starts over.
func testerRun(commits []branchCommit) (tester, base string, ok bool) {
	i := 0
	for i < len(commits) && commits[i].Commit.Author.Name != testerAuthor {
		i++
	}
	if i == 0 || i == len(commits) {
		return "", "", false
	}
	j := i
	for j+1 < len(commits) && commits[j+1].Commit.Author.Name == testerAuthor {
		j++
	}
	if len(commits[j].Parents) == 0 {
		return "", "", false
	}
	return commits[i].SHA, commits[j].Parents[0].SHA, true
}

// resume lets a retried US continue its branch instead of losing the dev's work: when the branch holds the tester's
// commits and later work, it returns the trees the checks compare against (the tester's base, and its tests) and
// skips the tester. Deleting the branch makes the US start over from the tester.
func (o *project) resume(is issue, branch string) (base, tester map[string]string, ok bool, err error) {
	if _, err := o.gitea.branchHead(branch); hasStatus(err, http.StatusNotFound) {
		return nil, nil, false, nil
	} else if err != nil {
		return nil, nil, false, err
	}
	commits, err := o.gitea.commits(branch)
	if err != nil {
		return nil, nil, false, err
	}
	testerSHA, baseSHA, ok := testerRun(commits)
	if !ok {
		return nil, nil, false, nil
	}
	if base, err = o.gitea.tree(baseSHA); err != nil {
		return nil, nil, false, err
	}
	if tester, err = o.gitea.tree(testerSHA); err != nil {
		return nil, nil, false, err
	}
	msg := fmt.Sprintf("♻️ Resuming `%s`: the tester's acceptance tests (%.12s) and the dev's earlier work are kept, the dev continues. Delete the branch to start over from the tester.", branch, testerSHA)
	_, err = o.gitea.comment(is.Number, msg)
	return base, tester, true, err
}
