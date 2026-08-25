package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/httpapi"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// newTestAPIServer starts a real internal/httpapi server (anonymous
// read enabled, so these list-only tests need no bearer token) seeded
// with one project and one ticket, and returns its /api/v1 base URL.
func newTestAPIServer(t *testing.T) string {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)

	ctx := context.Background()
	actor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, actor, "test-cid", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "Fix the parser",
	}, actor, "test-cid", "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	ts := httptest.NewServer(httpapi.NewHandler(svc, true))
	t.Cleanup(ts.Close)
	return ts.URL + "/api/v1"
}

// isolateClientEnv prevents a client command under test from picking
// up this developer machine's real client config file or TICKETS_*
// environment — every test that calls runProject/runTicket must call
// this first, so behavior depends only on the flags each test passes.
func isolateClientEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TICKETS_CLIENT_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	t.Setenv("TICKETS_API_URL", "")
	t.Setenv("TICKETS_API_TOKEN", "")
	t.Setenv("TICKETS_PROJECT", "")
	t.Setenv("NO_COLOR", "")
}

func TestProjectListJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)

	out := captureStdout(t, func() {
		if err := runProject([]string{"list", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runProject list: %v", err)
		}
	})
	if !strings.Contains(out, `"key": "ABC"`) {
		t.Errorf("project list --json output = %q, want it to contain project ABC", out)
	}
}

func TestProjectCreateJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runProject([]string{
			"create", "--url", apiURL, "--key", "XYZ", "--title", "Second Project",
			"--description", "A second scratch project", "--json",
		}); err != nil {
			t.Fatalf("runProject create: %v", err)
		}
	})
	var created map[string]any
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("decode project create --json output: %v (raw: %s)", err, out)
	}
	if created["key"] != "XYZ" || created["title"] != "Second Project" {
		t.Errorf("project create output = %v, want key=XYZ title=%q", created, "Second Project")
	}

	listOut := captureStdout(t, func() {
		if err := runProject([]string{"list", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runProject list: %v", err)
		}
	})
	if !strings.Contains(listOut, `"key": "XYZ"`) {
		t.Errorf("project list --json output = %q, want it to contain the newly created project XYZ", listOut)
	}
}

func TestProjectCreateRequiresKeyAndTitle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runProject([]string{"create", "--url", apiURL, "--title", "x"}); err == nil {
		t.Error("project create with no --key: want error, got nil")
	}
	if err := runProject([]string{"create", "--url", apiURL, "--key", "XYZ"}); err == nil {
		t.Error("project create with no --title: want error, got nil")
	}
}

func TestProjectListTable(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)

	out := captureStdout(t, func() {
		if err := runProject([]string{"list", "--url", apiURL}); err != nil {
			t.Fatalf("runProject list: %v", err)
		}
	})
	if !strings.Contains(out, "KEY") || !strings.Contains(out, "ABC") {
		t.Errorf("project list table output = %q, want a header row and project ABC", out)
	}
}

func TestProjectBriefTable(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)

	out := captureStdout(t, func() {
		if err := runProject([]string{"brief", "ABC", "--url", apiURL}); err != nil {
			t.Fatalf("runProject brief: %v", err)
		}
	})
	if !strings.Contains(out, "ABC") || !strings.Contains(out, "ISSUE REGISTER") || !strings.Contains(out, "FEATURES") {
		t.Errorf("project brief table output = %q, want the project key and section headers", out)
	}
}

func TestProjectBriefJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)

	out := captureStdout(t, func() {
		if err := runProject([]string{"brief", "ABC", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runProject brief: %v", err)
		}
	})
	var brief struct {
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
		IssueRegister []map[string]any `json:"issue_register"`
	}
	if err := json.Unmarshal([]byte(out), &brief); err != nil {
		t.Fatalf("unmarshal brief: %v (raw: %s)", err, out)
	}
	if brief.Project.Key != "ABC" {
		t.Errorf("brief.Project.Key = %q, want %q", brief.Project.Key, "ABC")
	}
	if len(brief.IssueRegister) != 1 {
		t.Errorf("len(IssueRegister) = %d, want 1 (the seeded bug)", len(brief.IssueRegister))
	}
}

func TestProjectBriefRequiresKeyArgument(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)

	if err := runProject([]string{"brief", "--url", apiURL}); err == nil {
		t.Error("project brief with no key argument: want error, got nil")
	}
}

// TestTicketListRequiresProject proves ticket list never silently
// hits GET /projects//tickets when no project is configured — it must
// fail client-side with a clear error before any request is sent.
func TestTicketListRequiresProject(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)

	if err := runTicket([]string{"list", "--url", apiURL}); err == nil {
		t.Error("ticket list with no --project and no TICKETS_PROJECT: want error, got nil")
	}
}

func TestTicketListJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"list", "--url", apiURL, "--project", "ABC", "--json"}); err != nil {
			t.Fatalf("runTicket list: %v", err)
		}
	})
	if !strings.Contains(out, `"ref": "ABC-1"`) {
		t.Errorf("ticket list --json output = %q, want it to contain ticket ABC-1", out)
	}
}

// TestTicketListProjectFromEnv proves TICKETS_PROJECT (the same
// client-side default convention ADR 0016 established for `tickets
// mcp --project`) also satisfies ticket list's --project requirement.
func TestTicketListProjectFromEnv(t *testing.T) {
	isolateClientEnv(t)
	apiURL := newTestAPIServer(t)
	t.Setenv("TICKETS_PROJECT", "ABC")

	out := captureStdout(t, func() {
		if err := runTicket([]string{"list", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runTicket list with TICKETS_PROJECT: %v", err)
		}
	})
	if !strings.Contains(out, `"ref": "ABC-1"`) {
		t.Errorf("ticket list --json output = %q, want it to contain ticket ABC-1", out)
	}
}

func TestProjectRequiresSubcommand(t *testing.T) {
	if err := runProject(nil); err == nil {
		t.Error("runProject with no subcommand: want error, got nil")
	}
}

func TestProjectRejectsUnknownSubcommand(t *testing.T) {
	if err := runProject([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runProject with an unknown subcommand: want error, got nil")
	}
}

func TestTicketRequiresSubcommand(t *testing.T) {
	if err := runTicket(nil); err == nil {
		t.Error("runTicket with no subcommand: want error, got nil")
	}
}

func TestTicketRejectsUnknownSubcommand(t *testing.T) {
	if err := runTicket([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runTicket with an unknown subcommand: want error, got nil")
	}
}
