package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TicketFilters holds optional, AND-composed narrowing predicates for
// PriorityQueue and IssueRegister (docs/contracts/list-filters.md).
// The zero value applies no filtering. Every non-empty/non-zero field
// adds one more parameterized "AND column = ?" clause — filters
// compose with AND only, never OR, matching how ?view= already
// behaves as a single all-or-nothing selector rather than a
// composable one. Enum-shaped fields (Status/Type/Severity/Priority)
// carry the raw wire value and are validated by the caller
// (internal/service) before reaching here — this layer only ever
// builds SQL from them, never validates.
type TicketFilters struct {
	Status       string // domain.WorkflowStatus wire value; "" = any
	Type         string // domain.TicketType wire value; "" = any
	Severity     string // domain.Severity wire value; "" = any
	Priority     string // domain.Priority wire value; "" = any
	FeatureID    int64  // internal feature entity id; 0 = any
	AssigneeID   int64  // internal actor id; 0 = any
	CreatorID    int64  // internal actor id; 0 = any
	UpdatedSince string // TimeLayout-formatted, UTC; "" = any
}

// clauseAndArgs renders f as a SQL fragment beginning with " AND ..."
// (empty string if f is the zero value) plus the positional args it
// references, in the same left-to-right order they appear in the
// fragment — the two must be spliced into a query and its arg list
// together, at the same position, or the placeholders and values will
// silently misalign.
func (f TicketFilters) clauseAndArgs() (string, []any) {
	var b strings.Builder
	var args []any
	if f.Status != "" {
		b.WriteString(" AND t.status = ?")
		args = append(args, f.Status)
	}
	if f.Type != "" {
		b.WriteString(" AND t.type = ?")
		args = append(args, f.Type)
	}
	if f.Severity != "" {
		b.WriteString(" AND t.severity = ?")
		args = append(args, f.Severity)
	}
	if f.Priority != "" {
		b.WriteString(" AND t.priority = ?")
		args = append(args, f.Priority)
	}
	if f.FeatureID != 0 {
		b.WriteString(" AND t.feature_id = ?")
		args = append(args, f.FeatureID)
	}
	if f.AssigneeID != 0 {
		b.WriteString(" AND t.assignee_id = ?")
		args = append(args, f.AssigneeID)
	}
	if f.CreatorID != 0 {
		b.WriteString(" AND e.created_by = ?")
		args = append(args, f.CreatorID)
	}
	if f.UpdatedSince != "" {
		b.WriteString(" AND e.updated_at >= ?")
		args = append(args, f.UpdatedSince)
	}
	return b.String(), args
}

// TicketsPage is a cursor-paginated page of tickets, returned by
// PriorityQueue and IssueRegister.
type TicketsPage struct {
	Tickets    []TicketRow
	NextCursor string
}

// scanTicketRowsPage runs the shared "fetch limit+1, keep the first
// limit, build a cursor from the last kept row" pattern the ticket
// list queries share with store.ListProjects. cursorFor receives only
// the rows that survive truncation (never the limit+1'th overflow
// row), matching ListProjects's existing guarantee that the cursor
// never names a row the client hasn't actually seen.
func scanTicketRowsPage(rowsIter interface {
	Next() bool
	Err() error
	Scan(dest ...any) error
}, limit int, cursorFor func(TicketRow) string) (TicketsPage, error) {
	var rows []TicketRow
	for rowsIter.Next() {
		row, err := scanTicketRow(rowsIter.Scan)
		if err != nil {
			return TicketsPage{}, fmt.Errorf("scan ticket: %w", err)
		}
		rows = append(rows, row)
	}
	if err := rowsIter.Err(); err != nil {
		return TicketsPage{}, err
	}

	page := TicketsPage{Tickets: rows}
	if len(rows) > limit {
		page.Tickets = rows[:limit]
		page.NextCursor = cursorFor(page.Tickets[limit-1])
	}
	return page, nil
}

