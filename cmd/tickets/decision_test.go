package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecisionCreateGetUpdateLifecycle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	createOut := captureStdout(t, func() {
		if err := runDecision([]string{
			"create", "--url", apiURL, "--title", "Use SQLite",
			"--context", "We need a store", "--decision", "Use SQLite", "--rationale", "Simplicity", "--json",
		}); err != nil {
			t.Fatalf("runDecision create: %v", err)
		}
	})
	var created map[string]any
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("decode decision create --json output: %v (raw: %s)", err, createOut)
	}
	ref, _ := created["ref"].(string)
	if ref == "" || created["status"] != "proposed" {
		t.Fatalf("decision create output = %v, want a ref and status=proposed", created)
	}

	getOut := captureStdout(t, func() {
		if err := runDecision([]string{"get", ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runDecision get: %v", err)
		}
	})
	if !strings.Contains(getOut, "Use SQLite") {
		t.Errorf("decision get output = %q, want it to contain the title", getOut)
	}

	updateOut := captureStdout(t, func() {
		if err := runDecision([]string{
			"update", ref, "--url", apiURL, "--title", "Use SQLite (final)", "--status", "accepted", "--if-version", "1", "--json",
		}); err != nil {
			t.Fatalf("runDecision update: %v", err)
		}
	})
	var updated map[string]any
	if err := json.Unmarshal([]byte(updateOut), &updated); err != nil {
		t.Fatalf("decode decision update --json output: %v (raw: %s)", err, updateOut)
	}
	if updated["status"] != "accepted" || updated["title"] != "Use SQLite (final)" {
		t.Errorf("decision update output = %v, want title=%q status=accepted", updated, "Use SQLite (final)")
	}
	if updated["context"] != "We need a store" || updated["decision"] != "Use SQLite" || updated["rationale"] != "Simplicity" {
		t.Errorf("decision update omitted --context/--decision/--rationale, want them preserved from the current value; got %v", updated)
	}
}

// TestDecisionCreateIdempotencyKeyIsWired proves --idempotency-key
// actually reaches apiclient.CreateDecision, not just that the flag
// parses: replaying the same key with identical arguments must return
// the original decision's ref, not create a second decision.
func TestDecisionCreateIdempotencyKeyIsWired(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	runCreate := func() map[string]any {
		out := captureStdout(t, func() {
			if err := runDecision([]string{
				"create", "--url", apiURL, "--title", "Use SQLite",
				"--context", "We need a store", "--decision", "Use SQLite", "--rationale", "Simplicity",
				"--idempotency-key", "dup-key-1", "--json",
			}); err != nil {
				t.Fatalf("runDecision create: %v", err)
			}
		})
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("decode decision create --json output: %v (raw: %s)", err, out)
		}
		return m
	}

	first := runCreate()
	replay := runCreate()
	if first["ref"] == "" || first["ref"] != replay["ref"] {
		t.Errorf("decision create replayed with the same --idempotency-key: refs = %v, %v — want the same ref (the flag isn't reaching the server)", first["ref"], replay["ref"])
	}
}

func TestDecisionListRequiresProject(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runDecision([]string{"list", "--url", apiURL}); err == nil {
		t.Error("decision list with no --project: want error, got nil")
	}
}

func TestDecisionCreateRequiresTitle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	if err := runDecision([]string{"create", "--url", apiURL}); err == nil {
		t.Error("decision create with no --title: want error, got nil")
	}
}

func TestDecisionCreateRejectsBothInlineAndFile(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	err := runDecision([]string{
		"create", "--url", apiURL, "--title", "T",
		"--context", "inline", "--context-file", "-",
	})
	if err == nil {
		t.Error("decision create with both --context and --context-file: want error, got nil")
	}
}

func TestDecisionUpdateRequiresFullRepresentation(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runDecision([]string{"update", "ABC-D1", "--url", apiURL, "--if-version", "1"}); err == nil {
		t.Error("decision update with no --title/--status: want error, got nil")
	}
	if err := runDecision([]string{"update", "ABC-D1", "--url", apiURL, "--title", "x", "--status", "accepted"}); err == nil {
		t.Error("decision update with no --if-version: want error, got nil")
	}
}

func TestDecisionRequiresSubcommand(t *testing.T) {
	if err := runDecision(nil); err == nil {
		t.Error("runDecision with no subcommand: want error, got nil")
	}
}

func TestDecisionRejectsUnknownSubcommand(t *testing.T) {
	if err := runDecision([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runDecision with an unknown subcommand: want error, got nil")
	}
}
