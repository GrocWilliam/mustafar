package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maxAttempts   = 3
	maxRevisions  = 3 // tester revisions per story: a story that moves things can break several earlier tests in turn
	ciTimeout     = 90 * time.Minute // a busy runner queues jobs: a story waits for its 7 jobs behind other stories' ones
	acceptanceDir = "tests/acceptance/"
	// supportDir holds the acceptance tests' shared domain layer (page objects, API setup): not frozen, so a UI change
	// updates it once instead of every test. shortcut: the reviewer alone checks a support change keeps each action's
	// meaning; diff the support layer against the frozen tests' expectations if devs ever weaken tests through it.
	supportDir = acceptanceDir + "support/"
)

// contract is what a story's checks compare against: the tree the tester started from, and the tree holding its
// tests (its last commit, or its latest revision). revisions counts the tester's revisions, up to maxRevisions.
type contract struct {
	base, tests map[string]string
	revisions   int
}

// conflictLine is how the dev reports a test that cannot pass (agents/prompts/dev.md).
var conflictLine = regexp.MustCompile(`(?m)^\W*TEST CONFLICT:`)

// ciFailed starts the feedback a dev gets after a CI failure.
const ciFailed = "CI is `"

// What follows a dev run, besides the checks.
const (
	goOn   = iota
	revise // send the tests back to the tester
	stuck  // nothing can change: the next attempts would check the same commit
)

// next decides what follows a dev run. A dev that adds no commit after a CI failure is stuck on the tests, whether
// or not it wrote a `TEST CONFLICT:` line: the tester revises them, or, its revisions used up, the story stops.
func next(final, feedback string, committed bool, revisions int) int {
	unchanged := !committed && strings.HasPrefix(feedback, ciFailed)
	switch {
	case (conflictLine.MatchString(final) || unchanged) && revisions < maxRevisions:
		return revise
	case unchanged:
		return stuck
	}
	return goOn
}

