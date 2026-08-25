package mcpsrv

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// TestInProcessBackendListTicketsFiltersByAssignee is the
// discriminating scenario Phase 7 closed: "find my assigned work" —
// §16 criterion 10's representative workflow's first step — was
// impossible over MCP because tickets_list exposed no filters at all,
// even though GET /projects/{key}/tickets always supported one
// (docs/contracts/list-filters.md). Seeds two tickets, assigns only
// one to the calling agent, and confirms an assignee-filtered
// tickets_list call returns exactly that one.
func TestInProcessBackendListTicketsFiltersByAssignee(t *testing.T) {
	backend, existingRef := newTestBackend(t)
	_, agentActor := mustIssueAgentToken(t, backend, "codex")
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{Actor: agentActor, Permission: auth.PermissionEditor, AuthMethod: "bearer"})

	ref, err := domain.Parse(existingRef)
	if err != nil {
		t.Fatalf("parse existing ticket ref: %v", err)
	}
	assigned, err := backend.Svc.AssignTicket(ctx, service.AssignTicketRequest{
		Ref: ref, Assignee: &agentActor, ExpectedVersion: 1,
	}, agentActor, service.NewCorrelationID())
	if err != nil {
		t.Fatalf("AssignTicket: %v", err)
	}
	if _, err := backend.Svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Unassigned ticket",
	}, agentActor, service.NewCorrelationID(), "", ""); err != nil {
		t.Fatalf("CreateTicket (unassigned): %v", err)
	}

	unfiltered, err := backend.ListTickets(ctx, "ABC", "priority_queue", TicketListFilters{}, 20, "")
	if err != nil {
		t.Fatalf("ListTickets (unfiltered): %v", err)
	}
	if len(unfiltered.Tickets) != 2 {
		t.Fatalf("unfiltered ListTickets = %d tickets, want 2", len(unfiltered.Tickets))
	}

	assigneeRef := "agent:" + agentActor.Name
	filtered, err := backend.ListTickets(ctx, "ABC", "priority_queue", TicketListFilters{Assignee: assigneeRef}, 20, "")
	if err != nil {
		t.Fatalf("ListTickets (assignee filter): %v", err)
	}
	if len(filtered.Tickets) != 1 || filtered.Tickets[0].Ref != assigned.Ref {
		t.Fatalf("assignee-filtered ListTickets = %+v, want exactly [%s]", filtered.Tickets, assigned.Ref)
	}
}
