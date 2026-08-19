package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestUpdateTicketFieldsRoundTrips confirms a field update writes and
// re-reads correctly, including switching type into one that allows
// severity.
func TestUpdateTicketFieldsRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	sev := domain.SeverityHigh
	updated, err := s.UpdateTicketFields(ctx, UpdateTicketFieldsRequest{
		Ref: ref, Type: domain.TicketTypeBug, Title: "Fixed title", Description: "desc",
		Priority: domain.PriorityCritical, Severity: &sev, ExpectedVersion: ticket.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateTicketFields: %v", err)
	}
	if updated.Type != domain.TicketTypeBug || updated.Title != "Fixed title" || updated.Priority != domain.PriorityCritical {
		t.Errorf("updated = %+v, want type=bug title=%q priority=critical", updated, "Fixed title")
	}
	if updated.Severity == nil || *updated.Severity != domain.SeverityHigh {
		t.Errorf("Severity = %v, want high", updated.Severity)
	}
}

// TestUpdateTicketFieldsRejectsSeverityOnTask mirrors CreateTicket's
// existing rule: severity only applies to bug/security tickets.
func TestUpdateTicketFieldsRejectsSeverityOnTask(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	sev := domain.SeverityHigh
	_, err = s.UpdateTicketFields(ctx, UpdateTicketFieldsRequest{
		Ref: ref, Type: domain.TicketTypeTask, Title: "T", Priority: domain.PriorityMedium,
		Severity: &sev, ExpectedVersion: ticket.Version,
	}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("UpdateTicketFields with severity on task = %v, want validation_failed", err)
	}
}

// TestAssignAndUnassignTicket confirms assigning stamps the actor and
// unassigning clears it, both readable back off GetTicket.
func TestAssignAndUnassignTicket(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	assigned, err := s.AssignTicket(ctx, AssignTicketRequest{Ref: ref, Assignee: &testActor, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("AssignTicket: %v", err)
	}
	if assigned.Assignee == nil || *assigned.Assignee != testActor {
		t.Fatalf("Assignee = %v, want %v", assigned.Assignee, testActor)
	}

	unassigned, err := s.AssignTicket(ctx, AssignTicketRequest{Ref: ref, Assignee: nil, ExpectedVersion: assigned.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if unassigned.Assignee != nil {
		t.Fatalf("Assignee after unassign = %v, want nil", unassigned.Assignee)
	}
}

// TestAssignTicketRejectsUnknownActor confirms assigning to an actor
// with no seeded row fails validation rather than writing a dangling
// assignee_id.
func TestAssignTicketRejectsUnknownActor(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	unknown := domain.ActorRef{Kind: domain.ActorAgent, Name: "does-not-exist"}
	_, err = s.AssignTicket(ctx, AssignTicketRequest{Ref: ref, Assignee: &unknown, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("AssignTicket to unknown actor = %v, want validation_failed", err)
	}
}

// TestMoveTicketFeatureWithinProject confirms a ticket moves from
// General to a new feature and back is reflected in FeatureRef.
func TestMoveTicketFeatureWithinProject(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if ticket.FeatureRef != "ABC-F1" {
		t.Fatalf("initial FeatureRef = %q, want ABC-F1 (General)", ticket.FeatureRef)
	}
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	featureRef, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}

	moved, err := s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: ticketRef, NewFeatureRef: featureRef, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("MoveTicketFeature: %v", err)
	}
	if moved.FeatureRef != feature.Ref {
		t.Errorf("FeatureRef after move = %q, want %q", moved.FeatureRef, feature.Ref)
	}
}

// TestMoveTicketFeatureRejectsCrossProject is ADR 0001's rule: a
// ticket cannot move to a feature in a different project.
func TestMoveTicketFeatureRejectsCrossProject(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	mustCreateProject(t, s, "XYZ")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	otherFeature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "XYZ", Title: "Other", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	featureRef, err := domain.Parse(otherFeature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}

	_, err = s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: ticketRef, NewFeatureRef: featureRef, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("MoveTicketFeature cross-project = %v, want validation_failed", err)
	}
}
