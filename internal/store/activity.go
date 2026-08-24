package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
)

// ActivityEventRow is the internal (store-only) view of one audit_events
// row joined with its entity's kind and, when set, the comment it
// describes — the shape a project's activity feed (§5.10) reads.
// Distinct from AuditEvent (audit.go): that type serves a single
// entity's own lifecycle trail (oldest-first, unpaginated); this one
// serves a project-wide, filterable, paginated feed that also needs to
// know each row's entity kind (to resolve a public reference) and, for
// comment-shaped events, the comment's live body (§5.10: "combines
// comments with selected audit events").
type ActivityEventRow struct {
	ID            int64
	EntityID      int64
	EntityKind    domain.EntityKind
	ActorID       int64
	EventType     string
	CommentID     *int64
	CommentBody   *string
	CorrelationID string
	Changes       string
	CreatedAt     string
}

// ActivityFilters narrows ListActivityPage's project-scoped query.
// ActorID/EntityKind are AND-composed (docs/contracts/list-filters.md's
// established convention) — resolved or validated by internal/service
// before reaching here; this layer does no parsing. EventTypes is
// always populated by internal/service (either the caller's one
// requested type, or the full activityEventTypes allowlist) and applied
// as an `IN (...)` filter in the query itself, not as a post-fetch
// filter — a post-fetch filter would silently return fewer than `limit`
// rows once a non-allowlisted event type exists, since the page
// boundary would already be fixed before the filter ran.
type ActivityFilters struct {
	ActorID    int64             // 0 = no filter
	EntityKind domain.EntityKind // "" = no filter
	EventTypes []string          // must be non-empty; internal/service always supplies at least the allowlist
}

// ActivityPage is a cursor-paginated page of activity events.
type ActivityPage struct {
	Events     []ActivityEventRow
	NextCursor string
}

// ListActivityPage returns projectEntityID's combined comment+audit
// activity (§5.10), newest first — the natural read order for a feed,
// unlike the oldest-first convention ListAuditEvents/
// ListDecisionsForProjectPage use for a single record's own history.
// Cursor-paginated by (created_at, id) descending: beforeCreatedAt/
// beforeID name the previous page's last (oldest) row, and this page
// continues strictly before that position.
//
// A row belongs to the project when its entity's project_id matches, or
// — for project-level events (project_created, project_updated) —
// when the entity *is* the project itself: a project's own entities row
// has project_id NULL (0001_initial.sql), never pointing at itself.
//
// Uses GetTicketRefByEntityIDAnyDeletion/GetFeatureRefByEntityIDAnyDeletion/
// GetDecisionRefByEntityIDAnyDeletion (called by internal/service, not
// here) rather than the deleted_at-filtered variants, because a
// ticket_deleted event describes a now-soft-deleted entity and product
// spec §5.12 requires audit history to stay visible regardless of
// ordinary application operations on the entity it describes.
func ListActivityPage(ctx context.Context, q Querier, projectEntityID int64, filters ActivityFilters, limit int, beforeCreatedAt string, beforeID int64) (ActivityPage, error) {
	query := `
		SELECT ae.id, ae.entity_id, e.kind, ae.actor_id, ae.event_type, ae.comment_id, c.body,
		       ae.correlation_id, ae.changes, ae.created_at
		FROM audit_events ae
		JOIN entities e ON e.id = ae.entity_id
		LEFT JOIN comments c ON c.id = ae.comment_id
		WHERE (e.project_id = ? OR e.id = ?)`
	args := []any{projectEntityID, projectEntityID}

	if filters.ActorID != 0 {
		query += ` AND ae.actor_id = ?`
		args = append(args, filters.ActorID)
	}
	if filters.EntityKind != "" {
		query += ` AND e.kind = ?`
		args = append(args, string(filters.EntityKind))
	}
	if len(filters.EventTypes) > 0 {
		placeholders := strings.Repeat("?,", len(filters.EventTypes))
		placeholders = placeholders[:len(placeholders)-1]
		query += ` AND ae.event_type IN (` + placeholders + `)`
		for _, t := range filters.EventTypes {
			args = append(args, t)
		}
	}
	if beforeCreatedAt != "" {
		query += ` AND (ae.created_at, ae.id) < (?, ?)`
		args = append(args, beforeCreatedAt, beforeID)
	}
	query += ` ORDER BY ae.created_at DESC, ae.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return ActivityPage{}, fmt.Errorf("list activity for project %d: %w", projectEntityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ActivityEventRow
	for rows.Next() {
		var (
			e           ActivityEventRow
			kind        string
			commentID   sql.NullInt64
			commentBody sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.EntityID, &kind, &e.ActorID, &e.EventType, &commentID, &commentBody,
			&e.CorrelationID, &e.Changes, &e.CreatedAt); err != nil {
			return ActivityPage{}, fmt.Errorf("scan activity event: %w", err)
		}
		e.EntityKind = domain.EntityKind(kind)
		if commentID.Valid {
			id := commentID.Int64
			e.CommentID = &id
		}
		if commentBody.Valid {
			body := commentBody.String
			e.CommentBody = &body
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return ActivityPage{}, err
	}

	page := ActivityPage{Events: out}
	if len(out) > limit {
		page.Events = out[:limit]
		last := page.Events[limit-1]
		page.NextCursor = EncodeCreatedAtIDCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}
