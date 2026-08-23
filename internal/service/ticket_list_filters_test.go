package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestListTicketsFilteredByStatus confirms a single filter narrows the
// result to only matching rows.
func TestListTicketsFilteredByStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	backlog, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "stays in backlog"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create backlog ticket: %v", err)
	}
	inProgress, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "moves to in_progress"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create second ticket: %v", err)
	}
	ref, err := domain.Parse(inProgress.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if _, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{Ref: ref, NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: inProgress.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("update status: %v", err)
	}

	page, err := s.ListTicketsFiltered(ctx, "ABC", TicketListViewPriorityQueue, 20, "", TicketListFilters{Status: domain.WorkflowStatusInProgress})
	if err != nil {
		t.Fatalf("ListTicketsFiltered(status=in_progress): %v", err)
	}
	if len(page.Tickets) != 1 || page.Tickets[0].Ref != inProgress.Ref {
		t.Fatalf("filtered page = %+v, want exactly %s", page.Tickets, inProgress.Ref)
	}
	_ = backlog
}

// TestListTicketsFilteredByAssignee confirms the assignee filter
// resolves the actor ref and narrows to only tickets assigned to it.
func TestListTicketsFilteredByAssignee(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	unassigned, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "unassigned"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create unassigned: %v", err)
	}
	assigned, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "assigned"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create assigned: %v", err)
	}
	ref, err := domain.Parse(assigned.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if _, err := s.AssignTicket(ctx, AssignTicketRequest{Ref: ref, Assignee: &testActor, ExpectedVersion: assigned.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AssignTicket: %v", err)
	}

	page, err := s.ListTicketsFiltered(ctx, "ABC", TicketListViewPriorityQueue, 20, "", TicketListFilters{Assignee: testActor.String()})
	if err != nil {
		t.Fatalf("ListTicketsFiltered(assignee): %v", err)
	}
	if len(page.Tickets) != 1 || page.Tickets[0].Ref != assigned.Ref {
		t.Fatalf("filtered page = %+v, want exactly %s", page.Tickets, assigned.Ref)
	}
	_ = unassigned
}

// TestListTicketsFilteredByFeatureRef confirms the feature_ref filter
// resolves the reference and narrows to that feature's tickets only,
// and rejects a feature reference from a different project.
func TestListTicketsFilteredByFeatureRef(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	mustCreateProject(t, s, "XYZ")

	secondFeature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Second feature"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create second feature: %v", err)
	}

	inGeneral, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "in General"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket in General: %v", err)
	}
	ref, err := domain.Parse(inGeneral.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	featureRef, err := domain.Parse(secondFeature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}
	moved, err := s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: ref, NewFeatureRef: featureRef, ExpectedVersion: inGeneral.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("move ticket to second feature: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "stays in General"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create second ticket in General: %v", err)
	}

	page, err := s.ListTicketsFiltered(ctx, "ABC", TicketListViewPriorityQueue, 20, "", TicketListFilters{FeatureRef: secondFeature.Ref})
	if err != nil {
		t.Fatalf("ListTicketsFiltered(feature_ref): %v", err)
	}
	if len(page.Tickets) != 1 || page.Tickets[0].Ref != moved.Ref {
		t.Fatalf("filtered page = %+v, want exactly %s", page.Tickets, moved.Ref)
	}

	// A feature reference from a different project is rejected, not
	// silently treated as "no such feature, empty result" — the ref
	// itself is well-formed, so this is the caller crossing a project
	// boundary by mistake, a validation error rather than a lookup miss.
	_, err = s.ListTicketsFiltered(ctx, "XYZ", TicketListViewPriorityQueue, 20, "", TicketListFilters{FeatureRef: secondFeature.Ref})
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "feature_ref" {
		t.Fatalf("cross-project feature_ref error = %v, want validation_failed on field feature_ref", err)
	}
}

// TestListTicketsFilteredCombinesWithAND confirms two filters compose
// with AND, not OR: a ticket matching only one of the two is excluded.
func TestListTicketsFilteredCombinesWithAND(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	sev := domain.SeverityHigh
	matches, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "high bug", Severity: &sev}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create matching ticket: %v", err)
	}
	lowSev := domain.SeverityLow
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "low bug", Severity: &lowSev}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create low-severity bug: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "high-priority task"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create task: %v", err)
	}

	page, err := s.ListTicketsFiltered(ctx, "ABC", TicketListViewPriorityQueue, 20, "", TicketListFilters{
		Type: domain.TicketTypeBug, Severity: domain.SeverityHigh,
	})
	if err != nil {
		t.Fatalf("ListTicketsFiltered(type+severity): %v", err)
	}
	if len(page.Tickets) != 1 || page.Tickets[0].Ref != matches.Ref {
		t.Fatalf("AND-combined filtered page = %+v, want exactly %s", page.Tickets, matches.Ref)
	}
}

// TestListTicketsFilteredInvalidEnum confirms an invalid filter value
// is rejected with validation_failed on the offending field, mirroring
// CreateTicket's own enum validation (ticket.go).
func TestListTicketsFilteredInvalidEnum(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	cases := []struct {
		name    string
		filters TicketListFilters
		field   string
	}{
		{"status", TicketListFilters{Status: "bogus"}, "status"},
		{"type", TicketListFilters{Type: "bogus"}, "type"},
		{"severity", TicketListFilters{Severity: "bogus"}, "severity"},
		{"priority", TicketListFilters{Priority: "bogus"}, "priority"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.ListTicketsFiltered(ctx, "ABC", TicketListViewPriorityQueue, 20, "", c.filters)
			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != c.field {
				t.Fatalf("ListTicketsFiltered(%s=bogus) error = %v, want validation_failed on field %s", c.name, err, c.field)
			}
		})
	}
}

// TestListTicketsFilteredUnknownAssignee confirms an actor reference
// that doesn't resolve to any actor is a validation error, not a
// silent empty page.
func TestListTicketsFilteredUnknownAssignee(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.ListTicketsFiltered(ctx, "ABC", TicketListViewPriorityQueue, 20, "", TicketListFilters{Assignee: "human:nobody"})
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "assignee" {
		t.Fatalf("ListTicketsFiltered(assignee=human:nobody) error = %v, want validation_failed on field assignee", err)
	}
}

// TestListTicketsFilteredInvalidUpdatedSince confirms a non-RFC3339
// updated_since value is rejected rather than silently ignored.
func TestListTicketsFilteredInvalidUpdatedSince(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.ListTicketsFiltered(ctx, "ABC", TicketListViewPriorityQueue, 20, "", TicketListFilters{UpdatedSince: "not-a-timestamp"})
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "updated_since" {
		t.Fatalf("ListTicketsFiltered(updated_since=not-a-timestamp) error = %v, want validation_failed on field updated_since", err)
	}
}

// TestListTicketsUnfilteredStillWorks confirms ListTickets (the
// existing, unfiltered entry point CLI/MCP callers use) is unaffected
// by ListTicketsFiltered's addition.
func TestListTicketsUnfilteredStillWorks(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	page, err := s.ListTickets(ctx, "ABC", TicketListViewPriorityQueue, 20, "")
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(page.Tickets) != 1 {
		t.Fatalf("ListTickets = %+v, want 1 ticket", page.Tickets)
	}
}
