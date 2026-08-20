package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TestTicketLifecycleAuditTrail is verification gate 3 (product spec
// §14's Phase 1 exit criterion): drive one ticket through create ->
// assign -> move feature -> reprioritize -> reorder -> comment -> edit
// comment -> relate -> soft-delete -> restore, and assert the audit
// trail contains exactly the expected event sequence, each one
// attributed to testActor. This is the only test that would catch a
// missing/duplicated emission or an event landing on the wrong
// entity — every operation above already has its own narrower test,
// but none of them checks the trail as a whole.
func TestTicketLifecycleAuditTrail(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	// create
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Lifecycle ticket",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	other := mustCreateTicket(t, s, "ABC", "Relate target")
	otherRef, err := domain.Parse(other.Ref)
	if err != nil {
		t.Fatalf("parse other ref: %v", err)
	}
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	featureRef, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}

	// assign
	ticket, err = s.AssignTicket(ctx, AssignTicketRequest{Ref: ref, Assignee: &testActor, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("AssignTicket: %v", err)
	}

	// move feature
	ticket, err = s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: ref, NewFeatureRef: featureRef, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("MoveTicketFeature: %v", err)
	}

	// reprioritize
	ticket, err = s.UpdateTicketFields(ctx, UpdateTicketFieldsRequest{
		Ref: ref, Type: domain.TicketTypeTask, Title: "Lifecycle ticket", Priority: domain.PriorityHigh, ExpectedVersion: ticket.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateTicketFields: %v", err)
	}

	// reorder (move to head of its own group — a no-op-shaped move is
	// still a real ReorderTicket call and still emits its event)
	ticket, err = s.ReorderTicket(ctx, ReorderTicketRequest{Ref: ref, AfterRef: nil, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("ReorderTicket: %v", err)
	}

	// comment, then edit it
	comment, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "First pass"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := s.EditComment(ctx, EditCommentRequest{CommentID: comment.ID, Body: "Revised", ExpectedVersion: comment.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("EditComment: %v", err)
	}

	// relate
	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: ref, TargetRef: otherRef, Type: domain.RelationshipRelatedTo}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}

	// soft-delete, then restore
	newVersion, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if _, err := s.RestoreTicket(ctx, RestoreTicketRequest{Ref: ref, ExpectedVersion: newVersion}, testActor, testCorrelationID); err != nil {
		t.Fatalf("RestoreTicket: %v", err)
	}

	entityID := mustEntityIDByUUID(t, s, ticket.UUID)
	events, err := store.ListAuditEvents(ctx, s.store.DB(), entityID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}

	wantTypes := []string{
		eventTicketCreated,
		eventTicketAssigned,
		eventTicketMoved,
		eventTicketUpdated,
		eventTicketReordered,
		eventCommentAdded,
		eventCommentEdited,
		eventRelationshipAdded,
		eventTicketDeleted,
		eventTicketRestored,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("got %d audit events, want %d\ngot:  %+v\nwant: %v", len(events), len(wantTypes), events, wantTypes)
	}
	for i, want := range wantTypes {
		if events[i].EventType != want {
			t.Errorf("events[%d].EventType = %q, want %q", i, events[i].EventType, want)
		}
		wantActorID := mustTestActorID(t, s)
		if events[i].ActorID != wantActorID {
			t.Errorf("events[%d].ActorID = %d, want %d (testActor)", i, events[i].ActorID, wantActorID)
		}
		if events[i].CorrelationID != testCorrelationID {
			t.Errorf("events[%d].CorrelationID = %q, want %q", i, events[i].CorrelationID, testCorrelationID)
		}
	}
}
