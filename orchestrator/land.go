package main

import (
	"errors"
	"fmt"
)

// land merges an approved pull request into is.base. Stories run in parallel, so that branch may have moved since CI ran:
// merges happen one at a time, and only once the branch contains its target and CI is green on the result.
// Only a merge into main deploys to staging, and is reverted if that fails. It returns what the branch still needs, if anything.
func (o *project) land(is issue, pr *pullRequest, branch, head string) (string, error) {
	o.merging.Lock()
	defer o.merging.Unlock()
	behind, err := o.gitea.behind(branch, is.base)
	if err != nil {
		return "", err
	}
	if behind > 0 {
		if err := o.gitea.updatePR(pr.Number); errors.Is(err, errConflict) {
			return fmt.Sprintf("The branch conflicts with `%[1]s`, which moved meanwhile. Run `git merge origin/%[1]s`, resolve the conflicts so that both sides keep working, run all the tests, and commit the merge.", is.base), nil
		} else if err != nil {
			return "", err
		}
		if head, err = o.gitea.branchHead(branch); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("🔄 `%s` moved by %d commit(s) since CI ran: merged it into the branch, waiting for CI again.", is.base, behind)
		if _, err := o.gitea.comment(is.Number, msg); err != nil {
			return "", err
		}
		if state, err := o.waitCI(head); err != nil {
			return "", err
		} else if state != "success" {
			return fmt.Sprintf("After merging the latest `%s` into the branch, CI is `%s` (%s): this work and what was merged meanwhile do not work together. Fix it.", is.base, state, pr.HTMLURL) + o.ciLog(head), nil
		}
	}
	previous, err := o.gitea.branchHead(is.base)
	if err != nil {
		return "", err
	}
	if err := o.gitea.merge(pr.Number, fmt.Sprintf("%s (#%d)", is.Title, is.Number), head); err != nil {
		return "", fmt.Errorf("merge failed: `%v`", err)
	}
	msg := fmt.Sprintf("✅ CI green and review approved, pull request merged into `%s`: %s", is.base, pr.HTMLURL)
	if is.base == o.base {
		// Still under the merge lock: main is exactly this merge, and the next one waits for the deployment verdict.
		merged, err := o.gitea.branchHead(o.base)
		if err != nil {
			return "", err
		}
		deployed, err := o.deploy(merged)
		if err != nil {
			return "", o.rollback(is, merged, previous, err)
		}
		if deployed {
			msg += fmt.Sprintf("\n\n🚀 Deployed to staging (http://localhost:%s), `/health` answers 200.", o.stagingPort())
		}
	}
	if _, err := o.gitea.comment(is.Number, msg); err != nil {
		return "", err
	}
	return "", o.gitea.closeIssue(is.Number)
}