// PriorityQueue returns one project's tickets ordered by priority,
// then position, then creation time (product spec §5.6), cursor-
// paginated by (priority_rank, position, created_at, id) — the
// ordering priority_rank exists to make correct (migration
// 0002_core_domain.sql: priority is TEXT and sorts alphabetically to
// critical, high, low, medium, which priority_rank fixes).
//
// Scoped to a single project, not global across all of them: §5.6
// doesn't say either way, but idx_tickets_priority_queue is built as
// (project_id, priority_rank, position), and every other list query in
// this codebase besides ListProjects (which has no project to scope
// to) is project-scoped. A global cross-project view would need a
// different index and has no caller until a Phase 4 UI view asks for
// one.
//
// filters narrows the result further (docs/contracts/list-filters.md);
// pass the zero value for no filtering. Filters do not change the
// cursor shape or ordering — idx_tickets_priority_queue only covers
// (project_id, priority_rank, position), so a selective filter (e.g.
// assignee) still scans in priority order and discards non-matching
// rows rather than seeking directly; see the filter benchmark in
// bench_test.go before assuming this holds at the §11 reference scale.
func PriorityQueue(ctx context.Context, q Querier, projectID int64, filters TicketFilters, limit int,
	afterRank, afterPosition int64, afterCreatedAt string, afterID int64) (TicketsPage, error) {
	filterClause, filterArgs := filters.clauseAndArgs()
	query := `SELECT` + ticketSelectColumns + `
		 FROM tickets t
		 JOIN entities e ON e.id = t.id
		 JOIN projects p ON p.id = t.project_id
		 JOIN features f ON f.id = t.feature_id
		 LEFT JOIN actors a ON a.id = t.assignee_id
		 LEFT JOIN actors ca ON ca.id = e.created_by
		 WHERE t.project_id = ? AND e.deleted_at IS NULL` + filterClause + `
		   AND (t.priority_rank, t.position, e.created_at, e.id) > (?, ?, ?, ?)
		 ORDER BY t.priority_rank ASC, t.position ASC, e.created_at ASC, e.id ASC
		 LIMIT ?`
	args := append([]any{projectID}, filterArgs...)
	args = append(args, afterRank, afterPosition, afterCreatedAt, afterID, limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return TicketsPage{}, fmt.Errorf("priority queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanTicketRowsPage(rows, limit, func(last TicketRow) string {
		return EncodeCursor(
			strconv.FormatInt(last.PriorityRank, 10),
			strconv.FormatInt(last.Position, 10),
			formatTimeForCursor(last.Entity.CreatedAt),
			strconv.FormatInt(last.ID, 10),
		)
	})
}

// IssueRegister returns one project's bug/security tickets ordered by
// severity, then priority, then position, then age (product spec
// §5.5), cursor-paginated by (severity_rank, priority_rank, position,
// created_at, id). Project-scoped for the same reasons PriorityQueue
// is; idx_tickets_issue_register is built to match.
//
// filters narrows further, exactly as PriorityQueue's filters
// parameter does. A filters.Type value of "bug" or "security" is
// legitimate here even though the query already restricts to that
// pair — it narrows to just one of the two, which the fixed
// `t.type IN (...)` predicate alone cannot express.
func IssueRegister(ctx context.Context, q Querier, projectID int64, filters TicketFilters, limit int,
	afterSeverityRank, afterPriorityRank, afterPosition int64, afterCreatedAt string, afterID int64) (TicketsPage, error) {
	filterClause, filterArgs := filters.clauseAndArgs()
	query := `SELECT` + ticketSelectColumns + `
		 FROM tickets t
		 JOIN entities e ON e.id = t.id
		 JOIN projects p ON p.id = t.project_id
		 JOIN features f ON f.id = t.feature_id
		 LEFT JOIN actors a ON a.id = t.assignee_id
		 LEFT JOIN actors ca ON ca.id = e.created_by
		 WHERE t.project_id = ? AND e.deleted_at IS NULL
		   AND t.type IN ('bug', 'security')` + filterClause + `
		   AND (t.severity_rank, t.priority_rank, t.position, e.created_at, e.id) > (?, ?, ?, ?, ?)
		 ORDER BY t.severity_rank ASC, t.priority_rank ASC, t.position ASC, e.created_at ASC, e.id ASC
		 LIMIT ?`
	args := append([]any{projectID}, filterArgs...)
	args = append(args, afterSeverityRank, afterPriorityRank, afterPosition, afterCreatedAt, afterID, limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return TicketsPage{}, fmt.Errorf("issue register: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanTicketRowsPage(rows, limit, func(last TicketRow) string {
		return EncodeCursor(
			strconv.FormatInt(last.SeverityRank, 10),
			strconv.FormatInt(last.PriorityRank, 10),
			strconv.FormatInt(last.Position, 10),
			formatTimeForCursor(last.Entity.CreatedAt),
			strconv.FormatInt(last.ID, 10),
		)
	})
}

// formatTimeForCursor re-renders a parsed CreatedAt back into
// TimeLayout's fixed-width form for cursor encoding. TicketRow.Entity
// carries a parsed time.Time (the wire/domain shape), not the raw
// TEXT column, so the cursor — which must preserve TimeLayout's
// lexicographic-comparison property — re-formats it rather than
// comparing time.Time values directly.
func formatTimeForCursor(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}
