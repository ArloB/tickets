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
