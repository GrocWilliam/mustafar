package main

import (
	"fmt"
	"log"
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
	stories, err := parsePlan(final)
	if err != nil {
		return o.block(spec.Number, "spec", "invalid plan: "+err.Error())
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

	for _, number := range created {
		if err := o.gitea.label(number, "us-todo"); err != nil {
			return o.abandonPlan(spec.Number, created, fmt.Sprintf("labelling the stories failed: `%v`", err))
		}
	}
	plan := fmt.Sprintf("📋 Plan: %d stories, built in this order.\n\n%s\n\n%s", len(created), strings.Join(lines, "\n"), planMarkerFor(created))
	if _, err := o.gitea.comment(spec.Number, plan); err != nil {
		return err
	}
	return o.gitea.move(spec.Number, "spec", "spec-active")
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
	costs := map[int]float64{} // per spec, computed once per poll
	for _, story := range todo {
		if o.isRunning(story.Number) || !o.dependenciesClosed(story) {
			continue
		}
		if spec := refs(partOfLine, story.Body); len(spec) > 0 {
			cost, known := costs[spec[0]]
			if !known {
				var err error
				if cost, err = o.specCost(spec[0]); err != nil {
					return ready, err
				}
				costs[spec[0]] = cost
				if cost > o.cfg.MaxCostPerSpec {
					if err := o.stopSpec(spec[0], cost); err != nil {
						return ready, err
					}
				}
			}
			if cost > o.cfg.MaxCostPerSpec {
				continue
			}
		}
		ready = append(ready, story)
	}
	return ready, nil
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
		cost, err := o.specCost(spec.Number)
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
func (o *project) stopSpec(spec int, cost float64) error {
	reason := fmt.Sprintf("spec #%d is over budget: $%.2f spent, MAX_COST_PER_SPEC is $%.2f.", spec, cost, o.cfg.MaxCostPerSpec)
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
	return o.block(spec, "spec-active", reason+" Raise the budget, then set `spec-active` again and `us-todo` on its blocked stories.")
}

// storiesOf reads a spec's story numbers from its plan comment.
func (o *project) storiesOf(spec int) ([]int, error) {
	comments, err := o.gitea.comments(spec)
	if err != nil {
		return nil, err
	}
	return planStories(comments), nil
}

// specCost adds up the cost of every agent run on a spec and on its stories, retries included.
func (o *project) specCost(spec int) (float64, error) {
	comments, err := o.gitea.comments(spec)
	if err != nil {
		return 0, err
	}
	total := sumCosts(comments)
	for _, number := range planStories(comments) {
		storyComments, err := o.gitea.comments(number)
		if err != nil {
			return 0, err
		}
		total += sumCosts(storyComments)
	}
	return total, nil
}
