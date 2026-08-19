package store

import (
	"context"
	"database/sql"
	"fmt"
)

// GroupMember is one row's (entity id, position) within a priority
// group, ordered by position ascending.
type GroupMember struct {
	EntityID int64
	Position int64
}

// TicketGroupMaxPosition returns the highest position among a
// project's non-deleted tickets at a given priority_rank, or 0 if the
// group is empty — matching domain.TailPosition's "0 if the group is
// currently empty" contract.
func TicketGroupMaxPosition(ctx context.Context, q Querier, projectID, priorityRank int64) (int64, error) {
	var max sql.NullInt64
	err := q.QueryRowContext(ctx,
		`SELECT MAX(t.position) FROM tickets t JOIN entities e ON e.id = t.id
		 WHERE t.project_id = ? AND t.priority_rank = ? AND e.deleted_at IS NULL`,
		projectID, priorityRank,
	).Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("ticket group max position: %w", err)
	}
	if !max.Valid {
		return 0, nil
	}
	return max.Int64, nil
}

// TicketGroupMaxPositionByPriority is TicketGroupMaxPosition scoped by
// a priority string instead of an already-computed rank — the
// priority-to-rank derivation stays inside this package (rank.go's
// "one place" rule), so callers outside internal/store never need
// priorityRank directly.
func TicketGroupMaxPositionByPriority(ctx context.Context, q Querier, projectID int64, priority string) (int64, error) {
	return TicketGroupMaxPosition(ctx, q, projectID, int64(priorityRank(priority)))
}

// TicketGroupOrderedExcluding returns every non-deleted ticket in a
// project's priority_rank group except excludeEntityID, ordered by
// position ascending — the "other members" a reorder or renumber
// needs to slot excludeEntityID's ticket among.
func TicketGroupOrderedExcluding(ctx context.Context, q Querier, projectID, priorityRank, excludeEntityID int64) ([]GroupMember, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT t.id, t.position FROM tickets t JOIN entities e ON e.id = t.id
		 WHERE t.project_id = ? AND t.priority_rank = ? AND e.deleted_at IS NULL AND t.id != ?
		 ORDER BY t.position ASC`,
		projectID, priorityRank, excludeEntityID,
	)
	if err != nil {
		return nil, fmt.Errorf("ticket group ordered: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.EntityID, &m.Position); err != nil {
			return nil, fmt.Errorf("scan group member: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SetTicketPositionUnversioned rewrites a ticket's position with no
// version bump and no updated_at change — the mechanical half of a
// group renumber (ADR 0011): only the record the caller explicitly
// moved gets a versioned, audited write; its neighbors' positions
// widening back out is bookkeeping, not a change a client should have
// to catch with If-Match.
func SetTicketPositionUnversioned(ctx context.Context, q Querier, entityID, position int64) error {
	if _, err := q.ExecContext(ctx, `UPDATE tickets SET position = ? WHERE id = ?`, position, entityID); err != nil {
		return fmt.Errorf("set ticket position: %w", err)
	}
	return nil
}

// SetTicketPositionVersioned is SetTicketPositionUnversioned's
// counterpart for the one record a reorder actually targets: a
// conditional write guarded by bumpEntityVersion (ADR 0008), so a
// stale caller gets version_conflict like any other mutation.
func SetTicketPositionVersioned(ctx context.Context, q Querier, entityID, position, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE tickets SET position = ? WHERE id = ?`, position, entityID); err != nil {
		return 0, fmt.Errorf("set ticket position: %w", err)
	}
	return newVersion, nil
}

// FeatureGroupMaxPosition is TicketGroupMaxPosition's counterpart for
// features.
func FeatureGroupMaxPosition(ctx context.Context, q Querier, projectID, priorityRank int64) (int64, error) {
	var max sql.NullInt64
	err := q.QueryRowContext(ctx,
		`SELECT MAX(f.position) FROM features f JOIN entities e ON e.id = f.id
		 WHERE f.project_id = ? AND f.priority_rank = ? AND e.deleted_at IS NULL`,
		projectID, priorityRank,
	).Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("feature group max position: %w", err)
	}
	if !max.Valid {
		return 0, nil
	}
	return max.Int64, nil
}

// FeatureGroupMaxPositionByPriority mirrors
// TicketGroupMaxPositionByPriority for features.
func FeatureGroupMaxPositionByPriority(ctx context.Context, q Querier, projectID int64, priority string) (int64, error) {
	return FeatureGroupMaxPosition(ctx, q, projectID, int64(priorityRank(priority)))
}

// FeatureGroupOrderedExcluding mirrors TicketGroupOrderedExcluding for
// features.
func FeatureGroupOrderedExcluding(ctx context.Context, q Querier, projectID, priorityRank, excludeEntityID int64) ([]GroupMember, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT f.id, f.position FROM features f JOIN entities e ON e.id = f.id
		 WHERE f.project_id = ? AND f.priority_rank = ? AND e.deleted_at IS NULL AND f.id != ?
		 ORDER BY f.position ASC`,
		projectID, priorityRank, excludeEntityID,
	)
	if err != nil {
		return nil, fmt.Errorf("feature group ordered: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.EntityID, &m.Position); err != nil {
			return nil, fmt.Errorf("scan group member: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SetFeaturePositionUnversioned mirrors SetTicketPositionUnversioned
// for features.
func SetFeaturePositionUnversioned(ctx context.Context, q Querier, entityID, position int64) error {
	if _, err := q.ExecContext(ctx, `UPDATE features SET position = ? WHERE id = ?`, position, entityID); err != nil {
		return fmt.Errorf("set feature position: %w", err)
	}
	return nil
}

// SetFeaturePositionVersioned mirrors SetTicketPositionVersioned for
// features.
func SetFeaturePositionVersioned(ctx context.Context, q Querier, entityID, position, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE features SET position = ? WHERE id = ?`, position, entityID); err != nil {
		return 0, fmt.Errorf("set feature position: %w", err)
	}
	return newVersion, nil
}
