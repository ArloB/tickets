package store

import (
	"context"
	"fmt"
)

// ExternalLinkRow is one named external link row (product spec
// §5.11's "named external links" half; migration
// 0006_external_links.sql).
type ExternalLinkRow struct {
	ID        int64
	EntityID  int64
	Title     string
	URL       string
	CreatedAt string
	CreatedBy int64
}

// InsertExternalLink writes one external_links row and returns its
// new id.
func InsertExternalLink(ctx context.Context, q Querier, entityID int64, title, url string, createdBy int64, now string) (int64, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO external_links(entity_id, title, url, created_at, created_by) VALUES (?, ?, ?, ?, ?)`,
		entityID, title, url, now, createdBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert external link: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("external link last insert id: %w", err)
	}
	return id, nil
}

// DeleteExternalLink removes one link, scoped to entityID as well as
// id so a caller can never delete a link belonging to a different
// entity by guessing/reusing another entity's link id — reporting
// whether a row was actually deleted, the same convention
// DeleteAssociation uses.
func DeleteExternalLink(ctx context.Context, q Querier, entityID, linkID int64) (bool, error) {
	res, err := q.ExecContext(ctx,
		`DELETE FROM external_links WHERE id = ? AND entity_id = ?`,
		linkID, entityID,
	)
	if err != nil {
		return false, fmt.Errorf("delete external link: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return affected > 0, nil
}

// ListExternalLinks returns entityID's links, oldest first. Unlike
// ListAssociatedEntityIDs, there is no "other side that might be
// soft-deleted" to filter — a link has exactly one owning entity, and
// a caller only reaches this after already resolving that entity to a
// live row.
func ListExternalLinks(ctx context.Context, q Querier, entityID int64) ([]ExternalLinkRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, entity_id, title, url, created_at, created_by FROM external_links WHERE entity_id = ? ORDER BY id ASC`,
		entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list external links for entity %d: %w", entityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ExternalLinkRow
	for rows.Next() {
		var row ExternalLinkRow
		if err := rows.Scan(&row.ID, &row.EntityID, &row.Title, &row.URL, &row.CreatedAt, &row.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan external link: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
