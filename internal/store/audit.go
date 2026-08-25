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

// InsertActorAuditEvent writes one append-only audit_events row whose
// subject is an actor, not an entity — CreateAgent/CreateAgentToken/
// RevokeAgentToken (Phase 6 Step 1, ADR 0012's amendment) have no
// entities.id to attach to (actors sit outside that registry, ADR
// 0002), so this sets target_actor_id instead and leaves entity_id
// NULL, per 0013_actor_audit_events.sql's CHECK constraint. changes
// must never carry a raw token value (product spec §10) — callers
// pass only the token id/description, never the secret.
func InsertActorAuditEvent(ctx context.Context, q Querier, targetActorID, actorID int64, eventType, correlationID, changes, now string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO audit_events(target_actor_id, actor_id, event_type, correlation_id, changes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		targetActorID, actorID, eventType, correlationID, changes, now,
	)
	if err != nil {
		return fmt.Errorf("insert actor audit event: %w", err)
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

// ActorAuditEvent is the internal (store-only) view of one
// target_actor_id-scoped audit_events row — the admin agent/token
// view's data source (Phase 6 Step 1), analogous to AuditEvent but for
// an actor's own trail instead of an entity's.
type ActorAuditEvent struct {
	ID            int64
	TargetActorID int64
	ActorID       int64
	EventType     string
	CorrelationID string
	Changes       string
	CreatedAt     string
}

// ListActorAuditEvents returns targetActorID's audit trail, oldest
// first — the same ordering convention ListAuditEvents uses for an
// entity's own trail.
func ListActorAuditEvents(ctx context.Context, q Querier, targetActorID int64) ([]ActorAuditEvent, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, target_actor_id, actor_id, event_type, correlation_id, changes, created_at
		 FROM audit_events WHERE target_actor_id = ? ORDER BY created_at ASC, id ASC`,
		targetActorID,
	)
	if err != nil {
		return nil, fmt.Errorf("list actor audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []ActorAuditEvent
	for rows.Next() {
		var e ActorAuditEvent
		if err := rows.Scan(&e.ID, &e.TargetActorID, &e.ActorID, &e.EventType, &e.CorrelationID, &e.Changes, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan actor audit event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
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
