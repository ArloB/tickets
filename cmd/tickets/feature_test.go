package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFeatureCreateGetUpdateLifecycle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	createOut := captureStdout(t, func() {
		if err := runFeature([]string{"create", "--url", apiURL, "--title", "Payments", "--priority", "high", "--description", "Handles billing", "--json"}); err != nil {
			t.Fatalf("runFeature create: %v", err)
		}
	})
	var created map[string]any
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("decode feature create --json output: %v (raw: %s)", err, createOut)
	}
	ref, _ := created["ref"].(string)
	if ref == "" || created["priority"] != "high" {
		t.Fatalf("feature create output = %v, want a ref and priority=high", created)
	}

	getOut := captureStdout(t, func() {
		if err := runFeature([]string{"get", ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runFeature get: %v", err)
		}
	})
	if !strings.Contains(getOut, "Payments") {
		t.Errorf("feature get output = %q, want it to contain the title", getOut)
	}

	updateOut := captureStdout(t, func() {
		if err := runFeature([]string{"update", ref, "--url", apiURL, "--title", "Payments (v2)", "--priority", "medium", "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("runFeature update: %v", err)
		}
	})
	var updated map[string]any
	if err := json.Unmarshal([]byte(updateOut), &updated); err != nil {
		t.Fatalf("decode feature update --json output: %v (raw: %s)", err, updateOut)
	}
	if updated["priority"] != "medium" || updated["title"] != "Payments (v2)" {
		t.Errorf("feature update output = %v, want title=%q priority=medium", updated, "Payments (v2)")
	}
	if updated["description"] != "Handles billing" {
		t.Errorf("feature update omitted --description, want it preserved from the current value; got %v", updated)
	}
}

func TestFeatureListRequiresProject(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runFeature([]string{"list", "--url", apiURL}); err == nil {
		t.Error("feature list with no --project: want error, got nil")
	}
}

func TestFeatureCreateRequiresTitle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	if err := runFeature([]string{"create", "--url", apiURL}); err == nil {
		t.Error("feature create with no --title: want error, got nil")
	}
}

func TestFeatureUpdateRequiresFullRepresentation(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runFeature([]string{"update", "ABC-F1", "--url", apiURL, "--if-version", "1"}); err == nil {
		t.Error("feature update with no --title/--priority: want error, got nil")
	}
	if err := runFeature([]string{"update", "ABC-F1", "--url", apiURL, "--title", "x", "--priority", "medium"}); err == nil {
		t.Error("feature update with no --if-version: want error, got nil")
	}
}

func TestFeatureListJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	out := captureStdout(t, func() {
		if err := runFeature([]string{"list", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runFeature list: %v", err)
		}
	})
	// newTestAPIServerWithAgent already seeds a second feature.
	if !strings.Contains(out, "ABC-F1") {
		t.Errorf("feature list --json output = %q, want it to contain the General feature", out)
	}
}

func TestFeatureRequiresSubcommand(t *testing.T) {
	if err := runFeature(nil); err == nil {
		t.Error("runFeature with no subcommand: want error, got nil")
	}
}

func TestFeatureRejectsUnknownSubcommand(t *testing.T) {
	if err := runFeature([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runFeature with an unknown subcommand: want error, got nil")
	}
}
