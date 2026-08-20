package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestListTicketsPriorityQueuePagination confirms the default view
// paginates correctly and its cursor round-trips.
func TestListTicketsPriorityQueuePagination(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	for _, title := range []string{"A", "B", "C"} {
		if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: title}, testActor, testCorrelationID, "", ""); err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
	}

	page1, err := s.ListTickets(ctx, "ABC", TicketListViewPriorityQueue, 2, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Tickets) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 tickets and a non-empty cursor", page1)
	}

	page2, err := s.ListTickets(ctx, "ABC", TicketListViewPriorityQueue, 2, page1.NextCursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Tickets) != 1 || page2.NextCursor != "" {
		t.Fatalf("page2 = %+v, want 1 ticket and no next cursor (last page)", page2)
	}
}

// TestListTicketsIssueRegisterFiltersToIssueTypes confirms the issue
// register view only returns bug/security tickets, ordered
// severity-first.
func TestListTicketsIssueRegisterFiltersToIssueTypes(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "not an issue"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create task: %v", err)
	}
	sev := domain.SeverityHigh
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "a bug", Severity: &sev}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create bug: %v", err)
	}

	page, err := s.ListTickets(ctx, "ABC", TicketListViewIssueRegister, 20, "")
	if err != nil {
		t.Fatalf("ListTickets(issue_register): %v", err)
	}
	if len(page.Tickets) != 1 || page.Tickets[0].Title != "a bug" {
		t.Fatalf("issue register = %+v, want exactly the one bug ticket", page.Tickets)
	}
}

// TestListTicketsRejectsCursorFromTheOtherView is the cross-view
// cursor-shape check the plan calls for: PriorityQueue's cursor has 4
// components, IssueRegister's has 5 — DecodeCursor's component-count
// check must reject a cursor from one view handed to the other with a
// clean validation error, not silently decode it into the wrong
// fields (which would produce a page that's wrong, not one that
// errors, the harder failure mode to notice).
func TestListTicketsRejectsCursorFromTheOtherView(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	for i := 0; i < 3; i++ {
		if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", ""); err != nil {
			t.Fatalf("create ticket %d: %v", i, err)
		}
	}

	pqPage, err := s.ListTickets(ctx, "ABC", TicketListViewPriorityQueue, 2, "")
	if err != nil {
		t.Fatalf("priority queue page: %v", err)
	}
	if pqPage.NextCursor == "" {
		t.Fatalf("expected a non-empty priority-queue cursor to reuse against issue_register")
	}

	_, err = s.ListTickets(ctx, "ABC", TicketListViewIssueRegister, 2, pqPage.NextCursor)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "cursor" {
		t.Fatalf("issue_register with a priority-queue cursor = %v, want validation_failed on field cursor", err)
	}
}

// TestListTicketsInvalidView confirms an unrecognized view name is
// rejected rather than silently falling back to the default.
func TestListTicketsInvalidView(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.ListTickets(ctx, "ABC", TicketListView("bogus"), 20, "")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "view" {
		t.Fatalf("ListTickets with an invalid view = %v, want validation_failed on field view", err)
	}
}
