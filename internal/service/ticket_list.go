package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TicketListView selects which store query backs ListTickets: the
// priority queue (product spec §5.6, the default — every ticket,
// ordered by priority then position) or the issue register (§5.5,
// bug/security tickets only, ordered severity-first).
type TicketListView string

const (
	TicketListViewPriorityQueue TicketListView = "priority_queue"
	TicketListViewIssueRegister TicketListView = "issue_register"
)

// TicketsListResult is ListTickets' output.
type TicketsListResult struct {
	Tickets    []domain.Ticket
	NextCursor string
}

// parseCursorInt treats an empty cursor component as 0 — DecodeCursor
// returns wantParts empty strings for the first page (cursor.go), and
// every ordering column PriorityQueue/IssueRegister compare against
// legitimately starts at 0 for a project's first ticket.
func parseCursorInt(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// decodePriorityQueueCursor decodes the 4-part (priority_rank,
// position, created_at, id) cursor store.PriorityQueue's ORDER BY
// uses — not store.DecodeCreatedAtIDCursor's generic 2-part shape,
// and not IssueRegister's 5-part one either: handing a cursor from one
// view to the other must fail cleanly (DecodeCursor's component-count
// check), not silently decode into the wrong fields.
func decodePriorityQueueCursor(cursor string) (rank, position int64, createdAt string, id int64, err error) {
	parts, err := store.DecodeCursor(cursor, 4)
	if err != nil {
		return 0, 0, "", 0, err
	}
	if rank, err = parseCursorInt(parts[0]); err != nil {
		return 0, 0, "", 0, err
	}
	if position, err = parseCursorInt(parts[1]); err != nil {
		return 0, 0, "", 0, err
	}
	if id, err = parseCursorInt(parts[3]); err != nil {
		return 0, 0, "", 0, err
	}
	return rank, position, parts[2], id, nil
}

// decodeIssueRegisterCursor is decodePriorityQueueCursor's counterpart
// for the issue register's 5-part (severity_rank, priority_rank,
// position, created_at, id) cursor.
func decodeIssueRegisterCursor(cursor string) (severityRank, priorityRank, position int64, createdAt string, id int64, err error) {
	parts, err := store.DecodeCursor(cursor, 5)
	if err != nil {
		return 0, 0, 0, "", 0, err
	}
	if severityRank, err = parseCursorInt(parts[0]); err != nil {
		return 0, 0, 0, "", 0, err
	}
	if priorityRank, err = parseCursorInt(parts[1]); err != nil {
		return 0, 0, 0, "", 0, err
	}
	if position, err = parseCursorInt(parts[2]); err != nil {
		return 0, 0, 0, "", 0, err
	}
	if id, err = parseCursorInt(parts[4]); err != nil {
		return 0, 0, 0, "", 0, err
	}
	return severityRank, priorityRank, position, parts[3], id, nil
}

// ListTickets wraps store.PriorityQueue/IssueRegister, decoding the
// cursor for whichever view was asked for and clamping limit the same
// way ListProjects does (project.go).
func (s *Service) ListTickets(ctx context.Context, projectKey string, view TicketListView, limit int, cursor string) (TicketsListResult, error) {
	proj, err := store.GetProjectByKey(ctx, s.store.DB(), projectKey)
	if errors.Is(err, store.ErrNotFound) {
		return TicketsListResult{}, newNotFoundError("project %q not found", projectKey)
	}
	if err != nil {
		return TicketsListResult{}, fmt.Errorf("service: look up project: %w", err)
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	var page store.TicketsPage
	switch view {
	case "", TicketListViewPriorityQueue:
		rank, position, createdAt, id, derr := decodePriorityQueueCursor(cursor)
		if derr != nil {
			return TicketsListResult{}, newValidationError("cursor", "invalid cursor")
		}
		page, err = store.PriorityQueue(ctx, s.store.DB(), proj.ID, limit, rank, position, createdAt, id)
	case TicketListViewIssueRegister:
		severityRank, priorityRank, position, createdAt, id, derr := decodeIssueRegisterCursor(cursor)
		if derr != nil {
			return TicketsListResult{}, newValidationError("cursor", "invalid cursor")
		}
		page, err = store.IssueRegister(ctx, s.store.DB(), proj.ID, limit, severityRank, priorityRank, position, createdAt, id)
	default:
		return TicketsListResult{}, newValidationError("view", "invalid view %q, want %q or %q", view, TicketListViewPriorityQueue, TicketListViewIssueRegister)
	}
	if err != nil {
		return TicketsListResult{}, fmt.Errorf("service: list tickets: %w", err)
	}

	tickets := make([]domain.Ticket, len(page.Tickets))
	for i, row := range page.Tickets {
		tickets[i] = row.Entity
	}
	return TicketsListResult{Tickets: tickets, NextCursor: page.NextCursor}, nil
}
