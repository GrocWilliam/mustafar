package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePlan(t *testing.T) {
	ok := "Here is the plan.\n```json\n" + `{"stories": [
		{"key": "S1", "title": "Setup", "body": "Makefile", "depends_on": []},
		{"key": "S2", "title": "Feature", "body": "Criteria", "depends_on": ["S1"]}]}` + "\n```"
	stories, err := parsePlan(ok)
	if err != nil || len(stories) != 2 || stories[1].DependsOn[0] != "S1" {
		t.Fatalf("parsePlan = %+v, %v", stories, err)
	}

	// Fences inside story bodies, and an earlier ```json mention in the prose, must not break parsing.
	fenced := "I'll answer with a ```json block.\n```json\n" + `{"stories": [
		{"key": "S1", "title": "CLI", "body": "Run:\n` + "```bash\\npython3 -m greeter\\n```" + `\nAcceptance criteria:\n- prints Hello", "depends_on": []}]}` + "\n```\n"
	if stories, err := parsePlan(fenced); err != nil || len(stories) != 1 || !strings.Contains(stories[0].Body, "```bash") {
		t.Fatalf("fenced plan = %+v, %v", stories, err)
	}

	bad := map[string]string{
		"no block":          "no plan here",
		"invalid json":      "```json\n{nope}\n```",
		"empty":             "```json\n{\"stories\": []}\n```",
		"forward reference": "```json\n" + `{"stories": [{"key": "A", "title": "a", "body": "a", "depends_on": ["B"]}, {"key": "B", "title": "b", "body": "b"}]}` + "\n```",
		"duplicate key":     "```json\n" + `{"stories": [{"key": "A", "title": "a", "body": "a"}, {"key": "A", "title": "b", "body": "b"}]}` + "\n```",
		"missing title":     "```json\n" + `{"stories": [{"key": "A", "title": " ", "body": "a"}]}` + "\n```",
		"too many":          "```json\n{\"stories\": [" + strings.Repeat(`{"key": "x", "title": "x", "body": "x"},`, maxStories) + `{"key": "y", "title": "y", "body": "y"}]}` + "\n```",
	}
	for name, text := range bad {
		if _, err := parsePlan(text); err == nil {
			t.Errorf("%s: parsePlan accepted it", name)
		}
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
	comments := []string{"hello", "📋 Plan\n\n" + planMarkerFor([]int{8, 9, 10})}
	if got := planStories(comments); !reflect.DeepEqual(got, []int{8, 9, 10}) {
		t.Fatalf("planStories = %v", got)
	}
	costs := []string{"a <!-- forge:cost=0.2600 -->", "no cost", "b <!-- forge:cost=1.5 -->"}
	if got := sumCosts(costs); got < 1.759 || got > 1.761 {
		t.Fatalf("sumCosts = %v", got)
	}
}
