package store

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

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
func PriorityQueue(ctx context.Context, q Querier, projectID int64, limit int,
	afterRank, afterPosition int64, afterCreatedAt string, afterID int64) (TicketsPage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT`+ticketSelectColumns+`
		 FROM tickets t
		 JOIN entities e ON e.id = t.id
		 JOIN projects p ON p.id = t.project_id
		 JOIN features f ON f.id = t.feature_id
		 WHERE t.project_id = ? AND e.deleted_at IS NULL
		   AND (t.priority_rank, t.position, e.created_at, e.id) > (?, ?, ?, ?)
		 ORDER BY t.priority_rank ASC, t.position ASC, e.created_at ASC, e.id ASC
		 LIMIT ?`,
		projectID, afterRank, afterPosition, afterCreatedAt, afterID, limit+1,
	)
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
func IssueRegister(ctx context.Context, q Querier, projectID int64, limit int,
	afterSeverityRank, afterPriorityRank, afterPosition int64, afterCreatedAt string, afterID int64) (TicketsPage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT`+ticketSelectColumns+`
		 FROM tickets t
		 JOIN entities e ON e.id = t.id
		 JOIN projects p ON p.id = t.project_id
		 JOIN features f ON f.id = t.feature_id
		 WHERE t.project_id = ? AND e.deleted_at IS NULL
		   AND t.type IN ('bug', 'security')
		   AND (t.severity_rank, t.priority_rank, t.position, e.created_at, e.id) > (?, ?, ?, ?, ?)
		 ORDER BY t.severity_rank ASC, t.priority_rank ASC, t.position ASC, e.created_at ASC, e.id ASC
		 LIMIT ?`,
		projectID, afterSeverityRank, afterPriorityRank, afterPosition, afterCreatedAt, afterID, limit+1,
	)
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
