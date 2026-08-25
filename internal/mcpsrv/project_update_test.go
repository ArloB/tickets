package mcpsrv

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// TestInProcessBackendUpdateProjectStatusAndFieldsTogether is the
// discriminating case ADR 0021's "one merged project_update tool"
// design needs covered: a single call setting both status and
// title/description in one UpdateProjectInput. This exercises the
// version-rethreading InProcessBackend.UpdateProject does internally
// (status via SetProjectStatus first, then fields via UpdateProject
// against the version *that* call returned, not the caller's original
// ExpectedVersion) — the one path most likely to be subtly wrong and,
// until now, never executed by any test.
func TestInProcessBackendUpdateProjectStatusAndFieldsTogether(t *testing.T) {
	backend, _ := newTestBackend(t)
	_, agentActor := mustIssueAgentToken(t, backend, "codex")
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{Actor: agentActor, Permission: auth.PermissionEditor, AuthMethod: "bearer"})

	// Seed a non-empty description first — newTestBackend's project has
	// none, and a wipe vs. a correct merge-fetch are indistinguishable
	// against an already-empty string.
	seedDesc := "the original description, not touched by this call"
	if _, err := backend.Svc.UpdateProject(ctx, service.UpdateProjectRequest{
		Key: "ABC", Title: "Example", Description: seedDesc, ExpectedVersion: 1,
	}, agentActor, service.NewCorrelationID()); err != nil {
		t.Fatalf("seed description: %v", err)
	}

	status := "archived"
	title := "Renamed while archiving"
	proj, err := backend.UpdateProject(ctx, UpdateProjectInput{
		Key: "ABC", Status: &status, Title: &title, ExpectedVersion: 2,
	})
	if err != nil {
		t.Fatalf("UpdateProject(status+title): %v", err)
	}
	if proj.Status != domain.ProjectStatusArchived {
		t.Errorf("Status = %q, want archived", proj.Status)
	}
	if proj.Title != "Renamed while archiving" {
		t.Errorf("Title = %q, want %q", proj.Title, "Renamed while archiving")
	}
	// Two service calls happened under the hood (status bump, then a
	// fields update against the resulting version): version must have
	// advanced by 2 from the seeded version, not 1 — the tell for a
	// wrong ExpectedVersion being threaded into the second call.
	if proj.Version != 4 {
		t.Fatalf("Version = %d, want 4 (archive bumped 2->3, then the field update bumped 3->4)", proj.Version)
	}

	// The Description field was left nil in the status+title call —
	// confirm it merge-fetched the seeded description rather than
	// wiping it to "".
	live, err := backend.Svc.GetProject(ctx, "ABC")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if live.Description != seedDesc {
		t.Errorf("Description = %q, want unchanged %q (a nil Description must merge-fetch, not wipe)", live.Description, seedDesc)
	}

	// A stale ExpectedVersion still surfaces as version_conflict, not a
	// silently-accepted overwrite of one of the two merged operations.
	staleStatus := "active"
	if _, err := backend.UpdateProject(ctx, UpdateProjectInput{
		Key: "ABC", Status: &staleStatus, ExpectedVersion: 1,
	}); err == nil {
		t.Error("UpdateProject with a stale ExpectedVersion: want an error, got nil")
	} else if svcErr, ok := err.(*service.Error); !ok || svcErr.Code != domain.ErrVersionConflict {
		t.Errorf("UpdateProject with a stale ExpectedVersion = %v, want version_conflict", err)
	}
}

// TestInProcessBackendUpdateProjectFieldsOnlyMergeFetches confirms the
// fields-only path (no Status) merge-fetches the current record when
// Description is nil, mirroring updateTicketInProcess's own contract.
func TestInProcessBackendUpdateProjectFieldsOnlyMergeFetches(t *testing.T) {
	backend, _ := newTestBackend(t)
	_, agentActor := mustIssueAgentToken(t, backend, "codex")
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{Actor: agentActor, Permission: auth.PermissionEditor, AuthMethod: "bearer"})

	seedDesc := "seeded description, not sent in the title-only call"
	if _, err := backend.Svc.UpdateProject(ctx, service.UpdateProjectRequest{
		Key: "ABC", Title: "Example", Description: seedDesc, ExpectedVersion: 1,
	}, agentActor, service.NewCorrelationID()); err != nil {
		t.Fatalf("seed description: %v", err)
	}

	title := "Title only"
	proj, err := backend.UpdateProject(ctx, UpdateProjectInput{Key: "ABC", Title: &title, ExpectedVersion: 2})
	if err != nil {
		t.Fatalf("UpdateProject(title only): %v", err)
	}
	if proj.Title != "Title only" || proj.Version != 3 {
		t.Errorf("proj = %+v, want Title=%q Version=3", proj, "Title only")
	}
	if proj.Description != seedDesc {
		t.Errorf("Description = %q, want unchanged %q (a nil Description must merge-fetch, not wipe)", proj.Description, seedDesc)
	}
}
