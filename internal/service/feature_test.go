package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func mustCreateProject(t *testing.T, s *Service, key string) {
	t.Helper()
	if _, err := s.CreateProject(context.Background(), CreateProjectRequest{Key: key, Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project %q: %v", key, err)
	}
}

// TestCreateFeatureAllocatesReference confirms a feature gets its own
// (project, kind)-scoped reference distinct from the auto-created
// General feature (ABC-F1), and that its fields round-trip.
func TestCreateFeatureAllocatesReference(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	f, err := s.CreateFeature(ctx, CreateFeatureRequest{
		ProjectKey: "ABC", Title: "Payments", Description: "Billing work", Priority: domain.PriorityHigh,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if f.Ref != "ABC-F2" {
		t.Errorf("Ref = %q, want ABC-F2 (General is ABC-F1)", f.Ref)
	}
	if f.Title != "Payments" || f.Description != "Billing work" || f.Priority != domain.PriorityHigh {
		t.Errorf("feature = %+v, want Title=Payments Description=%q Priority=high", f, "Billing work")
	}
	if f.Status != domain.WorkflowStatusBacklog {
		t.Errorf("Status = %q, want backlog", f.Status)
	}
}

// TestListFeaturesOrdersByPriorityThenPosition confirms ListFeatures
// doesn't just return insertion order — a TEXT-sorted priority would
// place "low" before "medium".
func TestListFeaturesOrdersByPriorityThenPosition(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	low, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Low", Priority: domain.PriorityLow}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create low: %v", err)
	}
	critical, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Critical", Priority: domain.PriorityCritical}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create critical: %v", err)
	}

	result, err := s.ListFeatures(ctx, "ABC", 0, "")
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	// critical (rank 0), then General (medium, rank 2), then low (rank 3).
	var gotOrder []string
	for _, f := range result.Features {
		gotOrder = append(gotOrder, f.Ref)
	}
	wantOrder := []string{critical.Ref, "ABC-F1", low.Ref}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("ListFeatures order = %v, want %v", gotOrder, wantOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("ListFeatures order = %v, want %v", gotOrder, wantOrder)
			break
		}
	}
}

// TestListFeaturesPaginates is ListFeatures' own pagination proof
// (store/features_test.go covers the raw query; this covers the
// service-level cursor encode/decode round trip a real caller uses).
func TestListFeaturesPaginates(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	// The auto-created General feature (ABC-F1) plus two more = 3 total.
	if _, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Second"}, testActor, testCorrelationID); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Third"}, testActor, testCorrelationID); err != nil {
		t.Fatalf("create third: %v", err)
	}

	page1, err := s.ListFeatures(ctx, "ABC", 2, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Features) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 features and a non-empty cursor", page1)
	}

	page2, err := s.ListFeatures(ctx, "ABC", 2, page1.NextCursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Features) != 1 || page2.NextCursor != "" {
		t.Fatalf("page2 = %+v, want 1 feature and no next cursor (last page)", page2)
	}
}

// TestListFeaturesRejectsCursorFromWrongView proves a cursor from a
// different view (the ticket priority queue's 4-part shape) is
// rejected as validation_failed rather than silently misread — the
// two cursors are deliberately different lengths (§ store.
// ListFeaturesForProjectPage's doc comment) specifically so this
// fails loudly instead of returning a plausible-looking wrong page.
func TestListFeaturesRejectsCursorFromWrongView(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T1"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create first ticket: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T2"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create second ticket: %v", err)
	}

	ticketPage, err := s.ListTickets(ctx, "ABC", TicketListViewPriorityQueue, 1, "")
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if ticketPage.NextCursor == "" {
		t.Fatalf("expected a non-empty priority-queue cursor to reuse against ListFeatures")
	}

	_, err = s.ListFeatures(ctx, "ABC", 10, ticketPage.NextCursor)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "cursor" {
		t.Fatalf("ListFeatures with a ticket priority-queue cursor = %v, want validation_failed on field cursor", err)
	}
}

// TestUpdateFeatureVersionConflict confirms a stale ExpectedVersion is
// rejected with the row's current version, mirroring
// TestUpdateTicketStatus's existing concurrency contract.
func TestUpdateFeatureVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	f, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	ref, err := domain.Parse(f.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	_, err = s.UpdateFeature(ctx, UpdateFeatureRequest{
		Ref: ref, Title: "Payments v2", Priority: domain.PriorityMedium, ExpectedVersion: f.Version + 1,
	}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("UpdateFeature with stale version = %v, want version_conflict", err)
	}
	if svcErr.CurrentVersion == nil || *svcErr.CurrentVersion != f.Version {
		t.Errorf("CurrentVersion = %v, want %d", svcErr.CurrentVersion, f.Version)
	}

	updated, err := s.UpdateFeature(ctx, UpdateFeatureRequest{
		Ref: ref, Title: "Payments v2", Priority: domain.PriorityHigh, ExpectedVersion: f.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateFeature with correct version: %v", err)
	}
	if updated.Title != "Payments v2" || updated.Priority != domain.PriorityHigh {
		t.Errorf("updated feature = %+v, want Title=%q Priority=high", updated, "Payments v2")
	}
}
