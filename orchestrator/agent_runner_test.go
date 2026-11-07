package main

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseAndDescribe(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[
		{"type":"text","text":"Let me look\n at the code."},
		{"type":"tool_use","name":"Bash","input":{"command":"npm test"}},
		{"type":"tool_use","name":"Edit","input":{"file_path":"src/app.js"}},
		{"type":"tool_use","name":"TodoWrite","input":{"todos":[]}}]}}`
	got := describe(parse([]byte(line)))
	want := []string{"Let me look at the code.", "`Bash` npm test", "`Edit` src/app.js", "`TodoWrite`"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("describe = %q, want %q", got, want)
	}

	result := parse([]byte(`{"type":"result","result":"done","num_turns":3,"total_cost_usd":0.12}`))
	if result.Type != "result" || result.NumTurns != 3 || result.TotalCostUSD != 0.12 || describe(result) != nil {
		t.Fatalf("result event = %+v", result)
	}
	// user events carry string content sometimes; they must not break parsing.
	if describe(parse([]byte(`{"type":"user","message":{"content":"hi"}}`))) != nil {
		t.Fatal("user event described")
	}
	if parse([]byte("not json")).Type != "" {
		t.Fatal("garbage parsed")
	}
}

func TestSummaryStatus(t *testing.T) {
	failed := parse([]byte(`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":30,"total_cost_usd":1.5}`))
	cases := []struct {
		exit   int
		result *streamEvent
		want   []string
	}{
		{0, &streamEvent{Subtype: "success"}, []string{"✅ finished", "forge:cost="}},
		{1, &failed, []string{"exit code 1", "`error_max_turns`", "cost: $1.50", "forge:cost=1.5000"}},
		{124, nil, []string{"timed out after 45m", "not counted"}},
	}
	for _, c := range cases {
		got := summary("dev", nil, c.result, c.exit, "")
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("summary(exit %d) = %q, missing %q", c.exit, got, want)
			}
		}
	}
	if got := summary("dev", nil, &streamEvent{Subtype: "success"}, 0, ""); strings.Contains(got, "Claude Code: `") {
		t.Errorf("success summary shows a subtype: %q", got)
	}
}

func TestShort(t *testing.T) {
	if got := short(strings.Repeat("é", 200), 10); got != strings.Repeat("é", 9)+"…" {
		t.Fatalf("short = %q", got)
	}
}

func TestDemux(t *testing.T) {
	var stream bytes.Buffer
	for _, payload := range []string{"line one\nline ", "two\n"} {
		header := make([]byte, 8)
		header[0] = 1 // stdout
		binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
		stream.Write(header)
		stream.WriteString(payload)
	}
	var out bytes.Buffer
	if err := demux(&stream, &out); err != nil || out.String() != "line one\nline two\n" {
		t.Fatalf("demux = %q, %v", out.String(), err)
	}
}

func TestLimitReset(t *testing.T) {
	now := time.Date(2026, 11, 2, 11, 52, 0, 0, time.UTC)
	cases := []struct {
		final string
		want  time.Time
		ok    bool
	}{
		{"You've hit your 5-hour limit · resets 1:40pm (UTC)", time.Date(2026, 11, 2, 13, 41, 0, 0, time.UTC), true},
		{"You've hit your 5-hour limit · resets 9am (UTC)", time.Date(2026, 11, 3, 9, 1, 0, 0, time.UTC), true},
		{"You've hit your limit · resets 1:30pm (Europe/Paris)", time.Date(2026, 11, 2, 12, 31, 0, 0, time.UTC), true},
		{"You've hit your weekly limit", now.Add(limitWait), true},
		{"All tests pass.", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := limitReset(c.final, now)
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("limitReset(%q) = %v, %v; want %v, %v", c.final, got, ok, c.want, c.ok)
		}
	}
}

func TestHandoff(t *testing.T) {
	steps := []string{"`Read` src/app.ts", "`Bash` make test"}
	if got := handoff("Done: added X.", 1, steps); got != "Done: added X." {
		t.Errorf("a final message is kept: %q", got)
	}
	if got := handoff("", 0, steps); got != "" {
		t.Errorf("a successful run needs no handoff: %q", got)
	}
	if got := handoff(" ", 124, steps); !strings.Contains(got, "cut short") || !strings.HasSuffix(got, "- `Bash` make test") {
		t.Errorf("a run cut short hands off its last steps: %q", got)
	}
}

func TestMaxTurns(t *testing.T) {
	for _, c := range []struct {
		role, complexity string
		want             int
	}{{"tester", "low", 30}, {"tester", "", 45}, {"tester", "medium", 45}, {"tester", "high", 60}, {"dev", "high", 30}, {"planner", "high", 30}} {
		if got := maxTurns(c.role, c.complexity); got != c.want {
			t.Errorf("maxTurns(%q, %q) = %d, want %d", c.role, c.complexity, got, c.want)
		}
	}
}
