package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/httpapi"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// newTestAPIServerWithAgent is newTestAPIServer plus a real agent
// bearer token — every ticket-write subcommand needs Editor permission
// (internal/httpapi's requireEditor), which anonymous reads alone
// don't grant.
func newTestAPIServerWithAgent(t *testing.T) (apiURL, token, ticketRef string) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)

	ctx := context.Background()
	human := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, human, "test-cid", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateFeature(ctx, service.CreateFeatureRequest{ProjectKey: "ABC", Title: "Second feature"}, human, "test-cid"); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "Fix the parser",
	}, human, "test-cid", "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	agent, err := svc.CreateAgent(ctx, service.CreateAgentRequest{Name: "codex"}, human, "test-cid")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	rawToken, _, err := svc.CreateAgentToken(ctx, agent.Ref, "", nil, human, "test-cid")
	if err != nil {
		t.Fatalf("create agent token: %v", err)
	}

	ts := httptest.NewServer(httpapi.NewHandler(svc, true))
	t.Cleanup(ts.Close)
	return ts.URL + "/api/v1", rawToken, ticket.Ref
}

func TestTicketUpdateJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"update", ref, "--url", apiURL, "--status", "in_progress", "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("runTicket update: %v", err)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode ticket update --json output: %v (raw: %s)", err, out)
	}
	if decoded["status"] != "in_progress" {
		t.Errorf("ticket update output status = %v, want in_progress", decoded["status"])
	}
}

func TestTicketUpdateRequiresIfVersion(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"update", ref, "--url", apiURL, "--status", "in_progress"}); err == nil {
		t.Error("ticket update with no --if-version: want error, got nil")
	}
}

// TestTicketUpdateStaleVersionExitsWithVersionConflictCode proves a
// stale --if-version surfaces as version_conflict all the way through
// to the CLI's exit code — not a silent overwrite, not a generic
// failure.
func TestTicketUpdateStaleVersionExitsWithVersionConflictCode(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	err := runTicket([]string{"update", ref, "--url", apiURL, "--status", "in_progress", "--if-version", "999"})
	if err == nil {
		t.Fatal("ticket update with a stale --if-version: want error, got nil")
	}
	var cerr *apiclient.Error
	if !errors.As(err, &cerr) || cerr.Code != domain.ErrVersionConflict {
		t.Fatalf("ticket update error = %#v, want a *apiclient.Error with code %q", err, domain.ErrVersionConflict)
	}
	if got := exitCode(cerr.Code); got != 13 {
		t.Errorf("exitCode(version_conflict) = %d, want 13", got)
	}
}

func TestTicketAssignAndUnassign(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"assign", ref, "--url", apiURL, "--assignee", "agent:codex", "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("runTicket assign: %v", err)
		}
	})
	if !strings.Contains(out, `"assignee": "agent:codex"`) {
		t.Errorf("ticket assign output = %q, want it to contain assignee=agent:codex", out)
	}

	captureStdout(t, func() {
		if err := runTicket([]string{"assign", ref, "--url", apiURL, "--unassign", "--if-version", "2"}); err != nil {
			t.Fatalf("runTicket unassign: %v", err)
		}
	})
}

func TestTicketAssignRequiresExactlyOneOfAssigneeOrUnassign(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"assign", ref, "--url", apiURL, "--if-version", "1"}); err == nil {
		t.Error("ticket assign with neither --assignee nor --unassign: want error, got nil")
	}
	if err := runTicket([]string{"assign", ref, "--url", apiURL, "--assignee", "agent:codex", "--unassign", "--if-version", "1"}); err == nil {
		t.Error("ticket assign with both --assignee and --unassign: want error, got nil")
	}
}

func TestTicketMove(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"move", ref, "--url", apiURL, "--feature", "ABC-F2", "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("runTicket move: %v", err)
		}
	})
	if !strings.Contains(out, `"feature": "ABC-F2"`) {
		t.Errorf("ticket move output = %q, want it to contain feature=ABC-F2", out)
	}
}

func TestTicketMoveRequiresFeature(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"move", ref, "--url", apiURL, "--if-version", "1"}); err == nil {
		t.Error("ticket move with no --feature: want error, got nil")
	}
}

func TestTicketDeleteAndRestore(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"delete", ref, "--url", apiURL, "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("runTicket delete: %v", err)
		}
	})
	if !strings.Contains(out, `"version": 2`) {
		t.Errorf("ticket delete output = %q, want it to report the new version", out)
	}

	captureStdout(t, func() {
		if err := runTicket([]string{"restore", ref, "--url", apiURL, "--if-version", "2"}); err != nil {
			t.Fatalf("runTicket restore: %v", err)
		}
	})
}

func TestTicketWriteSubcommandsRequireLeadingRef(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"update", "--url", apiURL, "--status", "in_progress"}); err == nil {
		t.Error("ticket update with no leading ref (a flag came first): want error, got nil")
	}
}
