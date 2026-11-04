package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Pure parsing behind planning: the planner's JSON plan, and the references the forge writes in Gitea.

const maxStories = 20

type plannedStory struct {
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	DependsOn []string `json:"depends_on"`
}

var (
	dependsLine = regexp.MustCompile(`(?mi)^depends on #(\d+)`)
	partOfLine  = regexp.MustCompile(`(?mi)^part of #(\d+)`)
	planMarker  = regexp.MustCompile(`<!-- forge:stories ([\d,]+) -->`)
	costMarker  = regexp.MustCompile(`<!-- forge:cost=([\d.]+) -->`)
	// budgetMarker on a spec comment overrides MAX_COST_PER_SPEC for that spec; the last one wins.
	budgetMarker = regexp.MustCompile(`<!-- forge:budget=([\d.]+) -->`)
)

// parsePlan extracts the stories from the last ```json block of the planner's final message and validates them.
// A planner that finds the spec too ambiguous to plan returns questions instead, for the human to answer in the spec.
func parsePlan(text string) ([]plannedStory, []string, error) {
	var plan struct {
		Stories   []plannedStory `json:"stories"`
		Questions []string       `json:"questions"`
	}
	// The JSON decoder finds where the value ends: story bodies may contain ``` fences, which a regex would stop at.
	err := errors.New("no ```json block in the planner's final message")
	for end := len(text); err != nil; {
		start := strings.LastIndex(text[:end], "```json")
		if start < 0 {
			return nil, nil, fmt.Errorf("plan JSON: %v", err)
		}
		plan.Stories, plan.Questions = nil, nil
		err = json.NewDecoder(strings.NewReader(text[start+len("```json"):])).Decode(&plan)
		end = start
	}
	var questions []string
	for _, q := range plan.Questions {
		if q = strings.TrimSpace(q); q != "" {
			questions = append(questions, q)
		}
	}
	if len(questions) > 0 {
		if len(plan.Stories) > 0 {
			return nil, nil, errors.New("the plan has both stories and questions: expected one or the other")
		}
		return nil, questions, nil
	}
	if n := len(plan.Stories); n == 0 || n > maxStories {
		return nil, nil, fmt.Errorf("the plan has %d stories, expected 1 to %d", n, maxStories)
	}
	seen := map[string]bool{}
	for _, story := range plan.Stories {
		if story.Key == "" || strings.TrimSpace(story.Title) == "" || strings.TrimSpace(story.Body) == "" {
			return nil, nil, fmt.Errorf("story %q lacks a key, a title or a body", story.Key)
		}
		if seen[story.Key] {
			return nil, nil, fmt.Errorf("story key %q is used twice", story.Key)
		}
		// Dependencies on earlier stories only: the list order is a valid build order, and there are no cycles.
		for _, dep := range story.DependsOn {
			if !seen[dep] {
				return nil, nil, fmt.Errorf("story %q depends on %q, which is not an earlier story", story.Key, dep)
			}
		}
		seen[story.Key] = true
	}
	return plan.Stories, nil, nil
}

// refs returns the issue numbers captured by re in text.
func refs(re *regexp.Regexp, text string) []int {
	var numbers []int
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.Atoi(match[1]); err == nil {
			numbers = append(numbers, n)
		}
	}
	return numbers
}

func planMarkerFor(stories []int) string {
	parts := make([]string, len(stories))
	for i, n := range stories {
		parts[i] = strconv.Itoa(n)
	}
	return "<!-- forge:stories " + strings.Join(parts, ",") + " -->"
}

// planStories reads the story numbers from the last plan marker in a spec's comments: a retried plan replaces an abandoned one.
func planStories(comments []string) []int {
	for i := len(comments) - 1; i >= 0; i-- {
		if match := planMarker.FindStringSubmatch(comments[i]); match != nil {
			var numbers []int
			for _, part := range strings.Split(match[1], ",") {
				if n, err := strconv.Atoi(part); err == nil {
					numbers = append(numbers, n)
				}
			}
			return numbers
		}
	}
	return nil
}

func sumCosts(comments []string) float64 {
	total := 0.0
	for _, body := range comments {
		for _, match := range costMarker.FindAllStringSubmatch(body, -1) {
			if cost, err := strconv.ParseFloat(match[1], 64); err == nil {
				total += cost
			}
		}
	}
	return total
}

// specBudget returns the budget set by the last budget marker in a spec's comments, or fallback.
func specBudget(comments []string, fallback float64) float64 {
	for i := len(comments) - 1; i >= 0; i-- {
		if match := budgetMarker.FindStringSubmatch(comments[i]); match != nil {
			if budget, err := strconv.ParseFloat(match[1], 64); err == nil {
				return budget
			}
		}
	}
	return fallback
}

func budgetMarkerFor(budget float64) string {
	return fmt.Sprintf("<!-- forge:budget=%.2f -->", budget)
}
