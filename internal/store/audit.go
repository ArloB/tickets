package store

import (
	"context"
	"fmt"
)

// InsertAuditEvent writes one append-only audit_events row (product
// spec §5.12). commentID is nil for every event except a
// comment-add/edit/delete, which sets it so the activity feed can join
// comments and other audit events into one per-entity stream (§5.10).
// changes is a JSON fragment — internal/service decides its shape per
// event type; this function does not interpret it. Must be called
// inside the same transaction as the mutation it describes, using that
// transaction's shared now (store.Now / the tx helper's now
// parameter), not a fresh timestamp.
func InsertAuditEvent(ctx context.Context, q Querier, entityID, actorID int64, eventType, correlationID string, commentID *int64, changes string, now string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO audit_events(entity_id, actor_id, event_type, comment_id, correlation_id, changes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entityID, actorID, eventType, commentID, correlationID, changes, now,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// AuditEvent is the internal (store-only) view of one audit_events row.
type AuditEvent struct {
	ID            int64
	EntityID      int64
	ActorID       int64
	EventType     string
	CommentID     *int64
	CorrelationID string
	Changes       string
	CreatedAt     string
}

// ListAuditEvents returns entityID's audit trail, oldest first — the
// order a lifecycle test asserts an exact event sequence against.
// Unpaginated for Phase 1: no caller needs pagination yet (a Phase 4/5
// activity-feed UI is the first one that will), and product spec §5.12
// doesn't bound trail length.
func ListAuditEvents(ctx context.Context, q Querier, entityID int64) ([]AuditEvent, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, entity_id, actor_id, event_type, comment_id, correlation_id, changes, created_at
		 FROM audit_events WHERE entity_id = ? ORDER BY created_at ASC, id ASC`,
		entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var commentID *int64
		if err := rows.Scan(&e.ID, &e.EntityID, &e.ActorID, &e.EventType, &commentID, &e.CorrelationID, &e.Changes, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		e.CommentID = commentID
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
