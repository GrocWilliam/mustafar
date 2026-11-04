package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// criteria gives every fixture story a valid criteria list, so each bad plan fails for its own fault only.
func criteria(plan string) string {
	return strings.ReplaceAll(plan, `"body": "`, `"body": "Acceptance criteria:\n- it works\n`)
}

func TestParsePlan(t *testing.T) {
	ok := criteria("Here is the plan.\n```json\n" + `{"stories": [
		{"key": "S1", "title": "Setup", "body": "Makefile", "depends_on": []},
		{"key": "S2", "title": "Feature", "body": "Criteria", "depends_on": ["S1"]}]}` + "\n```")
	stories, _, err := parsePlan(ok)
	if err != nil || len(stories) != 2 || stories[1].DependsOn[0] != "S1" {
		t.Fatalf("parsePlan = %+v, %v", stories, err)
	}

	// Fences inside story bodies, and an earlier ```json mention in the prose, must not break parsing.
	fenced := "I'll answer with a ```json block.\n```json\n" + `{"stories": [
		{"key": "S1", "title": "CLI", "body": "Run:\n` + "```bash\\npython3 -m greeter\\n```" + `\nAcceptance criteria:\n- prints Hello", "depends_on": []}]}` + "\n```\n"
	if stories, _, err := parsePlan(fenced); err != nil || len(stories) != 1 || !strings.Contains(stories[0].Body, "```bash") {
		t.Fatalf("fenced plan = %+v, %v", stories, err)
	}

	bad := map[string]string{
		"no block":              "no plan here",
		"invalid json":          "```json\n{nope}\n```",
		"empty":                 "```json\n{\"stories\": []}\n```",
		"forward reference":     "```json\n" + `{"stories": [{"key": "A", "title": "a", "body": "a", "depends_on": ["B"]}, {"key": "B", "title": "b", "body": "b"}]}` + "\n```",
		"duplicate key":         "```json\n" + `{"stories": [{"key": "A", "title": "a", "body": "a"}, {"key": "A", "title": "b", "body": "b"}]}` + "\n```",
		"missing title":         "```json\n" + `{"stories": [{"key": "A", "title": " ", "body": "a"}]}` + "\n```",
		"stories and questions": "```json\n" + `{"stories": [{"key": "A", "title": "a", "body": "a"}], "questions": ["Which?"]}` + "\n```",
		"blank questions":       "```json\n{\"questions\": [\" \"]}\n```",
		"too many":              "```json\n{\"stories\": [" + strings.Repeat(`{"key": "x", "title": "x", "body": "x"},`, maxStories) + `{"key": "y", "title": "y", "body": "y"}]}` + "\n```",
	}
	for name, text := range bad {
		if _, _, err := parsePlan(criteria(text)); err == nil {
			t.Errorf("%s: parsePlan accepted it", name)
		}
	}
}

func TestParsePlanCriteria(t *testing.T) {
	plan := func(body string) string {
		return "```json\n" + `{"stories": [{"key": "A", "title": "a", "body": "` + body + `"}]}` + "\n```"
	}
	for _, body := range []string{`Make it work.`, `Acceptance criteria:\n\nLater.`} {
		if _, _, err := parsePlan(plan(body)); err == nil || !strings.Contains(err.Error(), "acceptance criteria") {
			t.Errorf("%q: parsePlan = %v, want a missing criteria error", body, err)
		}
	}
	for _, body := range []string{`**Acceptance criteria:**\n- prints Hello`, `Acceptance Criteria\n1. prints Hello`} {
		if _, _, err := parsePlan(plan(body)); err != nil {
			t.Errorf("%q: parsePlan = %v", body, err)
		}
	}
}

func TestParsePlanQuestions(t *testing.T) {
	text := "The spec is ambiguous.\n```json\n" + `{"questions": ["Public, or logged-in users only?", " "]}` + "\n```"
	stories, questions, err := parsePlan(text)
	if err != nil || stories != nil || !reflect.DeepEqual(questions, []string{"Public, or logged-in users only?"}) {
		t.Fatalf("parsePlan = %+v, %q, %v", stories, questions, err)
	}
}

func TestRefsAndMarkers(t *testing.T) {
	body := "Do it.\n\n---\nPart of #7\nDepends on #8\ndepends on #9\nnot a Depends on #10"
	if got := refs(dependsLine, body); !reflect.DeepEqual(got, []int{8, 9}) {
		t.Fatalf("depends = %v", got)
	}
	if got := refs(partOfLine, body); !reflect.DeepEqual(got, []int{7}) {
		t.Fatalf("part of = %v", got)
	}
	comments := []string{"hello", "abandoned " + planMarkerFor([]int{5, 6}), "📋 Plan\n\n" + planMarkerFor([]int{8, 9, 10}), "later"}
	if got := planStories(comments); !reflect.DeepEqual(got, []int{8, 9, 10}) {
		t.Fatalf("planStories = %v", got)
	}
	costs := []string{"a <!-- forge:cost=0.2600 -->", "no cost", "b <!-- forge:cost=1.5 -->"}
	if got := sumCosts(costs); got < 1.759 || got > 1.761 {
		t.Fatalf("sumCosts = %v", got)
	}
}

func TestByPriority(t *testing.T) {
	story := func(n int, labels ...string) issue {
		is := issue{Number: n}
		for _, l := range labels {
			is.Labels = append(is.Labels, struct {
				Name string `json:"name"`
			}{l})
		}
		return is
	}
	ready := []issue{story(3, "us-todo"), story(5, "us-todo", "priority"), story(4, "us-todo"), story(9, "priority", "us-todo")}
	slices.SortStableFunc(ready, byPriority)
	var got []int
	for _, is := range ready {
		got = append(got, is.Number)
	}
	if want := []int{5, 9, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
