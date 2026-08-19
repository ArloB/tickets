package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
)

// GetActorIDByRef resolves an actor's (kind, name) — the wire form
// domain.ActorRef.String() renders — to its internal surrogate id, or
// ErrNotFound. internal/service.withTx calls this once per mutation to
// resolve the caller-supplied actor before writing anything, so every
// write inside the transaction can stamp the same actor id (ADR 0012).
//
// Only 'system' and 'local' exist as of Phase 1 (migration
// 0002_core_domain.sql's seed rows) — there is no actor creation
// surface yet, since real per-agent/per-human actors wait for ADR
// 0004's authentication in Phase 2.
func GetActorIDByRef(ctx context.Context, q Querier, kind domain.ActorKind, name string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx,
		`SELECT id FROM actors WHERE kind = ? AND name = ? AND deleted_at IS NULL`,
		string(kind), name,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("get actor %s:%s: %w", kind, name, err)
	}
	return id, nil
}

// GetActorRefByID resolves an actor's internal surrogate id back to
// its wire-safe (kind, name) ref — GetActorIDByRef's inverse, used
// when a row (e.g. tickets.assignee_id) stores the id and a caller
// needs to render it on the wire.
func GetActorRefByID(ctx context.Context, q Querier, id int64) (domain.ActorRef, error) {
	var kind, name string
	err := q.QueryRowContext(ctx,
		`SELECT kind, name FROM actors WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(&kind, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ActorRef{}, ErrNotFound
	}
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("get actor by id %d: %w", id, err)
	}
	return domain.ActorRef{Kind: domain.ActorKind(kind), Name: name}, nil
}
