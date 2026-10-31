package main

import (
	"fmt"
	"io"
	"net/url"
	"sort"
)

func (g *gitea) defaultBranch() (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	err := g.call("GET", "", nil, &repo)
	return repo.DefaultBranch, err
}

// branchHead returns the commit sha at the tip of branch.
func (g *gitea) branchHead(branch string) (string, error) {
	var b struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	err := g.call("GET", "/branches/"+branch, nil, &b)
	return b.Commit.ID, err
}

// tree returns path -> blob sha for every file of commit sha.
func (g *gitea) tree(sha string) (map[string]string, error) {
	var t struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := g.call("GET", "/git/trees/"+sha+"?recursive=true&per_page=10000", nil, &t); err != nil {
		return nil, err
	}
	// shortcut: one page only; a truncated tree fails the US instead of passing an incomplete check, paginate if repos grow that big
	if t.Truncated {
		return nil, fmt.Errorf("tree of %s is truncated: too many files for one API page", sha)
	}
	files := make(map[string]string, len(t.Tree))
	for _, entry := range t.Tree {
		if entry.Type == "blob" {
			files[entry.Path] = entry.SHA
		}
	}
	return files, nil
}

// archive streams the repository at sha as a .tar.gz, every path under one top-level directory.
func (g *gitea) archive(sha string) (io.ReadCloser, error) {
	resp, err := g.do("GET", "/archive/"+sha+".tar.gz", nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// protect forbids pushes to branch, deploy keys included: agents hold a push key, so only the orchestrator's merges may reach it.
func (g *gitea) protect(branch string) error {
	var rules []struct {
		RuleName string `json:"rule_name"`
	}
	if err := g.call("GET", "/branch_protections", nil, &rules); err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.RuleName == branch {
			return g.call("PATCH", "/branch_protections/"+url.PathEscape(branch), map[string]bool{"enable_push": false}, nil)
		}
	}
	return g.call("POST", "/branch_protections", map[string]any{"rule_name": branch, "enable_push": false}, nil)
}

type repoInfo struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
}

// forgeRepos lists the repositories tagged with topic that the token can see, by name. Call it on the API root client.
func (g *gitea) forgeRepos(topic string) ([]repoInfo, error) {
	// shortcut: first 50 projects only, paginate if the forge ever serves more
	var found struct {
		Data []repoInfo `json:"data"`
	}
	err := g.call("GET", "/repos/search?topic=true&limit=50&q="+url.QueryEscape(topic), nil, &found)
	sort.Slice(found.Data, func(i, j int) bool { return found.Data[i].FullName < found.Data[j].FullName })
	return found.Data, err
}
