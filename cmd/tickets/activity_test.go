package main

import (
	"encoding/json"
	"testing"
)

func TestActivityListJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	out := captureStdout(t, func() {
		if err := runActivity([]string{"list", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runActivity list: %v", err)
		}
	})
	var page struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("decode activity list --json output: %v (raw: %s)", err, out)
	}
	// newTestAPIServerWithAgent seeds a project, a feature, a ticket, and
	// an agent — the first three each emit an audit event.
	if len(page.Events) < 3 {
		t.Fatalf("activity list events = %+v, want at least 3 (project/feature/ticket created)", page.Events)
	}
}

func TestActivityListFiltersByEventType(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	out := captureStdout(t, func() {
		if err := runActivity([]string{"list", "--url", apiURL, "--event-type", "project_created", "--json"}); err != nil {
			t.Fatalf("runActivity list: %v", err)
		}
	})
	var page struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("decode activity list --json output: %v (raw: %s)", err, out)
	}
	if len(page.Events) != 1 || page.Events[0]["event_type"] != "project_created" {
		t.Errorf("activity list --event-type project_created = %+v, want exactly one project_created event", page.Events)
	}
}

func TestActivityListRequiresProject(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runActivity([]string{"list", "--url", apiURL}); err == nil {
		t.Error("activity list with no --project: want error, got nil")
	}
}

func TestActivityRequiresSubcommand(t *testing.T) {
	if err := runActivity(nil); err == nil {
		t.Error("runActivity with no subcommand: want error, got nil")
	}
}

func TestActivityRejectsUnknownSubcommand(t *testing.T) {
	if err := runActivity([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runActivity with an unknown subcommand: want error, got nil")
	}
}
