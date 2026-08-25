package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestListActivityIncludesTicketAndCommentEvents proves the feed
// combines comments and audit events (§5.10): creating the project,
// creating a ticket, and commenting on it all show up, newest first,
// with the right actor, entity reference, and (for the comment) an
// excerpt of its body.
func TestListActivityIncludesTicketAndCommentEvents(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "First comment"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	result, err := s.ListActivity(ctx, "ABC", ActivityListFilters{}, 10, "")
	if err != nil {
		t.Fatalf("ListActivity: %v", err)
	}
	if len(result.Events) != 3 {
		t.Fatalf("events = %+v, want 3 (project_created, ticket_created, comment_added)", result.Events)
	}
	// Newest first: the comment (added second) comes before ticket_created.
	if result.Events[0].EventType != eventCommentAdded {
		t.Errorf("Events[0].EventType = %q, want %q (newest first)", result.Events[0].EventType, eventCommentAdded)
	}
	if result.Events[0].CommentBody == nil || *result.Events[0].CommentBody != "First comment" {
		t.Errorf("Events[0].CommentBody = %v, want %q", result.Events[0].CommentBody, "First comment")
	}
	if result.Events[0].EntityRef != ticket.Ref {
		t.Errorf("Events[0].EntityRef = %q, want %q", result.Events[0].EntityRef, ticket.Ref)
	}
	if result.Events[0].Actor != testActor {
		t.Errorf("Events[0].Actor = %v, want %v", result.Events[0].Actor, testActor)
	}
	if result.Events[1].EventType != eventTicketCreated {
		t.Errorf("Events[1].EventType = %q, want %q", result.Events[1].EventType, eventTicketCreated)
	}
}

// TestListActivityIncludesProjectCommentWithEmptyEntityRef is Phase 6
// Step 2's regression test: a comment on a project itself is a new
// case (comments were ticket-only before this phase) that
// ListActivityPage's `WHERE (e.project_id = ? OR e.id = ?)` already
// handles via the `e.id = ?` branch (the same one project_created
// uses), and activityEntityRef already returns "" for KindProject
// rather than erroring — this confirms the feed page actually renders
// rather than 500ing once a project comment exists.
func TestListActivityIncludesProjectCommentWithEmptyEntityRef(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	projectRef := domain.Reference{ProjectKey: "ABC", Kind: domain.KindProject}
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: projectRef, Body: "kickoff"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("AddComment on project: %v", err)
	}

	result, err := s.ListActivity(ctx, "ABC", ActivityListFilters{}, 10, "")
	if err != nil {
		t.Fatalf("ListActivity: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("events = %+v, want 2 (project_created, comment_added)", result.Events)
	}
	if result.Events[0].EventType != eventCommentAdded {
		t.Fatalf("Events[0].EventType = %q, want %q", result.Events[0].EventType, eventCommentAdded)
	}
	if result.Events[0].EntityRef != "" {
		t.Errorf("Events[0].EntityRef = %q, want empty for a project-kind entity", result.Events[0].EntityRef)
	}
	if result.Events[0].EntityKind != domain.KindProject {
		t.Errorf("Events[0].EntityKind = %q, want %q", result.Events[0].EntityKind, domain.KindProject)
	}
}

// TestListActivityFiltersByEventType proves the event_type filter
// actually narrows the query rather than being silently ignored.
func TestListActivityFiltersByEventType(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	mustCreateTicket(t, s, "ABC", "T1")
	mustCreateTicket(t, s, "ABC", "T2")

	result, err := s.ListActivity(ctx, "ABC", ActivityListFilters{EventType: eventTicketCreated}, 10, "")
	if err != nil {
		t.Fatalf("ListActivity: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("events = %+v, want 2 ticket_created events", result.Events)
	}
	for _, e := range result.Events {
		if e.EventType != eventTicketCreated {
			t.Errorf("event = %+v, want only ticket_created", e)
		}
	}
}

// TestListActivityRejectsInvalidEventType proves the event_type filter
// is validated against the allowlist, not passed through blind.
func TestListActivityRejectsInvalidEventType(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.ListActivity(ctx, "ABC", ActivityListFilters{EventType: "not_a_real_event"}, 10, "")
	if !isValidationError(err, "event_type") {
		t.Errorf("ListActivity with invalid event_type = %v, want validation_failed on field event_type", err)
	}
}

// TestListActivityFiltersByEntityKind proves the entity_kind filter
// narrows to only the requested kind (a feature_created event should
// not appear when filtering for ticket).
func TestListActivityFiltersByEntityKind(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	mustCreateTicket(t, s, "ABC", "T1")
	if _, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "F1"}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	result, err := s.ListActivity(ctx, "ABC", ActivityListFilters{EntityKind: string(domain.KindFeature)}, 10, "")
	if err != nil {
		t.Fatalf("ListActivity: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].EntityKind != domain.KindFeature {
		t.Fatalf("events = %+v, want exactly 1 feature-kind event", result.Events)
	}
}

// TestListActivityPaginates mirrors TestListDecisionsPaginates: a
// second page continues strictly before the first page's oldest row.
func TestListActivityPaginates(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	for _, title := range []string{"First", "Second", "Third"} {
		mustCreateTicket(t, s, "ABC", title)
	}

	page1, err := s.ListActivity(ctx, "ABC", ActivityListFilters{}, 2, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Events) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 events and a non-empty cursor", page1)
	}
	page2, err := s.ListActivity(ctx, "ABC", ActivityListFilters{}, 2, page1.NextCursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Events) != 2 || page2.NextCursor != "" {
		t.Fatalf("page2 = %+v, want 2 events (the project_created event) and no next cursor", page2)
	}
	seen := map[int64]bool{}
	for _, e := range append(page1.Events, page2.Events...) {
		if seen[e.ID] {
			t.Errorf("event id %d appeared on both pages", e.ID)
		}
		seen[e.ID] = true
	}
}

// isValidationError checks err is a *Error with Code
// domain.ErrValidationFailed and the given field, without exposing
// *Error's zero-value ambiguity to every call site.
func isValidationError(err error, field string) bool {
	svcErr, ok := err.(*Error)
	if !ok {
		return false
	}
	return svcErr.Code == domain.ErrValidationFailed && svcErr.Field == field
}
