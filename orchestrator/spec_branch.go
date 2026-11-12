package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// A spec's stories merge into its own branch, each tested alone, so specs built in parallel only meet once each is
// complete. The branch then goes through the whole acceptance suite and reaches main, and staging, in one pull request.

const specPrefix = "spec/"

func specBranch(spec int) string { return fmt.Sprintf("%s%d", specPrefix, spec) }

// targetOf returns the branch a story builds on and merges into: its open spec's branch, created on first use, or the
// default branch for a story of a spec already delivered.
func (o *project) targetOf(is issue) (string, error) {
	parent := refs(partOfLine, is.Body)
	if len(parent) == 0 {
		return o.base, nil
	}
	spec, err := o.gitea.getIssue(parent[0])
	if err != nil {
		return "", err
	}
	if spec.State != "open" {
		return o.base, nil
	}
	branch := specBranch(spec.Number)
	if err := o.gitea.ensureBranch(branch, o.base); err != nil {
		return "", err
	}
	// Agents hold a key that can push to any unprotected branch, and this one reaches main.
	if err := o.gitea.protect(branch); err != nil {
		return "", fmt.Errorf("protecting %s: %w", branch, err)
	}
	return branch, nil
}

// specFinished tells whether a spec can land: every story of its plan is closed, and so is any story added since.
func (o *project) specFinished(spec int) (bool, int, error) {
	stories, err := o.storiesOf(spec)
	if err != nil {
		return false, 0, err
	}
	for _, number := range stories {
		if story, err := o.gitea.getIssue(number); err != nil {
			return false, 0, err
		} else if story.State != "closed" {
			return false, 0, nil
		}
	}
	// An added story merges into this branch too: landing now would delete it under the story.
	for _, label := range []string{"us-todo", "us-in-progress", "us-review", "us-blocked"} {
		open, err := o.gitea.issuesWith(label)
		if err != nil {
			return false, 0, err
		}
		for _, story := range open {
			if parent := refs(partOfLine, story.Body); len(parent) > 0 && parent[0] == spec {
				return false, 0, nil
			}
		}
	}
	return true, len(stories), nil
}

// landSpec merges a finished spec's branch into main and closes the spec. Anything in the way blocks the spec, whose
// branch stays as it is for the retry.
func (o *project) landSpec(spec issue, stories int) error {
	spec.base = o.base
	branch := specBranch(spec.Number)
	ahead := 0 // no branch, or nothing on it: a spec planned before spec branches, or stories that changed nothing
	if _, err := o.gitea.branchHead(branch); err == nil {
		if ahead, err = o.gitea.behind(o.base, branch); err != nil {
			return err
		}
	} else if !hasStatus(err, http.StatusNotFound) {
		return err
	}
	if ahead > 0 {
		if err := o.deliverSpec(spec, branch); err != nil {
			log.Printf("%s: spec #%d blocked: %v", o.name, spec.Number, err)
			return o.block(spec.Number, "spec-active", fmt.Sprintf("`%s` could not land on `%s`: %s\n\n"+
				"The stories stay merged on `%[1]s`. Fix it, then retry the spec from the forge UI: the `forge-unblock` skill has the steps.", branch, o.base, strings.TrimSpace(err.Error())))
		}
	} else if err := o.gitea.closeIssue(spec.Number); err != nil {
		return err
	}
	cost, _, err := o.specCost(spec.Number)
	if err != nil {
		return err
	}
	if _, err := o.gitea.comment(spec.Number, fmt.Sprintf("✅ All %d stories are done. Total agent cost: $%.2f.", stories, cost)); err != nil {
		return err
	}
	log.Printf("%s: spec #%d done ($%.2f)", o.name, spec.Number, cost)
	// The spec is delivered: a branch left behind is only clutter, not worth blocking anything.
	if err := o.gitea.unprotect(branch); err != nil {
		log.Printf("%s: unprotecting %s: %v", o.name, branch, err)
	} else if err := o.gitea.deleteBranch(branch); err != nil {
		log.Printf("%s: deleting %s: %v", o.name, branch, err)
	}
	return nil
}

// deliverSpec takes a finished spec to main from a copy of its branch, the only place the integrator can push: CI runs
// the whole acceptance suite there, and the integrator runs only for what stands in the way, a failure or a conflict.
func (o *project) deliverSpec(spec issue, branch string) error {
	work := fmt.Sprintf("agent/spec-%d", spec.Number)
	pr, err := o.openDelivery(spec, branch, work)
	if err != nil {
		return err
	}
	for run := 1; ; run++ {
		problem, err := o.tryLand(spec, pr, work)
		if err != nil || problem == "" {
			return err
		}
		if run > maxAttempts {
			return fmt.Errorf("still failing after %d integrator runs. Last problem:\n\n%s", maxAttempts, problem)
		}
		msg := fmt.Sprintf("🔁 Integrator run %d/%d, for:\n\n%s", run, maxAttempts, problem)
		if _, err := o.gitea.comment(spec.Number, msg); err != nil {
			return err
		}
		if err := o.integrate(spec, branch, work, problem); err != nil {
			return err
		}
	}
}

// openDelivery starts a delivery from the spec branch as it is now: a retry may follow a fix merged there.
func (o *project) openDelivery(spec issue, branch, work string) (*pullRequest, error) {
	stale, err := o.gitea.findPR(work)
	if err != nil {
		return nil, err
	}
	if stale != nil {
		if err := o.gitea.closePR(stale.Number); err != nil {
			return nil, err
		}
	}
	if err := o.gitea.deleteBranch(work); err != nil {
		return nil, err
	}
	if err := o.gitea.ensureBranch(work, branch); err != nil {
		return nil, err
	}
	pr, err := o.gitea.openPR(spec.Title, fmt.Sprintf("Delivers spec #%d: every story is merged on `%s`.", spec.Number, branch), work, o.base)
	if err != nil {
		return nil, err
	}
	_, err = o.gitea.comment(spec.Number, "🔀 All stories are merged, running every acceptance test before `"+o.base+"`: "+pr.HTMLURL)
	return pr, err
}

// tryLand waits for the whole suite on the delivery branch, then lands it. It returns what stands in the way, if anything.
func (o *project) tryLand(spec issue, pr *pullRequest, work string) (string, error) {
	head, err := o.gitea.branchHead(work)
	if err != nil {
		return "", err
	}
	state, err := o.waitCI(head)
	if err != nil {
		return "", err
	}
	if state != "success" {
		return fmt.Sprintf("The full test suite is `%s` on this branch (%s).", state, pr.HTMLURL) + o.ciLog(head), nil
	}
	return o.land(spec, pr, work, head)
}

// integrate runs the integrator, then holds it to the rules: it fixes code, and may not make a test pass by editing it.
func (o *project) integrate(spec issue, branch, work, problem string) error {
	before, err := o.treeOf(work)
	if err != nil {
		return err
	}
	if ok, _, err := o.runAgent("integrator", spec, problem); err != nil || !ok {
		return agentFailure("integrator", err)
	}
	after, err := o.treeOf(work)
	if err != nil {
		return err
	}
	specTree, err := o.treeOf(branch)
	if err != nil {
		return err
	}
	mainTree, err := o.treeOf(o.base)
	if err != nil {
		return err
	}
	for _, prefix := range append([]string{acceptanceDir}, o.cfg.Protected...) {
		if touched := outside(drifted(after, prefix, before, specTree, mainTree), supportDir); len(touched) > 0 {
			return fmt.Errorf("the integrator changed frozen or protected files: %s", list(touched))
		}
	}
	return nil
}
