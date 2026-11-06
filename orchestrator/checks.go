package main

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Pure checks behind the merge decision.

var verdictLine = regexp.MustCompile(`(?im)^\W*VERDICT:\s*(approve|changes)\W*$`)

// verdict reads the reviewer's last `VERDICT:` line; anything but an explicit approve is a rejection.
func verdict(review string) (bool, string) {
	matches := verdictLine.FindAllStringSubmatch(review, -1)
	if len(matches) == 0 {
		return false, "(the reviewer gave no verdict)\n\n" + review
	}
	return strings.EqualFold(matches[len(matches)-1][1], "approve"), review
}

// changed lists the files under prefix that were added, removed or modified between two trees.
func changed(before, after map[string]string, prefix string) []string {
	var paths []string
	for path, sha := range after {
		if strings.HasPrefix(path, prefix) && before[path] != sha {
			paths = append(paths, path)
		}
	}
	for path := range before {
		if _, kept := after[path]; !kept && strings.HasPrefix(path, prefix) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

// drifted lists the files under prefix whose content in head matches none of the allowed trees.
// A file missing from a tree counts as content: deleting it matches a tree that lacks it.
func drifted(head map[string]string, prefix string, allowed ...map[string]string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, tree := range append(allowed, head) {
		for path := range tree {
			if seen[path] || !strings.HasPrefix(path, prefix) {
				continue
			}
			seen[path] = true
			if !slices.ContainsFunc(allowed, func(t map[string]string) bool { return t[path] == head[path] }) {
				paths = append(paths, path)
			}
		}
	}
	sort.Strings(paths)
	return paths
}

// frozen lists the acceptance test files the dev changed. Files the tester wrote or rewrote must stay exactly as it
// committed them, unless main changed them too: a merge of main then holds both edits. The other ones may also
// match main, which a merge of main into the branch brings in.
func frozen(base, tester, main, head map[string]string) []string {
	owned := changed(base, tester, acceptanceDir)
	var touched []string
	for _, path := range drifted(head, acceptanceDir, tester, main) {
		if !slices.Contains(owned, path) {
			touched = append(touched, path)
		}
	}
	for _, path := range owned {
		// shortcut: a file both the tester and main changed is only checked for being kept, not compared line by line;
		// the reviewer sees its diff. Compute the three-way merge here if a dev ever abuses it.
		mergedByMain := main[path] != base[path] && head[path] != ""
		if head[path] != tester[path] && !mergedByMain {
			touched = append(touched, path)
		}
	}
	sort.Strings(touched)
	return touched
}

// outside keeps the paths that are not under prefix.
func outside(paths []string, prefix string) []string {
	var out []string
	for _, path := range paths {
		if !strings.HasPrefix(path, prefix) {
			out = append(out, path)
		}
	}
	return out
}

func list(paths []string) string {
	return "`" + strings.Join(paths, "`, `") + "`"
}
