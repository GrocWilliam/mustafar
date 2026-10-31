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
)

// parsePlan extracts the stories from the last ```json block of the planner's final message and validates them.
func parsePlan(text string) ([]plannedStory, error) {
	var plan struct {
		Stories []plannedStory `json:"stories"`
	}
	// The JSON decoder finds where the value ends: story bodies may contain ``` fences, which a regex would stop at.
	err := errors.New("no ```json block in the planner's final message")
	for end := len(text); err != nil; {
		start := strings.LastIndex(text[:end], "```json")
		if start < 0 {
			return nil, fmt.Errorf("plan JSON: %v", err)
		}
		plan.Stories = nil
		err = json.NewDecoder(strings.NewReader(text[start+len("```json"):])).Decode(&plan)
		end = start
	}
	if n := len(plan.Stories); n == 0 || n > maxStories {
		return nil, fmt.Errorf("the plan has %d stories, expected 1 to %d", n, maxStories)
	}
	seen := map[string]bool{}
	for _, story := range plan.Stories {
		if story.Key == "" || strings.TrimSpace(story.Title) == "" || strings.TrimSpace(story.Body) == "" {
			return nil, fmt.Errorf("story %q lacks a key, a title or a body", story.Key)
		}
		if seen[story.Key] {
			return nil, fmt.Errorf("story key %q is used twice", story.Key)
		}
		// Dependencies on earlier stories only: the list order is a valid build order, and there are no cycles.
		for _, dep := range story.DependsOn {
			if !seen[dep] {
				return nil, fmt.Errorf("story %q depends on %q, which is not an earlier story", story.Key, dep)
			}
		}
		seen[story.Key] = true
	}
	return plan.Stories, nil
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

// planStories reads the story numbers from the first plan marker found in a spec's comments.
func planStories(comments []string) []int {
	for _, body := range comments {
		if match := planMarker.FindStringSubmatch(body); match != nil {
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
