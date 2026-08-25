package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProjectUpdateAndArchiveLifecycle is the CLI-layer counterpart to
// internal/service/project_test.go and
// internal/httpapi/project_lifecycle_test.go — `project update` and
// `project archive`/`project unarchive` (ADR 0021) round-tripping
// through cfg.newClient() the way TestFeatureCreateGetUpdateLifecycle
// does for features.
func TestProjectUpdateAndArchiveLifecycle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	// newTestAPIServerWithAgent already creates project ABC at version 1.
	updateOut := captureStdout(t, func() {
		if err := runProject([]string{"update", "ABC", "--url", apiURL, "--title", "Example v2", "--description", "renamed", "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("runProject update: %v", err)
		}
	})
	var updated map[string]any
	if err := json.Unmarshal([]byte(updateOut), &updated); err != nil {
		t.Fatalf("decode project update --json output: %v (raw: %s)", err, updateOut)
	}
	if updated["title"] != "Example v2" || updated["description"] != "renamed" {
		t.Errorf("project update output = %v, want title=%q description=%q", updated, "Example v2", "renamed")
	}
	version := int64(updated["version"].(float64))
	if version != 2 {
		t.Fatalf("version after update = %d, want 2", version)
	}

	archiveOut := captureStdout(t, func() {
		if err := runProject([]string{"archive", "ABC", "--url", apiURL, "--if-version", "2", "--json"}); err != nil {
			t.Fatalf("runProject archive: %v", err)
		}
	})
	var archived map[string]any
	if err := json.Unmarshal([]byte(archiveOut), &archived); err != nil {
		t.Fatalf("decode project archive --json output: %v (raw: %s)", err, archiveOut)
	}
	if archived["status"] != "archived" {
		t.Errorf("project archive output = %v, want status=archived", archived)
	}

	// Default `project list` excludes it; --include-archived shows it.
	defaultListOut := captureStdout(t, func() {
		if err := runProject([]string{"list", "--url", apiURL}); err != nil {
			t.Fatalf("runProject list: %v", err)
		}
	})
	if strings.Contains(defaultListOut, "ABC") {
		t.Errorf("default project list = %q, want ABC excluded once archived", defaultListOut)
	}
	includeArchivedOut := captureStdout(t, func() {
		if err := runProject([]string{"list", "--url", apiURL, "--include-archived"}); err != nil {
			t.Fatalf("runProject list --include-archived: %v", err)
		}
	})
	if !strings.Contains(includeArchivedOut, "ABC") {
		t.Errorf("project list --include-archived = %q, want ABC present", includeArchivedOut)
	}

	unarchiveOut := captureStdout(t, func() {
		if err := runProject([]string{"unarchive", "ABC", "--url", apiURL, "--if-version", "3", "--json"}); err != nil {
			t.Fatalf("runProject unarchive: %v", err)
		}
	})
	var unarchived map[string]any
	if err := json.Unmarshal([]byte(unarchiveOut), &unarchived); err != nil {
		t.Fatalf("decode project unarchive --json output: %v (raw: %s)", err, unarchiveOut)
	}
	if unarchived["status"] != "active" {
		t.Errorf("project unarchive output = %v, want status=active", unarchived)
	}

	backOut := captureStdout(t, func() {
		if err := runProject([]string{"list", "--url", apiURL}); err != nil {
			t.Fatalf("runProject list: %v", err)
		}
	})
	if !strings.Contains(backOut, "ABC") {
		t.Errorf("default project list after unarchive = %q, want ABC present again", backOut)
	}
}

func TestProjectUpdateRequiresIfVersionAndTitle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runProject([]string{"update", "ABC", "--url", apiURL, "--title", "x"}); err == nil {
		t.Error("project update with no --if-version: want error, got nil")
	}
	if err := runProject([]string{"update", "ABC", "--url", apiURL, "--if-version", "1"}); err == nil {
		t.Error("project update with no --title: want error, got nil")
	}
}
