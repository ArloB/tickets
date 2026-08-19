package store

import (
	"context"
	"fmt"
)

// AssociationExists reports whether the exact canonical (sourceID,
// targetID) row is already stored — the same pre-check convention
// RelationshipExists uses, rather than parsing a UNIQUE-constraint
// error out of the driver.
func AssociationExists(ctx context.Context, q Querier, sourceID, targetID int64) (bool, error) {
	var exists int
	err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM entity_associations WHERE source_id = ? AND target_id = ?)`,
		sourceID, targetID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check association exists: %w", err)
	}
	return exists == 1, nil
}

// InsertAssociation writes one canonical entity_associations row.
func InsertAssociation(ctx context.Context, q Querier, sourceID, targetID, createdBy int64, now string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO entity_associations(source_id, target_id, created_at, created_by) VALUES (?, ?, ?, ?)`,
		sourceID, targetID, now, createdBy,
	)
	if err != nil {
		return fmt.Errorf("insert association: %w", err)
	}
	return nil
}

// DeleteAssociation removes one canonical row, reporting whether a
// row was actually deleted.
func DeleteAssociation(ctx context.Context, q Querier, sourceID, targetID int64) (bool, error) {
	res, err := q.ExecContext(ctx,
		`DELETE FROM entity_associations WHERE source_id = ? AND target_id = ?`,
		sourceID, targetID,
	)
	if err != nil {
		return false, fmt.Errorf("delete association: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return affected > 0, nil
}

// ListAssociatedEntityIDs returns the internal entity ids associated
// with entityID from either side of the (symmetric) edge, filtered to
// far endpoints that are not soft-deleted — the same read-time filter
// ListRelationshipsForEntity applies, so a deleted partner's
// association vanishes from this list and reappears on restore rather
// than needing cleanup at delete time.
func ListAssociatedEntityIDs(ctx context.Context, q Querier, entityID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT ea.target_id
		FROM entity_associations ea
		JOIN entities e ON e.id = ea.target_id
		WHERE ea.source_id = ? AND e.deleted_at IS NULL
		UNION
		SELECT ea.source_id
		FROM entity_associations ea
		JOIN entities e ON e.id = ea.source_id
		WHERE ea.target_id = ? AND e.deleted_at IS NULL`,
		entityID, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list associated entity ids for %d: %w", entityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan associated entity id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
