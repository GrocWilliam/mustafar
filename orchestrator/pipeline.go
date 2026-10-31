package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	maxAttempts   = 3
	ciTimeout     = 30 * time.Minute
	acceptanceDir = "tests/acceptance/"
)

// deliver takes a US from acceptance tests to merge: tester, then up to maxAttempts dev cycles.
// Agents write code; only the checks here decide. label tracks the US's current label for block().
// Attempts and feedback live in memory: a restart re-queues the US from scratch.
func (o *project) deliver(is issue, label *string) error {
	branch := fmt.Sprintf("agent/us-%d", is.Number)
	baseTree, err := o.treeOf(o.base)
	if err != nil {
		return err
	}

	if ok, _, err := o.runAgent("tester", is, ""); err != nil || !ok {
		return agentFailure("tester", err)
	}
	testerTree, err := o.treeOf(branch)
	if err != nil {
		return err
	}
	ownDir := fmt.Sprintf("%sus-%d/", acceptanceDir, is.Number)
	if stray := outside(changed(baseTree, testerTree, ""), acceptanceDir); len(stray) > 0 {
		return fmt.Errorf("the tester changed files outside `%s`: %s", acceptanceDir, list(stray))
	}
	if len(changed(baseTree, testerTree, ownDir)) == 0 {
		return fmt.Errorf("the tester wrote no acceptance tests in `%s`", ownDir)
	}
	// A story may change behavior that earlier stories' tests pin down: the tester updates those tests, visibly.
	if earlier := outside(changed(baseTree, testerTree, acceptanceDir), ownDir); len(earlier) > 0 {
		msg := "🧪 This story supersedes earlier acceptance tests, which the tester updated: " + list(earlier) + ". The reviewer checks that the story requires it."
		if _, err := o.gitea.comment(is.Number, msg); err != nil {
			return err
		}
	}

	feedback := ""
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			msg := fmt.Sprintf("🔁 Attempt %d/%d. Feedback for the dev agent:\n\n%s", attempt, maxAttempts, feedback)
			if _, err := o.gitea.comment(is.Number, msg); err != nil {
				return err
			}
		}
		if feedback, err = o.attempt(is, branch, baseTree, testerTree, feedback, label); err != nil || feedback == "" {
			return err
		}
	}
	return fmt.Errorf("still rejected after %d attempts. Last feedback:\n\n%s", maxAttempts, feedback)
}

// attempt runs one dev cycle and its checks. It returns the feedback for the next cycle, or "" once merged.
// An error means the US cannot go on (infrastructure failure or misbehaving agent).
func (o *project) attempt(is issue, branch string, baseTree, testerTree map[string]string, feedback string, label *string) (string, error) {
	if ok, final, err := o.runAgent("dev", is, feedback); err != nil {
		return "", err
	} else if !ok {
		// Its own explanation (a contradiction in the tests, a missing tool...) beats a generic retry message.
		msg := "Your previous run failed or produced no new commit. Make the acceptance tests pass and commit your work."
		if final = strings.TrimSpace(final); final != "" {
			msg += "\n\nYour previous run ended with:\n\n" + clip(final, 3000)
		}
		return msg, nil
	}
	head, err := o.gitea.branchHead(branch)
	if err != nil {
		return "", err
	}
	pr, err := o.ensurePR(is, branch)
	if err != nil {
		return "", err
	}
	if *label != "us-review" {
		if err := o.gitea.move(is.Number, *label, "us-review"); err != nil {
			return "", err
		}
		*label = "us-review"
	}

	headTree, err := o.gitea.tree(head)
	if err != nil {
		return "", err
	}
	// The dev may merge main into the branch, which brings in the files merged by other stories meanwhile.
	mainTree, err := o.treeOf(o.base)
	if err != nil {
		return "", err
	}
	if touched := frozen(baseTree, testerTree, mainTree, headTree); len(touched) > 0 {
		return "You modified frozen acceptance tests: " + list(touched) + ". Restore them exactly as committed by the tester and make the code pass them instead.", nil
	}
	for _, prefix := range o.cfg.Protected {
		if touched := drifted(headTree, prefix, baseTree, mainTree); len(touched) > 0 {
			return "You modified protected paths: " + list(touched) + ". Revert these changes.", nil
		}
	}
	state, err := o.waitCI(head)
	if err != nil {
		return "", err
	}
	if state != "success" {
		return fmt.Sprintf("CI is `%s` on your last commit (%s): `make test` or an acceptance test `run.sh` fails. Reproduce and fix.", state, pr.HTMLURL), nil
	}

	ok, review, err := o.runAgent("reviewer", is, "")
	if err != nil || !ok {
		return "", agentFailure("reviewer", err)
	}
	// The reviewer may only read: what it approved must be what gets merged.
	if after, err := o.gitea.branchHead(branch); err != nil {
		return "", err
	} else if after != head {
		return "", errors.New("the reviewer pushed to the branch, which it must never do")
	}
	if approved, notes := verdict(review); !approved {
		return "The reviewer requested changes:\n\n" + notes, nil
	}

	return o.land(is, pr, branch, head)
}

// ensurePR returns the branch's pull request, opening it on the first attempt.
// A re-run of a blocked US force-pushes the same branch, so its pull request may already exist.
func (o *project) ensurePR(is issue, branch string) (*pullRequest, error) {
	pr, err := o.gitea.findPR(branch)
	if err != nil || pr != nil {
		return pr, err
	}
	if pr, err = o.gitea.openPR(is.Title, fmt.Sprintf("Closes #%d", is.Number), branch); err != nil {
		return nil, err
	}
	_, err = o.gitea.comment(is.Number, "🔀 Pull request opened: "+pr.HTMLURL)
	return pr, err
}

func (o *project) treeOf(branch string) (map[string]string, error) {
	sha, err := o.gitea.branchHead(branch)
	if err != nil {
		return nil, err
	}
	return o.gitea.tree(sha)
}

// waitCI polls the combined commit status of sha until CI reaches a final state.
func (o *project) waitCI(sha string) (string, error) {
	for deadline := time.Now().Add(ciTimeout); time.Now().Before(deadline); time.Sleep(o.cfg.PollEvery) {
		state, err := o.gitea.ciState(sha)
		if err != nil {
			return "", err
		}
		if state == "success" || state == "failure" || state == "error" || state == "warning" {
			return state, nil
		}
	}
	return "", fmt.Errorf("no CI result after %s: is the runner up?", ciTimeout)
}

func agentFailure(role string, err error) error {
	if err != nil {
		return fmt.Errorf("orchestrator error while running the %s: `%v`", role, err)
	}
	return fmt.Errorf("the %s agent failed or produced no commit (see its comment above)", role)
}
