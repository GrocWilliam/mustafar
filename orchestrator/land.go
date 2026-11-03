package main

import (
	"errors"
	"fmt"
)

// land merges an approved pull request, then deploys main to staging and reverts the merge if that fails. Stories run in parallel, so main may have moved since CI ran:
// merges happen one at a time, and only once the branch contains main and CI is green on the result.
func (o *project) land(is issue, pr *pullRequest, branch, head string) (string, error) {
	o.merging.Lock()
	defer o.merging.Unlock()
	behind, err := o.gitea.behind(branch, o.base)
	if err != nil {
		return "", err
	}
	if behind > 0 {
		if err := o.gitea.updatePR(pr.Number); errors.Is(err, errConflict) {
			return fmt.Sprintf("The review approved your work, but your branch conflicts with `%[1]s`, which other stories changed meanwhile. Run `git merge origin/%[1]s`, resolve the conflicts so that both your story and theirs keep working, run all the tests, and commit the merge.", o.base), nil
		} else if err != nil {
			return "", err
		}
		if head, err = o.gitea.branchHead(branch); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("🔄 `%s` moved by %d commit(s) since CI ran: merged it into the branch, waiting for CI again.", o.base, behind)
		if _, err := o.gitea.comment(is.Number, msg); err != nil {
			return "", err
		}
		if state, err := o.waitCI(head); err != nil {
			return "", err
		} else if state != "success" {
			return fmt.Sprintf("The review approved your work, but after merging the latest `%s` into your branch, CI is `%s` (%s): your change and the stories merged meanwhile do not work together. Fix it.", o.base, state, pr.HTMLURL) + o.ciLog(head), nil
		}
	}
	previous, err := o.gitea.branchHead(o.base)
	if err != nil {
		return "", err
	}
	if err := o.gitea.merge(pr.Number, fmt.Sprintf("%s (#%d)", is.Title, is.Number), head); err != nil {
		return "", fmt.Errorf("merge failed: `%v`", err)
	}
	// Still under the merge lock: main is exactly this merge, and the next one waits for the deployment verdict.
	merged, err := o.gitea.branchHead(o.base)
	if err != nil {
		return "", err
	}
	deployed, err := o.deploy(merged)
	if err != nil {
		return "", o.rollback(is, merged, previous, err)
	}
	msg := "✅ CI green and review approved, pull request merged: " + pr.HTMLURL
	if deployed {
		msg += fmt.Sprintf("\n\n🚀 Deployed to staging (http://localhost:%s), `/health` answers 200.", o.stagingPort())
	}
	if _, err := o.gitea.comment(is.Number, msg); err != nil {
		return "", err
	}
	return "", o.gitea.closeIssue(is.Number)
}
