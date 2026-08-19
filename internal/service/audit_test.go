package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
	"github.com/google/uuid"
)

// TestCreateProjectEmitsAuditEvent is Step 4a's core assertion: every
// mutation attributes its audit_events row to the actor and
// correlation id the caller supplied (ADR 0012), not a placeholder.
func TestCreateProjectEmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	proj, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	entityID := mustEntityIDByUUID(t, s, proj.UUID)
	events, err := store.ListAuditEvents(ctx, s.store.DB(), entityID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d audit events, want 1", len(events))
	}
	ev := events[0]
	if ev.EventType != eventProjectCreated {
		t.Errorf("EventType = %q, want %q", ev.EventType, eventProjectCreated)
	}
	if ev.CorrelationID != testCorrelationID {
		t.Errorf("CorrelationID = %q, want %q", ev.CorrelationID, testCorrelationID)
	}
	wantActorID := mustTestActorID(t, s)
	if ev.ActorID != wantActorID {
		t.Errorf("ActorID = %d, want %d (the resolved test actor)", ev.ActorID, wantActorID)
	}
}

// TestCreateTicketEmitsAuditEvent mirrors the project case for tickets.
func TestCreateTicketEmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	entityID := mustEntityIDByUUID(t, s, ticket.UUID)
	events, err := store.ListAuditEvents(ctx, s.store.DB(), entityID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventType != eventTicketCreated {
		t.Fatalf("got %+v, want exactly one %q event", events, eventTicketCreated)
	}
}

// TestUpdateTicketStatusEmitsAuditEvent asserts the status-update event
// carries a from/to changes payload, not an empty one.
func TestUpdateTicketStatusEmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	if _, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
		Ref: ref, NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: ticket.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}

	entityID := mustEntityIDByUUID(t, s, ticket.UUID)
	events, err := store.ListAuditEvents(ctx, s.store.DB(), entityID)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	// ticket_created, then ticket_status_changed, oldest first.
	if len(events) != 2 {
		t.Fatalf("got %d audit events, want 2 (created + status changed): %+v", len(events), events)
	}
	if events[0].EventType != eventTicketCreated {
		t.Errorf("events[0].EventType = %q, want %q", events[0].EventType, eventTicketCreated)
	}
	if events[1].EventType != eventTicketStatusChanged {
		t.Errorf("events[1].EventType = %q, want %q", events[1].EventType, eventTicketStatusChanged)
	}
	if events[1].Changes == "{}" || events[1].Changes == "" {
		t.Errorf("status-change event has empty Changes, want a from/to payload")
	}
}

func mustEntityIDByUUID(t *testing.T, s *Service, uuidStr string) int64 {
	t.Helper()
	u, err := uuid.Parse(uuidStr)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", uuidStr, err)
	}
	var id int64
	if err := s.store.DB().QueryRow(`SELECT id FROM entities WHERE uuid = ?`, u[:]).Scan(&id); err != nil {
		t.Fatalf("resolve entity id for uuid %s: %v", uuidStr, err)
	}
	return id
}

func mustTestActorID(t *testing.T, s *Service) int64 {
	t.Helper()
	id, err := store.GetActorIDByRef(context.Background(), s.store.DB(), testActor.Kind, testActor.Name)
	if err != nil {
		t.Fatalf("resolve test actor id: %v", err)
	}
	return id
}
