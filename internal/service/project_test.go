package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestUpdateProject confirms the happy path: title/description
// change, version bumps by exactly one, and the project is
// re-fetchable with the new values.
func TestUpdateProject(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	updated, err := s.UpdateProject(ctx, UpdateProjectRequest{
		Key: "ABC", Title: "Example v2", Description: "new description", ExpectedVersion: 1,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Title != "Example v2" || updated.Description != "new description" {
		t.Errorf("updated project = %+v, want Title=%q Description=%q", updated, "Example v2", "new description")
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2", updated.Version)
	}

	reloaded, err := s.GetProject(ctx, "ABC")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if reloaded.Title != "Example v2" || reloaded.Description != "new description" {
		t.Errorf("reloaded project = %+v, want the same fields UpdateProject returned", reloaded)
	}
}

// TestUpdateProjectVersionConflict mirrors
// TestUpdateFeatureVersionConflict: a stale ExpectedVersion is
// rejected as version_conflict carrying the current version, and
// retrying with the correct version succeeds.
func TestUpdateProjectVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.UpdateProject(ctx, UpdateProjectRequest{
		Key: "ABC", Title: "Example v2", ExpectedVersion: 2,
	}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("UpdateProject with stale version = %v, want version_conflict", err)
	}
	if svcErr.CurrentVersion == nil || *svcErr.CurrentVersion != 1 {
		t.Errorf("CurrentVersion = %v, want 1", svcErr.CurrentVersion)
	}

	updated, err := s.UpdateProject(ctx, UpdateProjectRequest{
		Key: "ABC", Title: "Example v2", ExpectedVersion: 1,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateProject with correct version: %v", err)
	}
	if updated.Title != "Example v2" {
		t.Errorf("Title = %q, want %q", updated.Title, "Example v2")
	}
}

// TestSetProjectStatusArchiveIsVisibilityOnly is ADR 0021's contract
// as a test: archiving excludes a project from the default
// ListProjects page but not from includeArchived=true, GetProject
// stays reachable throughout (so unarchive is always possible), and
// unarchiving restores default visibility.
func TestSetProjectStatusArchiveIsVisibilityOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	archived, err := s.SetProjectStatus(ctx, SetProjectStatusRequest{
		Key: "ABC", NewStatus: domain.ProjectStatusArchived, ExpectedVersion: 1,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("SetProjectStatus archive: %v", err)
	}
	if archived.Status != domain.ProjectStatusArchived {
		t.Errorf("Status = %q, want archived", archived.Status)
	}
	if archived.Version != 2 {
		t.Errorf("Version = %d, want 2", archived.Version)
	}

	defaultPage, err := s.ListProjects(ctx, 20, "", false)
	if err != nil {
		t.Fatalf("ListProjects (default): %v", err)
	}
	for _, p := range defaultPage.Projects {
		if p.Key == "ABC" {
			t.Errorf("ListProjects default page = %+v, want ABC excluded once archived", defaultPage.Projects)
		}
	}

	allPage, err := s.ListProjects(ctx, 20, "", true)
	if err != nil {
		t.Fatalf("ListProjects (includeArchived): %v", err)
	}
	var sawArchived bool
	for _, p := range allPage.Projects {
		if p.Key == "ABC" {
			sawArchived = true
		}
	}
	if !sawArchived {
		t.Errorf("ListProjects includeArchived=true = %+v, want ABC still present", allPage.Projects)
	}

	// GetProject stays status-blind — an archived project must remain
	// fetchable, or unarchiving would be unreachable through the API.
	fetched, err := s.GetProject(ctx, "ABC")
	if err != nil {
		t.Fatalf("GetProject on archived project: %v", err)
	}
	if fetched.Status != domain.ProjectStatusArchived {
		t.Errorf("GetProject Status = %q, want archived", fetched.Status)
	}

	unarchived, err := s.SetProjectStatus(ctx, SetProjectStatusRequest{
		Key: "ABC", NewStatus: domain.ProjectStatusActive, ExpectedVersion: archived.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("SetProjectStatus unarchive: %v", err)
	}
	if unarchived.Status != domain.ProjectStatusActive {
		t.Errorf("Status = %q, want active", unarchived.Status)
	}

	defaultPage2, err := s.ListProjects(ctx, 20, "", false)
	if err != nil {
		t.Fatalf("ListProjects (default) after unarchive: %v", err)
	}
	var sawActive bool
	for _, p := range defaultPage2.Projects {
		if p.Key == "ABC" {
			sawActive = true
		}
	}
	if !sawActive {
		t.Errorf("ListProjects default page after unarchive = %+v, want ABC present again", defaultPage2.Projects)
	}
}

// TestUpdateProjectReindexesSearch confirms indexProjectSearchDoc's
// UpsertSearchDocument call actually replaces the row on edit, rather
// than leaving a stale one alongside it: search by the old title finds
// nothing after the edit, and the new title is findable.
func TestUpdateProjectReindexesSearch(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	before, err := s.Search(ctx, SearchRequest{Query: "Example"})
	if err != nil {
		t.Fatalf("Search before update: %v", err)
	}
	if !hasProjectHit(before.Hits, "ABC") {
		t.Fatalf("Search before update = %+v, want a project hit for ABC", before.Hits)
	}

	if _, err := s.UpdateProject(ctx, UpdateProjectRequest{
		Key: "ABC", Title: "Glorbnaxian", ExpectedVersion: 1,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	stale, err := s.Search(ctx, SearchRequest{Query: "Example"})
	if err != nil {
		t.Fatalf("Search for old title: %v", err)
	}
	if hasProjectHit(stale.Hits, "ABC") {
		t.Errorf("Search for old title = %+v, want no project hit after the title changed (stale index row)", stale.Hits)
	}

	fresh, err := s.Search(ctx, SearchRequest{Query: "Glorbnaxian"})
	if err != nil {
		t.Fatalf("Search for new title: %v", err)
	}
	if !hasProjectHit(fresh.Hits, "ABC") {
		t.Errorf("Search for new title = %+v, want a project hit for ABC", fresh.Hits)
	}
}

// TestArchivedProjectTicketsStayFullyWritable is ADR 0021's central
// claim as a test: archiving a project is visibility only and does
// not cascade — a ticket underneath an archived project can still be
// created and updated exactly as before.
func TestArchivedProjectTicketsStayFullyWritable(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	if _, err := s.SetProjectStatus(ctx, SetProjectStatusRequest{
		Key: "ABC", NewStatus: domain.ProjectStatusArchived, ExpectedVersion: 1,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("SetProjectStatus archive: %v", err)
	}

	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Work continues under an archived project",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket under an archived project: %v", err)
	}

	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	updated, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
		Ref: ref, NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: ticket.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateTicketStatus under an archived project: %v", err)
	}
	if updated.Status != domain.WorkflowStatusInProgress {
		t.Errorf("Status = %q, want in_progress", updated.Status)
	}
}

func hasProjectHit(hits []SearchHit, ref string) bool {
	for _, h := range hits {
		if h.Kind == "project" && h.Ref == ref {
			return true
		}
	}
	return false
}
