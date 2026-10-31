package main

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
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
