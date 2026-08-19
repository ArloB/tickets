package store

import (
	"context"
	"fmt"
)

// SoftDeleteEntity conditionally soft-deletes any entities-backed row
// (ADR 0013): it only takes effect if the row's current version
// matches expectedVersion and it is not already deleted, returning
// ErrVersionConflict otherwise. One generic function serves every
// principal entity kind (tickets, features, projects) — soft-deletion
// lives entirely on entities.deleted_at, never on the kind-specific
// table, so nothing here needs to know which table a given entityID
// belongs to.
func SoftDeleteEntity(ctx context.Context, q Querier, entityID, expectedVersion int64, now string) (newVersion int64, err error) {
	res, err := q.ExecContext(ctx,
		`UPDATE entities SET version = version + 1, updated_at = ?, deleted_at = ?
		 WHERE id = ? AND version = ? AND deleted_at IS NULL`,
		now, now, entityID, expectedVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("soft-delete entity: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return 0, ErrVersionConflict
	}
	return expectedVersion + 1, nil
}

// SoftDeleteEntityUnconditional soft-deletes a row without an
// expectedVersion check — used only for cascade-deleted dependents
// (e.g. a feature's tickets when DeleteFeature is called with
// Cascade), where the caller has no per-dependent version to check
// against and the transaction's write lock (ADR 0003's BEGIN
// IMMEDIATE) already guarantees nothing else touched the row since it
// was counted as a dependent moments earlier in the same transaction.
// Still guarded on deleted_at IS NULL so it can't double-decrement an
// already-deleted row.
func SoftDeleteEntityUnconditional(ctx context.Context, q Querier, entityID int64, now string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE entities SET version = version + 1, updated_at = ?, deleted_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		now, now, entityID,
	)
	if err != nil {
		return fmt.Errorf("soft-delete entity (unconditional): %w", err)
	}
	return nil
}

// RestoreEntity conditionally clears deleted_at: it only takes effect
// if the row's current version matches expectedVersion and it is
// currently deleted, returning ErrVersionConflict otherwise. A
// soft-deleted row's version keeps incrementing on restore, the same
// as any other mutation — restoring is itself a change a stale
// caller's If-Match should catch.
func RestoreEntity(ctx context.Context, q Querier, entityID, expectedVersion int64, now string) (newVersion int64, err error) {
	res, err := q.ExecContext(ctx,
		`UPDATE entities SET version = version + 1, updated_at = ?, deleted_at = NULL
		 WHERE id = ? AND version = ? AND deleted_at IS NOT NULL`,
		now, entityID, expectedVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("restore entity: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return 0, ErrVersionConflict
	}
	return expectedVersion + 1, nil
}

// ListTicketEntityIDsForFeature returns the internal entity ids of
// every non-deleted ticket currently in a feature — DeleteFeature's
// dependents count/list for the has_dependents check and the cascade
// path (ADR 0013).
func ListTicketEntityIDsForFeature(ctx context.Context, q Querier, featureEntityID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT t.id FROM tickets t JOIN entities e ON e.id = t.id
		 WHERE t.feature_id = ? AND e.deleted_at IS NULL`,
		featureEntityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list ticket ids for feature %d: %w", featureEntityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan ticket id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
