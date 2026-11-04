package main

import (
	"fmt"
	"log"
	"slices"
	"strings"
)

// planSpec runs the planner on a `spec` issue and turns its plan into `us-todo` issues.
// The planner only proposes: the orchestrator validates the plan and creates the issues itself.
func (o *project) planSpec(spec issue) error {
	log.Printf("%s: planning spec #%d", o.name, spec.Number)

	ok, final, err := o.runAgent("planner", spec, "")
	if err != nil || !ok {
		return o.block(spec.Number, "spec", agentFailure("planner", err).Error())
	}
	stories, questions, err := parsePlan(final)
	if err != nil {
		return o.block(spec.Number, "spec", "invalid plan: "+err.Error())
	}
	if len(questions) > 0 {
		// Cheaper to ask than to build every story on a wrong reading of the spec.
		return o.block(spec.Number, "spec", "❓ the planner needs answers before it can plan this spec:\n\n- "+
			strings.Join(questions, "\n- ")+"\n\nAnswer them in the spec's description: the planner reads only the description, not the comments.")
	}

	numbers := map[string]int{}
	var created []int
	var lines []string
	for _, story := range stories {
		body := fmt.Sprintf("%s\n\n---\nPart of #%d", strings.TrimSpace(story.Body), spec.Number)
		var deps []string
		for _, key := range story.DependsOn {
			body += fmt.Sprintf("\nDepends on #%d", numbers[key])
			deps = append(deps, fmt.Sprintf("#%d", numbers[key]))
		}
		// Unlabelled until the whole plan exists: nothing picks up a story of a half-created plan.
		number, err := o.gitea.createIssue(story.Title, body)
		if err != nil {
			return o.abandonPlan(spec.Number, created, fmt.Sprintf("creating the stories failed: `%v`", err))
		}
		numbers[story.Key] = number
		created = append(created, number)
		line := fmt.Sprintf("- #%d %s", number, story.Title)
		if len(deps) > 0 {
			line += " (after " + strings.Join(deps, ", ") + ")"
		}
		lines = append(lines, line)
	}

	// The spec leaves `spec` before any story can start, so a failure here never plans it twice.
	plan := fmt.Sprintf("📋 Plan: %d stories, built in this order.\n\n%s\n\n%s", len(created), strings.Join(lines, "\n"), planMarkerFor(created))
	if _, err := o.gitea.comment(spec.Number, plan); err != nil {
		return o.abandonPlan(spec.Number, created, fmt.Sprintf("posting the plan failed: `%v`", err))
	}
	if err := o.gitea.move(spec.Number, "spec", "spec-active"); err != nil {
		return o.abandonPlan(spec.Number, created, fmt.Sprintf("marking the spec active failed: `%v`", err))
	}
	for _, number := range created {
		if err := o.gitea.label(number, "us-todo"); err != nil {
			return o.block(spec.Number, "spec-active", fmt.Sprintf("labelling the stories failed: `%v`. Set `us-todo` on the plan's stories that lack it, then set `spec-active` again.", err))
		}
	}
	return nil
}

// abandonPlan closes the stories of a plan that could not be created in full, so that retrying the spec does not duplicate them.
func (o *project) abandonPlan(spec int, created []int, reason string) error {
	for _, number := range created {
		if err := o.gitea.closeIssue(number); err != nil {
			reason += fmt.Sprintf(" Closing the partial story #%d failed too: `%v`.", number, err)
		}
	}
	return o.block(spec, "spec", reason)
}

// readyStories returns the `us-todo` US whose dependencies are all closed, oldest first.
// On the way, it stops the specs that went over budget.
func (o *project) readyStories(todo []issue) ([]issue, error) {
	var ready []issue
	type spend struct{ cost, budget float64 }
	costs := map[int]spend{} // per spec, computed once per poll
	for _, story := range todo {
		if o.isRunning(story.Number) || !o.dependenciesClosed(story) {
			continue
		}
		if spec := refs(partOfLine, story.Body); len(spec) > 0 {
			s, known := costs[spec[0]]
			if !known {
				cost, budget, err := o.specCost(spec[0])
				if err != nil {
					return ready, err
				}
				s = spend{cost, budget}
				costs[spec[0]] = s
				if s.cost > s.budget {
					if err := o.stopSpec(spec[0], s.cost, s.budget); err != nil {
						return ready, err
					}
				}
			}
			if s.cost > s.budget {
				continue
			}
		}
		ready = append(ready, story)
	}
	slices.SortStableFunc(ready, byPriority)
	return ready, nil
}

