package main

import (
	"encoding/json"
	"testing"
)

func TestDecisionCreateAndUpdateConsequences(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	createOut := captureStdout(t, func() {
		if err := runDecision([]string{
			"create", "--url", apiURL, "--title", "Use SQLite", "--consequences", "Simpler ops", "--json",
		}); err != nil {
			t.Fatalf("runDecision create: %v", err)
		}
	})
	var created map[string]any
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("decode decision create --json output: %v (raw: %s)", err, createOut)
	}
	if created["consequences"] != "Simpler ops" {
		t.Errorf("decision create consequences = %v, want %q", created["consequences"], "Simpler ops")
	}
	ref, _ := created["ref"].(string)

	updateOut := captureStdout(t, func() {
		if err := runDecision([]string{
			"update", ref, "--url", apiURL, "--title", "Use SQLite", "--status", "accepted",
			"--consequences", "Even simpler ops", "--if-version", "1", "--json",
		}); err != nil {
			t.Fatalf("runDecision update: %v", err)
		}
	})
	var updated map[string]any
	if err := json.Unmarshal([]byte(updateOut), &updated); err != nil {
		t.Fatalf("decode decision update --json output: %v (raw: %s)", err, updateOut)
	}
	if updated["consequences"] != "Even simpler ops" {
		t.Errorf("decision update consequences = %v, want %q", updated["consequences"], "Even simpler ops")
	}
}

// TestDecisionUpdatePreservesConsequencesWhenOmitted proves omitting
// --consequences on update falls back to the current server-side value
// (the full-representation contract every other text field already
// has), rather than silently wiping it.
func TestDecisionUpdatePreservesConsequencesWhenOmitted(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	createOut := captureStdout(t, func() {
		if err := runDecision([]string{
			"create", "--url", apiURL, "--title", "Use SQLite", "--consequences", "Simpler ops", "--json",
		}); err != nil {
			t.Fatalf("runDecision create: %v", err)
		}
	})
	var created map[string]any
	_ = json.Unmarshal([]byte(createOut), &created)
	ref, _ := created["ref"].(string)

	updateOut := captureStdout(t, func() {
		if err := runDecision([]string{
			"update", ref, "--url", apiURL, "--title", "Use SQLite (renamed)", "--status", "accepted", "--if-version", "1", "--json",
		}); err != nil {
			t.Fatalf("runDecision update: %v", err)
		}
	})
	var updated map[string]any
	_ = json.Unmarshal([]byte(updateOut), &updated)
	if updated["consequences"] != "Simpler ops" {
		t.Errorf("decision update with no --consequences = %v, want the preserved current value %q", updated["consequences"], "Simpler ops")
	}
}

func TestDecisionVersionsAndDiffCLI(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	createOut := captureStdout(t, func() {
		if err := runDecision([]string{"create", "--url", apiURL, "--title", "v1", "--json"}); err != nil {
			t.Fatalf("runDecision create: %v", err)
		}
	})
	var created map[string]any
	_ = json.Unmarshal([]byte(createOut), &created)
	ref, _ := created["ref"].(string)

	if err := runDecision([]string{"update", ref, "--url", apiURL, "--title", "v2", "--status", "accepted", "--if-version", "1"}); err != nil {
		t.Fatalf("runDecision update: %v", err)
	}

	versionsOut := captureStdout(t, func() {
		if err := runDecision([]string{"versions", ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runDecision versions: %v", err)
		}
	})
	var versionsPage struct {
		Versions []map[string]any `json:"versions"`
	}
	if err := json.Unmarshal([]byte(versionsOut), &versionsPage); err != nil {
		t.Fatalf("decode decision versions --json output: %v (raw: %s)", err, versionsOut)
	}
	if len(versionsPage.Versions) != 1 || versionsPage.Versions[0]["title"] != "v1" {
		t.Errorf("decision versions = %+v, want exactly one archived version titled v1", versionsPage.Versions)
	}

	diffOut := captureStdout(t, func() {
		if err := runDecision([]string{"diff", ref, "--url", apiURL, "--from", "1", "--to", "2", "--json"}); err != nil {
			t.Fatalf("runDecision diff: %v", err)
		}
	})
	var diff map[string]any
	if err := json.Unmarshal([]byte(diffOut), &diff); err != nil {
		t.Fatalf("decode decision diff --json output: %v (raw: %s)", err, diffOut)
	}
	if diff["status_from"] != "proposed" || diff["status_to"] != "accepted" {
		t.Errorf("decision diff status = %v -> %v, want proposed -> accepted", diff["status_from"], diff["status_to"])
	}
}

func TestDecisionDiffRequiresFromAndTo(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runDecision([]string{"diff", "ABC-D1", "--url", apiURL}); err == nil {
		t.Error("decision diff with no --from/--to: want error, got nil")
	}
	if err := runDecision([]string{"diff", "ABC-D1", "--url", apiURL, "--from", "1"}); err == nil {
		t.Error("decision diff with no --to: want error, got nil")
	}
}

func TestDecisionVersionsRequiresRef(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runDecision([]string{"versions", "--url", apiURL}); err == nil {
		t.Error("decision versions with no ref argument: want error, got nil")
	}
}
