package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseClaudeTranscriptExtractsTicketsToolsOnly(t *testing.T) {
	f, err := os.Open("testdata/claude-success.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	tr, err := parseClaudeTranscript(f)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"project_brief", "tickets_list", "ticket_update"}
	if got := tr.sequence(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence = %v, want %v (host-side ToolSearch must not be counted)", got, want)
	}
	if got := tr.firstCall(); got != "project_brief" {
		t.Fatalf("firstCall = %q, want project_brief", got)
	}
	if got := tr.errorCount(); got != 1 {
		t.Fatalf("errorCount = %d, want 1", got)
	}
	if got := tr.Calls[1].Error; !strings.Contains(got, "invalid argument") {
		t.Fatalf("Calls[1].Error = %q, want it to carry the tool_result text", got)
	}
	if tr.HostError != "" {
		t.Fatalf("HostError = %q, want empty for a successful result event", tr.HostError)
	}
}

func TestParseCodexTranscriptExtractsToolCallsAndErrors(t *testing.T) {
	f, err := os.Open("testdata/codex-success.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	tr, err := parseCodexTranscript(f)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"project_brief", "project_brief", "ticket_update"}
	if got := tr.sequence(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence = %v, want %v (item.started must not double-count)", got, want)
	}
	if got := tr.errorCount(); got != 1 {
		t.Fatalf("errorCount = %d, want 1", got)
	}
	if got := tr.Calls[0].Error; !strings.Contains(got, "tool call failed") {
		t.Fatalf("Calls[0].Error = %q, want the error message field", got)
	}
}

func TestParseClaudeTranscriptReportsHostFailure(t *testing.T) {
	in := strings.NewReader(`{"type":"result","subtype":"error_max_turns","is_error":true}`)
	tr, err := parseClaudeTranscript(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tr.HostError, "error_max_turns") {
		t.Fatalf("HostError = %q, want it to name the failure subtype", tr.HostError)
	}
}

func TestParsersTolerateMalformedLines(t *testing.T) {
	in := `not json
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"mcp__tickets__search"}]}}

`
	tr, err := parseClaudeTranscript(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := tr.sequence(); !reflect.DeepEqual(got, []string{"search"}) {
		t.Fatalf("sequence = %v, want [search]", got)
	}

	ctr, err := parseCodexTranscript(strings.NewReader("garbage\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ctr.Calls) != 0 {
		t.Fatalf("Calls = %v, want none", ctr.Calls)
	}
}

func TestSchemaErrorsAreDistinguishedFromDomainErrors(t *testing.T) {
	cases := []struct {
		err    string
		schema bool
	}{
		{"Mcp error: -32602: invalid arguments for tool project_brief", true},
		{"invalid params: validating \"arguments\"", true},
		{"unknown tool: project_brie", true},
		{"not_found: content item not found", false},
		{"version_conflict: expected_version 2, current 3", false},
		{"validation_failed: title must not be empty", false},
		{"", false},
		{"some unrecognized phrasing a future CLI version invents", true},
	}
	for _, c := range cases {
		if got := (toolCall{Tool: "x", Error: c.err}).isSchemaError(); got != c.schema {
			t.Errorf("isSchemaError(%q) = %v, want %v", c.err, got, c.schema)
		}
	}
}