// byPriority puts the stories labelled `priority` first, keeping the oldest-first order otherwise.
func byPriority(a, b issue) int {
	switch pa, pb := a.hasLabel("priority"), b.hasLabel("priority"); {
	case pa == pb:
		return 0
	case pa:
		return -1
	}
	return 1
}

func (o *project) dependenciesClosed(is issue) bool {
	for _, number := range refs(dependsLine, is.Body) {
		dep, err := o.gitea.getIssue(number)
		if err != nil {
			// A broken reference must not stall the whole queue: this US just waits.
			log.Printf("%s: US #%d: dependency #%d: %v", o.name, is.Number, number, err)
			return false
		}
		if dep.State != "closed" {
			return false
		}
	}
	return true
}

// closeFinishedSpecs closes every active spec whose stories are all closed, with a cost summary.
func (o *project) closeFinishedSpecs() error {
	specs, err := o.gitea.issuesWith("spec-active")
	if err != nil {
		return err
	}
	for _, spec := range specs {
		stories, err := o.storiesOf(spec.Number)
		if err != nil {
			return err
		}
		finished := true
		for _, number := range stories {
			if story, err := o.gitea.getIssue(number); err != nil {
				return err
			} else if story.State != "closed" {
				finished = false
				break
			}
		}
		if !finished {
			continue
		}
		cost, _, err := o.specCost(spec.Number)
		if err != nil {
			return err
		}
		msg := fmt.Sprintf("✅ All %d stories are done. Total agent cost: $%.2f.", len(stories), cost)
		if _, err := o.gitea.comment(spec.Number, msg); err != nil {
			return err
		}
		if err := o.gitea.closeIssue(spec.Number); err != nil {
			return err
		}
		log.Printf("%s: spec #%d done ($%.2f)", o.name, spec.Number, cost)
	}
	return nil
}

// stopSpec blocks a spec over budget and its stories still waiting in `us-todo`.
func (o *project) stopSpec(spec int, cost, budget float64) error {
	reason := fmt.Sprintf("spec #%d is over budget: $%.2f spent, its budget is $%.2f.", spec, cost, budget)
	log.Printf("%s: %s", o.name, reason)
	stories, err := o.storiesOf(spec)
	if err != nil {
		return err
	}
	for _, number := range stories {
		if story, err := o.gitea.getIssue(number); err != nil {
			return err
		} else if story.hasLabel("us-todo") && !o.isRunning(number) {
			if err := o.block(number, "us-todo", reason); err != nil {
				return err
			}
		}
	}
	return o.block(spec, "spec-active", reason+" Raise its budget from the forge UI (http://localhost:8000), or MAX_COST_PER_SPEC, then retry the spec.")
}

// storiesOf reads a spec's story numbers from its plan comment.
func (o *project) storiesOf(spec int) ([]int, error) {
	comments, err := o.gitea.comments(spec)
	if err != nil {
		return nil, err
	}
	return planStories(comments), nil
}

// specCost adds up the cost of every agent run on a spec and on its stories, retries included,
// and returns the spec's budget: MAX_COST_PER_SPEC, unless a budget marker on the spec raised it.
func (o *project) specCost(spec int) (cost, budget float64, err error) {
	comments, err := o.gitea.comments(spec)
	if err != nil {
		return 0, 0, err
	}
	cost = sumCosts(comments)
	for _, number := range planStories(comments) {
		storyComments, err := o.gitea.comments(number)
		if err != nil {
			return 0, 0, err
		}
		cost += sumCosts(storyComments)
	}
	return cost, specBudget(comments, o.settings().MaxCostPerSpec), nil
}
