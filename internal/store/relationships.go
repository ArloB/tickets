package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
)

// RelationshipExists reports whether the exact canonical (sourceID,
// targetID, type) row is already stored. internal/service checks this
// before inserting, matching the existing already-exists convention
// (project.go's key check) rather than parsing a UNIQUE-constraint
// error out of the driver.
func RelationshipExists(ctx context.Context, q Querier, sourceID, targetID int64, relType domain.RelationshipType) (bool, error) {
	var exists int
	err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM ticket_relationships WHERE source_id = ? AND target_id = ? AND type = ?)`,
		sourceID, targetID, string(relType),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check relationship exists: %w", err)
	}
	return exists == 1, nil
}

// RelationshipWouldCycle reports whether inserting the edge
// newSourceID -> newTargetID (of relType) would close a cycle in
// relType's existing graph — i.e. whether newTargetID can already
// reach newSourceID by following existing relType edges. Scoped by
// internal/service to RelationshipBlocks and RelationshipParentOf
// (ADR 0014, product spec §5.7); other types have no cycle concept.
//
// The caller must pass canonical (post-CanonicalRelationship) ids and
// type — the recursive walk only sees rows as actually stored, so a
// cycle expressed by mixing e.g. parent_of and child_of input is only
// caught if both were canonicalized to the same stored type first.
func RelationshipWouldCycle(ctx context.Context, q Querier, relType domain.RelationshipType, newSourceID, newTargetID int64) (bool, error) {
	var exists int
	err := q.QueryRowContext(ctx, `
		WITH RECURSIVE reach(id) AS (
			SELECT target_id FROM ticket_relationships WHERE source_id = ? AND type = ?
			UNION
			SELECT tr.target_id FROM ticket_relationships tr JOIN reach r ON tr.source_id = r.id WHERE tr.type = ?
		)
		SELECT EXISTS(SELECT 1 FROM reach WHERE id = ?)`,
		newTargetID, string(relType), string(relType), newSourceID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check relationship cycle: %w", err)
	}
	return exists == 1, nil
}

// InsertRelationship writes one canonical ticket_relationships row.
// Callers check RelationshipExists (and, for blocks/parent_of,
// RelationshipWouldCycle) first — this function does not re-check
// either, matching the store package's convention of pure typed
// statements with no business-rule branching.
func InsertRelationship(ctx context.Context, q Querier, sourceID, targetID int64, relType domain.RelationshipType, createdBy int64, now string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO ticket_relationships(source_id, target_id, type, created_at, created_by) VALUES (?, ?, ?, ?, ?)`,
		sourceID, targetID, string(relType), now, createdBy,
	)
	if err != nil {
		return fmt.Errorf("insert relationship: %w", err)
	}
	return nil
}

// DeleteRelationship removes one canonical row, reporting whether a
// row was actually deleted (false means the edge didn't exist).
func DeleteRelationship(ctx context.Context, q Querier, sourceID, targetID int64, relType domain.RelationshipType) (bool, error) {
	res, err := q.ExecContext(ctx,
		`DELETE FROM ticket_relationships WHERE source_id = ? AND target_id = ? AND type = ?`,
		sourceID, targetID, string(relType),
	)
	if err != nil {
		return false, fmt.Errorf("delete relationship: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return affected > 0, nil
}

// RelationshipEdge is one relationship as seen from a specific
// entity's perspective — Type is already resolved to what that
// entity should call it (the stored type if the entity is the
// source, or its Inverse() if the entity is the target).
type RelationshipEdge struct {
	Type    domain.RelationshipType
	OtherID int64
}

// ListRelationshipsForEntity returns every relationship touching
// entityID, from entityID's perspective. An edge where entityID is
// the stored target is only included if its type has a defined
// Inverse() — RelationshipDuplicateOf does not (domain/enums.go), so
// per ADR 0014 it is visible only from the end it was stored against,
// not surfaced here as a synthetic "duplicated_by".
func ListRelationshipsForEntity(ctx context.Context, q Querier, entityID int64) ([]RelationshipEdge, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT type, target_id, 0 AS as_target FROM ticket_relationships WHERE source_id = ?
		UNION ALL
		SELECT type, source_id, 1 AS as_target FROM ticket_relationships WHERE target_id = ?`,
		entityID, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list relationships for entity %d: %w", entityID, err)
	}
	defer func() { _ = rows.Close() }()

	var edges []RelationshipEdge
	for rows.Next() {
		var (
			relType  string
			otherID  int64
			asTarget int
		)
		if err := rows.Scan(&relType, &otherID, &asTarget); err != nil {
			return nil, fmt.Errorf("scan relationship edge: %w", err)
		}
		typ := domain.RelationshipType(relType)
		if asTarget == 1 {
			inv, ok := typ.Inverse()
			if !ok {
				continue
			}
			typ = inv
		}
		edges = append(edges, RelationshipEdge{Type: typ, OtherID: otherID})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return edges, nil
}

// GetTicketRefByEntityID resolves a ticket's public reference from its
// internal entity id, or ErrNotFound if it's missing/deleted/not a
// ticket. Relationships store bare entity ids (ADR 0002); this is the
// reverse of GetTicketByRef, used to turn a relationship edge's
// OtherID back into a wire-safe reference.
func GetTicketRefByEntityID(ctx context.Context, q Querier, entityID int64) (domain.Reference, error) {
	var (
		projectKey string
		seq        int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.key, t.seq
		 FROM tickets t
		 JOIN entities e ON e.id = t.id
		 JOIN projects p ON p.id = t.project_id
		 WHERE t.id = ? AND e.deleted_at IS NULL`,
		entityID,
	).Scan(&projectKey, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Reference{}, ErrNotFound
	}
	if err != nil {
		return domain.Reference{}, fmt.Errorf("get ticket ref for entity %d: %w", entityID, err)
	}
	return domain.Reference{ProjectKey: projectKey, Kind: domain.KindTicket, Seq: seq}, nil
}
