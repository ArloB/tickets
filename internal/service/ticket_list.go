package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

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
// way ListProjects does (project.go). Applies no filtering — CLI and
// MCP callers list unfiltered pages (plan.md §7.2 scoped MCP's list
// tools to plain view= selection, not the full filter surface); the
// web UI's backlog/board views want filters and call
// ListTicketsFiltered instead.
func (s *Service) ListTickets(ctx context.Context, projectKey string, view TicketListView, limit int, cursor string) (TicketsListResult, error) {
	return s.ListTicketsFiltered(ctx, projectKey, view, limit, cursor, TicketListFilters{})
}

// TicketListFilters holds optional, AND-composed filters for
// ListTicketsFiltered (docs/contracts/list-filters.md): every
// non-empty field narrows the result further, on top of whichever view
// (priority queue vs. issue register) selects the base ordering. The
// zero value applies no filtering — TicketFilters do not encode into
// the pagination cursor, so a caller must resupply the same filters on
// every page of a given listing; a cursor obtained under one set of
// filters is replayed as-is under a different set rather than
// rejected, exactly like a bare page-2 request with a changed ?view=
// would misbehave today (docs/contracts/list-filters.md's "filters and
// cursors" section documents this deliberately-simple contract).
type TicketListFilters struct {
	Status       domain.WorkflowStatus
	Type         domain.TicketType
	Severity     domain.Severity
	Priority     domain.Priority
	FeatureRef   string // feature reference, e.g. "ABC-F1"; "" = any
	Assignee     string // actor ref wire form, e.g. "human:alice"; "" = any
	Creator      string // actor ref wire form; "" = any
	UpdatedSince string // RFC3339 timestamp; "" = any
}

// resolveTicketFilters validates TicketListFilters' enum-shaped fields
// and resolves its reference-shaped fields (FeatureRef/Assignee/
// Creator) to the internal ids store.TicketFilters needs, returning a
// *Error (never a bare error) on any invalid input — mirrors
// CreateTicket's validation style (ticket.go).
func (s *Service) resolveTicketFilters(ctx context.Context, projectKey string, f TicketListFilters) (store.TicketFilters, *Error) {
	var out store.TicketFilters

	if f.Status != "" {
		if !f.Status.Valid() {
			return store.TicketFilters{}, newValidationError("status", "invalid status %q", f.Status)
		}
		out.Status = string(f.Status)
	}
	if f.Type != "" {
		if !f.Type.Valid() {
			return store.TicketFilters{}, newValidationError("type", "invalid ticket type %q", f.Type)
		}
		out.Type = string(f.Type)
	}
	if f.Severity != "" {
		if !f.Severity.Valid() {
			return store.TicketFilters{}, newValidationError("severity", "invalid severity %q", f.Severity)
		}
		out.Severity = string(f.Severity)
	}
	if f.Priority != "" {
		if !f.Priority.Valid() {
			return store.TicketFilters{}, newValidationError("priority", "invalid priority %q", f.Priority)
		}
		out.Priority = string(f.Priority)
	}
	if f.FeatureRef != "" {
		ref, perr := domain.Parse(f.FeatureRef)
		if perr != nil || ref.Kind != domain.KindFeature {
			return store.TicketFilters{}, newValidationError("feature_ref", "invalid feature reference %q", f.FeatureRef)
		}
		if ref.ProjectKey != projectKey {
			return store.TicketFilters{}, newValidationError("feature_ref", "feature %q does not belong to project %q", f.FeatureRef, projectKey)
		}
		feature, ferr := store.GetFeatureByRef(ctx, s.store.DB(), ref)
		if errors.Is(ferr, store.ErrNotFound) {
			return store.TicketFilters{}, newValidationError("feature_ref", "feature %q not found", f.FeatureRef)
		}
		if ferr != nil {
			return store.TicketFilters{}, &Error{Code: domain.ErrInternal, Message: fmt.Sprintf("service: look up feature filter: %v", ferr)}
		}
		out.FeatureID = feature.ID
	}
	if f.Assignee != "" {
		id, aerr := s.resolveActorFilterID(ctx, "assignee", f.Assignee)
		if aerr != nil {
			return store.TicketFilters{}, aerr
		}
		out.AssigneeID = id
	}
	if f.Creator != "" {
		id, aerr := s.resolveActorFilterID(ctx, "creator", f.Creator)
		if aerr != nil {
			return store.TicketFilters{}, aerr
		}
		out.CreatorID = id
	}
	if f.UpdatedSince != "" {
		formatted, terr := formatUpdatedSinceFilter(f.UpdatedSince)
		if terr != nil {
			return store.TicketFilters{}, terr
		}
		out.UpdatedSince = formatted
	}
	return out, nil
}

// resolveActorFilterID parses an actor ref wire string and resolves it
// to an internal actor id for an assignee/creator filter, sharing one
// error shape between the two call sites in resolveTicketFilters.
func (s *Service) resolveActorFilterID(ctx context.Context, field, ref string) (int64, *Error) {
	parsed, perr := domain.ParseActorRef(ref)
	if perr != nil {
		return 0, newValidationError(field, "invalid actor reference %q", ref)
	}
	id, err := store.GetActorIDByRef(ctx, s.store.DB(), parsed.Kind, parsed.Name)
	if errors.Is(err, store.ErrNotFound) {
		return 0, newValidationError(field, "actor %q not found", ref)
	}
	if err != nil {
		return 0, &Error{Code: domain.ErrInternal, Message: fmt.Sprintf("service: look up %s filter: %v", field, err)}
	}
	return id, nil
}

// formatUpdatedSinceFilter parses an RFC3339 wire timestamp and
// reformats it into store.TimeLayout — the fixed-width, UTC form
// entities.updated_at is stored in and compared against
// lexicographically (store.TimeLayout's own doc comment). Comparing a
// bare RFC3339 string against a TimeLayout column would silently
// misorder the moment their fractional-second digit counts differ.
func formatUpdatedSinceFilter(raw string) (string, *Error) {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", newValidationError("updated_since", "invalid timestamp %q, want RFC3339", raw)
	}
	return t.UTC().Format(store.TimeLayout), nil
}

// ListTicketsFiltered is ListTickets plus TicketListFilters — the web
// UI's backlog/board/priority-queue/issue-register views call this
// directly (internal/httpapi/tickets.go); ListTickets is unchanged and
// remains the entry point for callers that never filter.
func (s *Service) ListTicketsFiltered(ctx context.Context, projectKey string, view TicketListView, limit int, cursor string, filters TicketListFilters) (TicketsListResult, error) {
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

	storeFilters, ferr := s.resolveTicketFilters(ctx, projectKey, filters)
	if ferr != nil {
		return TicketsListResult{}, ferr
	}

	var page store.TicketsPage
	switch view {
	case "", TicketListViewPriorityQueue:
		rank, position, createdAt, id, derr := decodePriorityQueueCursor(cursor)
		if derr != nil {
			return TicketsListResult{}, newValidationError("cursor", "invalid cursor")
		}
		page, err = store.PriorityQueue(ctx, s.store.DB(), proj.ID, storeFilters, limit, rank, position, createdAt, id)
	case TicketListViewIssueRegister:
		severityRank, priorityRank, position, createdAt, id, derr := decodeIssueRegisterCursor(cursor)
		if derr != nil {
			return TicketsListResult{}, newValidationError("cursor", "invalid cursor")
		}
		page, err = store.IssueRegister(ctx, s.store.DB(), proj.ID, storeFilters, limit, severityRank, priorityRank, position, createdAt, id)
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
