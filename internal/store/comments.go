package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
)

// CommentRow is the internal (store-only) view of a comment.
// EntityID is internal-only, the same boundary TicketRow/FeatureRow
// already draw. DeletedAt is included even though Get/List don't
// filter on it — a comment's tombstone stays visible (§5.10), unlike
// a soft-deleted principal entity.
type CommentRow struct {
	Entity   domain.Comment
	EntityID int64
}

// InsertComment writes a new comment row (version 1, no
// comment_versions row yet — comments.body is always the current
// text; comment_versions only holds superseded ones).
func InsertComment(ctx context.Context, q Querier, entityID, authorID int64, body, now string) (commentID int64, err error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO comments(entity_id, author_id, body, version, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)`,
		entityID, authorID, body, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert comment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

const commentSelectColumns = `
	c.id, c.entity_id, a.kind, a.name, c.body, c.version, c.created_at, c.updated_at, c.deleted_at`

func scanCommentRow(scan func(dest ...any) error) (CommentRow, error) {
	var (
		row                    CommentRow
		authorKind, authorName string
		createdAt, updatedAt   string
		deletedAt              sql.NullString
	)
	err := scan(&row.Entity.ID, &row.EntityID, &authorKind, &authorName,
		&row.Entity.Body, &row.Entity.Version, &createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return CommentRow{}, err
	}
	row.Entity.Author = domain.ActorRef{Kind: domain.ActorKind(authorKind), Name: authorName}
	if row.Entity.CreatedAt, err = parseTime(createdAt); err != nil {
		return CommentRow{}, fmt.Errorf("parse comment created_at: %w", err)
	}
	if row.Entity.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return CommentRow{}, fmt.Errorf("parse comment updated_at: %w", err)
	}
	if deletedAt.Valid {
		t, err := parseTime(deletedAt.String)
		if err != nil {
			return CommentRow{}, fmt.Errorf("parse comment deleted_at: %w", err)
		}
		row.Entity.DeletedAt = &t
	}
	return row, nil
}

// GetComment looks up a comment by its own id, or ErrNotFound.
// Deliberately does not filter deleted_at: a soft-deleted comment's
// tombstone stays visible (§5.10) — internal/service decides whether
// deleted_at != nil should block a given operation (e.g. editing),
// not this lookup.
func GetComment(ctx context.Context, q Querier, commentID int64) (CommentRow, error) {
	query := `SELECT` + commentSelectColumns + `
		FROM comments c JOIN actors a ON a.id = c.author_id
		WHERE c.id = ?`
	row, err := scanCommentRow(q.QueryRowContext(ctx, query, commentID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return CommentRow{}, ErrNotFound
	}
	if err != nil {
		return CommentRow{}, fmt.Errorf("get comment %d: %w", commentID, err)
	}
	return row, nil
}

// ListCommentsForEntity returns every comment on entityID, oldest
// first, tombstones included (§5.10).
func ListCommentsForEntity(ctx context.Context, q Querier, entityID int64) ([]CommentRow, error) {
	query := `SELECT` + commentSelectColumns + `
		FROM comments c JOIN actors a ON a.id = c.author_id
		WHERE c.entity_id = ?
		ORDER BY c.created_at ASC, c.id ASC`
	rows, err := q.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, fmt.Errorf("list comments for entity %d: %w", entityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []CommentRow
	for rows.Next() {
		row, err := scanCommentRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateCommentBody applies a conditional edit: it only takes effect
// if the comment's current version matches expectedVersion and it is
// not already soft-deleted, returning ErrVersionConflict otherwise —
// the same pattern bumpEntityVersion applies to entities, but comments
// version themselves independently (comments.version, not
// entities.version: adding or editing a comment does not bump its
// parent entity's version, a deliberate Phase 1 decision recorded in
// docs/contracts/concurrency.md's Phase 1 addendum).
func UpdateCommentBody(ctx context.Context, q Querier, commentID int64, newBody string, expectedVersion int64, now string) (newVersion int64, err error) {
	res, err := q.ExecContext(ctx,
		`UPDATE comments SET version = version + 1, body = ?, updated_at = ?
		 WHERE id = ? AND version = ? AND deleted_at IS NULL`,
		newBody, now, commentID, expectedVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("update comment body: %w", err)
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

// InsertCommentVersion archives a comment's prior body ahead of an
// edit overwriting it. editedBy is the actor performing the edit that
// supersedes this version, not necessarily who originally wrote it —
// see domain.CommentVersion's doc for why, and audit_events for the
// full authorship chain.
func InsertCommentVersion(ctx context.Context, q Querier, commentID, version int64, body string, editedBy int64, now string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO comment_versions(comment_id, version, body, edited_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		commentID, version, body, editedBy, now,
	)
	if err != nil {
		return fmt.Errorf("insert comment version: %w", err)
	}
	return nil
}

// ListCommentVersions returns a comment's archived prior bodies,
// oldest first — the live body (comments.body) is not included, since
// it's not archived until the next edit.
func ListCommentVersions(ctx context.Context, q Querier, commentID int64) ([]domain.CommentVersion, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT cv.version, cv.body, a.kind, a.name, cv.created_at
		 FROM comment_versions cv JOIN actors a ON a.id = cv.edited_by
		 WHERE cv.comment_id = ?
		 ORDER BY cv.version ASC`,
		commentID,
	)
	if err != nil {
		return nil, fmt.Errorf("list comment versions for %d: %w", commentID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.CommentVersion
	for rows.Next() {
		var (
			v                          domain.CommentVersion
			editedByKind, editedByName string
			createdAt                  string
		)
		if err := rows.Scan(&v.Version, &v.Body, &editedByKind, &editedByName, &createdAt); err != nil {
			return nil, fmt.Errorf("scan comment version: %w", err)
		}
		v.EditedBy = domain.ActorRef{Kind: domain.ActorKind(editedByKind), Name: editedByName}
		if v.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("parse comment version created_at: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SoftDeleteComment applies a conditional soft-delete: it only takes
// effect if the comment's current version matches expectedVersion and
// it is not already deleted. Unlike a principal entity's delete, this
// leaves the row and its body intact (the tombstone must stay
// visible, §5.10) — it only stamps deleted_at.
func SoftDeleteComment(ctx context.Context, q Querier, commentID int64, expectedVersion int64, now string) error {
	res, err := q.ExecContext(ctx,
		`UPDATE comments SET deleted_at = ? WHERE id = ? AND version = ? AND deleted_at IS NULL`,
		now, commentID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("soft-delete comment: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return ErrVersionConflict
	}
	return nil
}