// deliver takes a US from acceptance tests to merge: tester, then up to maxAttempts dev cycles.
// Agents write code; only the checks here decide. label tracks the US's current label for block().
// Attempts and feedback live in memory: a restart re-queues the US, which resumes its branch if the dev had started.
func (o *project) deliver(is issue, label *string) error {
	branch := fmt.Sprintf("agent/us-%d", is.Number)
	var err error
	if is.base, err = o.targetOf(is); err != nil {
		return err
	}
	c, resumed, err := o.resume(is, branch)
	if err != nil {
		return err
	}
	if !resumed {
		if err := o.test(is, branch, &c); err != nil {
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
		// A resumed branch may already hold finished work (e.g. a restart between CI and review): check it before rerunning the dev.
		skipDev := resumed && attempt == 1
		if feedback, err = o.attempt(is, branch, &c, feedback, label, skipDev); err != nil || feedback == "" {
			return err
		}
	}
	return fmt.Errorf("still rejected after %d attempts. Last feedback:\n\n%s", maxAttempts, feedback)
}

// attempt runs one dev cycle and its checks (only the checks when skipDev). It returns the feedback for the next cycle, or "" once merged.
// An error means the US cannot go on (infrastructure failure or misbehaving agent).
func (o *project) attempt(is issue, branch string, c *contract, feedback string, label *string, skipDev bool) (string, error) {
	// The next dev run starts a fresh session: what this one said it did and left undone spares it re-exploring.
	summary := ""
	retry := func(msg string) (string, error) {
		if summary != "" {
			msg += "\n\nYour previous run ended with:\n\n" + summary
		}
		return msg, nil
	}
	if !skipDev {
		before, err := o.gitea.branchHead(branch)
		if err != nil {
			return "", err
		}
		ok, final, err := o.runAgent("dev", is, feedback)
		if err != nil {
			return "", err
		}
		after, err := o.gitea.branchHead(branch)
		if err != nil {
			return "", err
		}
		summary = clip(strings.TrimSpace(final), 3000)
		switch next(final, feedback, after != before, c.revisions) {
		case revise:
			report := conflictReport(final)
			if !conflictLine.MatchString(final) {
				report = "The dev added no commit after this CI failure: it could not make the tests pass. Check whether the failing tests contradict the story, pin what an earlier story defined that this one changes, or are flaky.\n\n" +
					clip(feedback, 8000) + "\n\nThe dev's summary:\n\n" + summary
			}
			if rejected, err := o.revise(is, branch, c, report); err != nil {
				return "", err
			} else if rejected != "" {
				return rejected, nil
			}
		case stuck:
			return "", fmt.Errorf("the dev added no commit after a CI failure, and the tester already revised the tests %d times: the tests or the story need a human. Last CI feedback:\n\n%s", c.revisions, clip(feedback, 3000))
		case goOn:
			if !ok {
				// Its own explanation (a contradiction in the tests, a missing tool...) beats a generic retry message.
				return retry("Your previous run stopped before finishing (it failed, or produced no new commit). Whatever it had done is committed on the branch: continue from it, make the acceptance tests pass and commit your work.")
			}
		}
	}
	pr, err := o.ensurePR(is, branch)
	if err != nil {
		return "", err
	}
	if skipDev {
		// Gitea never reruns CI on an unchanged commit: a retry after a flaky failure needs a new head to get a fresh run.
		if conflict, err := o.freshCI(is, branch, pr); err != nil {
			return "", err
		} else if conflict {
			head0, err := o.gitea.branchHead(branch)
			if err != nil {
				return "", err
			}
			// Without this the dev gets the stale CI failure, finds nothing to fix, and spends every attempt on it.
			return retry(fmt.Sprintf("CI had failed on this branch, and `%s` has moved since: it conflicts with your branch, so no fresh CI run could start. Merge `origin/%s` into your branch, resolve the conflicts so both sides keep working, run the tests and commit.%s", is.base, is.base, o.ciLog(head0)))
		}
	}
	head, err := o.gitea.branchHead(branch)
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
	// The dev may merge the target into the branch, which brings in the files merged by other stories meanwhile.
	targetTree, err := o.treeOf(is.base)
	if err != nil {
		return "", err
	}
	if touched := frozen(c.base, c.tests, targetTree, headTree); len(touched) > 0 {
		return retry("You modified frozen acceptance tests: " + list(touched) + ". Restore them exactly as committed by the tester and make the code pass them instead.")
	}
	for _, prefix := range o.cfg.Protected {
		if touched := drifted(headTree, prefix, c.base, targetTree); len(touched) > 0 {
			return retry("You modified protected paths: " + list(touched) + ". Revert these changes.")
		}
	}
	state, err := o.waitCI(head)
	if err != nil {
		return "", err
	}
	if state != "success" {
		return retry(fmt.Sprintf("CI is `%s` on your last commit (%s): `make test` or an acceptance test `run.sh` fails. Reproduce and fix.", state, pr.HTMLURL) + o.ciLog(head))
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
		return retry("The reviewer requested changes:\n\n" + notes)
	}

	return o.land(is, pr, branch, head)
}

// conflictReport puts the dev's `TEST CONFLICT:` lines first, where clipping its summary cannot cut them off.
func conflictReport(final string) string {
	var lines []string
	for _, line := range strings.Split(final, "\n") {
		if conflictLine.MatchString(line) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return strings.Join(lines, "\n") + "\n\nThe dev's summary:\n\n" + clip(strings.TrimSpace(final), 3000)
}

// test runs the tester on a fresh branch and checks what it committed. A resumed branch passed these checks when
// its tester ran, and a revision may follow them since: its trees hold the dev's work too.
func (o *project) test(is issue, branch string, c *contract) error {
	var err error
	if c.base, err = o.treeOf(is.base); err != nil {
		return err
	}
	if ok, _, err := o.runAgent("tester", is, ""); err != nil || !ok {
		return agentFailure("tester", err)
	}
	if c.tests, err = o.treeOf(branch); err != nil {
		return err
	}
	ownDir := fmt.Sprintf("%sus-%d/", acceptanceDir, is.Number)
	if stray := outside(changed(c.base, c.tests, ""), acceptanceDir); len(stray) > 0 {
		return fmt.Errorf("the tester changed files outside `%s`: %s", acceptanceDir, list(stray))
	}
	// On a spec's branch this run.sh is all CI runs for the story: without one, the story would pass on nothing.
	if _, ok := c.tests[ownDir+"run.sh"]; !ok || len(changed(c.base, c.tests, ownDir)) == 0 {
		return fmt.Errorf("the tester wrote no acceptance tests in `%s`, or no `run.sh`", ownDir)
	}
	// A story may change behavior that earlier stories' tests pin down: the tester updates those tests, visibly.
	if earlier := outside(changed(c.base, c.tests, acceptanceDir), ownDir); len(earlier) > 0 {
		msg := "🧪 This story supersedes earlier acceptance tests, which the tester updated: " + list(earlier) + ". The reviewer checks that the story requires it."
		if _, err := o.gitea.comment(is.Number, msg); err != nil {
			return err
		}
	}
	return nil
}

// revise runs the tester again on the tests the dev reported as conflicting or could not make pass (report: why). The checks
// then hold the dev to the revised tests. It returns the dev's feedback when the revision cannot go ahead or the
// tester kept the tests, "" to go on with the checks.
func (o *project) revise(is issue, branch string, c *contract, report string) (string, error) {
	before, err := o.treeOf(branch)
	if err != nil {
		return "", err
	}
	targetTree, err := o.treeOf(is.base)
	if err != nil {
		return "", err
	}
	// The revision would adopt the dev's own edits of the tests as the tester's.
	if touched := frozen(c.base, c.tests, targetTree, before); len(touched) > 0 {
		return "You modified frozen acceptance tests: " + list(touched) + ". Restore them exactly as committed by the tester; report a conflict with a `TEST CONFLICT:` line instead of editing the test.", nil
	}
	c.revisions++ // counted whatever the tester decides
	ok, final, err := o.runAgent("retester", is, report)
	if err != nil || !ok {
		return "", agentFailure("retester", err)
	}
	after, err := o.treeOf(branch)
	if err != nil {
		return "", err
	}
	if stray := outside(changed(before, after, ""), acceptanceDir); len(stray) > 0 {
		return "", fmt.Errorf("the tester's revision changed files outside `%s`: %s", acceptanceDir, list(stray))
	}
	revised := changed(before, after, acceptanceDir)
	if len(revised) == 0 {
		return "The tester checked the test conflicts you reported and kept the tests: fix the code. Its answer:\n\n" + clip(strings.TrimSpace(final), 3000), nil
	}
	c.tests = after
	msg := "🧪 The dev reported a test conflict: the tester revised " + list(revised) + ". The reviewer checks that the story requires it."
	_, err = o.gitea.comment(is.Number, msg)
	return "", err
}

// ensurePR returns the branch's pull request, opening it on the first attempt.
// A re-run of a blocked US force-pushes the same branch, so its pull request may already exist, with another target
// if its spec was delivered or got its branch since.
func (o *project) ensurePR(is issue, branch string) (*pullRequest, error) {
	pr, err := o.gitea.findPR(branch)
	if err != nil {
		return nil, err
	}
	if pr != nil {
		if pr.Base.Ref != is.base {
			err = o.gitea.retarget(pr.Number, is.base)
		}
		return pr, err
	}
	if pr, err = o.gitea.openPR(is.Title, fmt.Sprintf("Closes #%d", is.Number), branch, is.base); err != nil {
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
	for deadline := time.Now().Add(ciTimeout); time.Now().Before(deadline); time.Sleep(o.settings().pollEvery()) {
		state, err := o.gitea.ciState(sha)
		if err != nil {
			return "", err
		}
		if slices.Contains([]string{"success", "failure", "error", "warning"}, state) {
			return state, nil
		}
	}
	return "", fmt.Errorf("no CI result after %s: is the runner up?", ciTimeout)
}

// freshCI merges the base branch into a resumed branch whose last CI failed, so CI runs again on the new merge commit.
// It does nothing when CI did not fail or the branch already contains the base: the dev then gets the failure.
// It reports a conflict when the target cannot be merged in: the dev has to merge it.
func (o *project) freshCI(is issue, branch string, pr *pullRequest) (conflict bool, err error) {
	head, err := o.gitea.branchHead(branch)
	if err != nil {
		return false, err
	}
	if state, err := o.gitea.ciState(head); err != nil || (state != "failure" && state != "error") {
		return false, err
	}
	behind, err := o.gitea.behind(branch, is.base)
	if err != nil || behind == 0 {
		return false, err
	}
	if err := o.gitea.updatePR(pr.Number); errors.Is(err, errConflict) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	_, err = o.gitea.comment(is.Number, fmt.Sprintf("🔄 CI had failed on the resumed branch: merged `%s` into it for a fresh CI run.", is.base))
	return false, err
}

// ciLog is the end of the failed CI jobs' logs on sha, as feedback for the dev; empty if Gitea cannot give it.
func (o *project) ciLog(sha string) string {
	log, err := o.gitea.failedCILog(sha, 60)
	if err != nil || log == "" {
		return ""
	}
	return "\n\nEnd of the failed CI job's log:\n\n```\n" + clip(log, 6000) + "\n```"
}

func agentFailure(role string, err error) error {
	if err != nil {
		return fmt.Errorf("orchestrator error while running the %s: `%v`", role, err)
	}
	return fmt.Errorf("the %s agent failed or produced no commit (see its comment above)", role)
}
