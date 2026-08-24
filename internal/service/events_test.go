package service

import (
	"context"
	"sync"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// fakeBroadcaster records every published hint, for tests asserting a
// mutation did (or did not) publish one — the same role a real
// internal/httpapi.Hub plays, without an HTTP server.
type fakeBroadcaster struct {
	mu    sync.Mutex
	hints []ChangeHint
}

func (f *fakeBroadcaster) Publish(hint ChangeHint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hints = append(f.hints, hint)
}

func (f *fakeBroadcaster) all() []ChangeHint {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ChangeHint, len(f.hints))
	copy(out, f.hints)
	return out
}

func TestCreateTicketBroadcastsEntityChanged(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	fb := &fakeBroadcaster{}
	s.SetBroadcaster(fb)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "EVT", Title: "Events"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	fb.mu.Lock()
	fb.hints = nil // drop the CreateProject/General-feature hints; this test is about the ticket create
	fb.mu.Unlock()

	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "EVT", Type: domain.TicketTypeTask, Title: "Watch me"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	hints := fb.all()
	if len(hints) != 1 {
		t.Fatalf("hints = %#v, want exactly one entity_changed hint", hints)
	}
	if hints[0].Kind != HintEntityChanged || hints[0].Ref != ticket.Ref || hints[0].Project != "EVT" {
		t.Errorf("hint = %#v, want {%s %s EVT}", hints[0], HintEntityChanged, ticket.Ref)
	}
}

func TestFailedMutationDoesNotBroadcast(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	fb := &fakeBroadcaster{}
	s.SetBroadcaster(fb)

	// A version conflict rolls the transaction back — no hint should
	// ever reach a subscriber for a mutation that never committed.
	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "EVT", Title: "Events"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "EVT", Type: domain.TicketTypeTask, Title: "Watch me"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	fb.mu.Lock()
	fb.hints = nil
	fb.mu.Unlock()

	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	_, err = s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{Ref: ref, NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: ticket.Version + 99}, testActor, testCorrelationID)
	if err == nil {
		t.Fatal("expected a version conflict")
	}

	if hints := fb.all(); len(hints) != 0 {
		t.Errorf("hints after a failed mutation = %#v, want none", hints)
	}
}

func TestAssignTicketBroadcastsNotificationsChangedToAssignee(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")
	fb := &fakeBroadcaster{}
	s.SetBroadcaster(fb)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "EVT", Title: "Events"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "EVT", Type: domain.TicketTypeTask, Title: "Assign me"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	fb.mu.Lock()
	fb.hints = nil
	fb.mu.Unlock()

	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if _, err := s.AssignTicket(ctx, AssignTicketRequest{Ref: ref, Assignee: &testActorAlice, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("assign ticket: %v", err)
	}

	var sawEntity, sawNotification bool
	for _, h := range fb.all() {
		switch h.Kind {
		case HintEntityChanged:
			sawEntity = true
		case HintNotificationsChanged:
			sawNotification = true
			if h.Actor != testActorAlice.String() {
				t.Errorf("notifications_changed actor = %q, want %q", h.Actor, testActorAlice.String())
			}
		}
	}
	if !sawEntity {
		t.Error("expected an entity_changed hint for the assigned ticket")
	}
	if !sawNotification {
		t.Error("expected a notifications_changed hint for the new assignee")
	}
}

func TestNilBroadcasterIsSafe(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t) // no SetBroadcaster call
	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "EVT", Title: "Events"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project with no broadcaster registered: %v", err)
	}
}
