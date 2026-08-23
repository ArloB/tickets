package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
)

// DeleteMentionsFromSource removes every derived_mentions row for a
// given (source entity, source comment) pair, ahead of reinserting
// the current scan result — §5.2's delete-and-reinsert rule.
// sourceCommentID is 0 for an entity's own body (schema's sentinel,
// not a real comments.id).
func DeleteMentionsFromSource(ctx context.Context, q Querier, sourceEntityID, sourceCommentID int64) error {
	_, err := q.ExecContext(ctx,
		`DELETE FROM derived_mentions WHERE source_entity_id = ? AND source_comment_id = ?`,
		sourceEntityID, sourceCommentID,
	)
	if err != nil {
		return fmt.Errorf("delete mentions from source: %w", err)
	}
	return nil
}

// InsertDerivedMention writes one mention edge.
func InsertDerivedMention(ctx context.Context, q Querier, sourceEntityID, sourceCommentID, targetEntityID int64, now string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO derived_mentions(source_entity_id, source_comment_id, target_entity_id, created_at) VALUES (?, ?, ?, ?)`,
		sourceEntityID, sourceCommentID, targetEntityID, now,
	)
	if err != nil {
		return fmt.Errorf("insert derived mention: %w", err)
	}
	return nil
}

// ListMentionTargetsFromSource returns the entity ids a given
// (source entity, source comment) body currently mentions, filtered
// to targets that are not soft-deleted (gate 8: a mention of a
// deleted entity must vanish from every read path, same reasoning as
// ListRelationshipsForEntity's far-endpoint filter — and it comes
// back for free on restore).
func ListMentionTargetsFromSource(ctx context.Context, q Querier, sourceEntityID, sourceCommentID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT dm.target_entity_id
		 FROM derived_mentions dm
		 JOIN entities e ON e.id = dm.target_entity_id
		 WHERE dm.source_entity_id = ? AND dm.source_comment_id = ? AND e.deleted_at IS NULL
		 ORDER BY dm.target_entity_id ASC`,
		sourceEntityID, sourceCommentID,
	)
	if err != nil {
		return nil, fmt.Errorf("list mention targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan mention target: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// MentionSourceRow is one reverse-mention edge: an entity (and,
// optionally, one specific comment on it) that currently mentions some
// target. SourceCommentID is 0 for "the source entity's own body", a
// real comments.id otherwise — the same sentinel derived_mentions
// itself uses, so a caller can tell "mentioned in ABC-124's
// description" from "mentioned in a comment on ABC-124" and link to
// the right place.
type MentionSourceRow struct {
	SourceEntityID  int64
	SourceCommentID int64
}

// ListMentionSourcesToTarget is ListMentionTargetsFromSource's reverse
// direction: every (source entity, source comment) that currently
// mentions targetEntityID — targetEntityID's backlinks (product spec
// §6.1: "View backlinks generated from Markdown references").
// idx_derived_mentions_target covers the WHERE clause below.
//
// Filters out two things the forward query never has to consider:
// sources whose *entity* is soft-deleted (same reasoning as
// ListMentionTargetsFromSource's target filter), and — new here,
// since only a reverse query can originate from a comment at all —
// sources whose *comment* is soft-deleted. A tombstoned comment
// (§5.10: soft-deleted with a visible tombstone) keeps its row, so a
// naive join would still surface its mention as a live backlink; the
// tombstone is visible in the comment's own history, not as a
// still-active reference to whatever it once mentioned.
func ListMentionSourcesToTarget(ctx context.Context, q Querier, targetEntityID int64) ([]MentionSourceRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT dm.source_entity_id, dm.source_comment_id
		 FROM derived_mentions dm
		 JOIN entities e ON e.id = dm.source_entity_id
		 LEFT JOIN comments c ON c.id = dm.source_comment_id
		 WHERE dm.target_entity_id = ? AND e.deleted_at IS NULL
		   AND (dm.source_comment_id = 0 OR c.deleted_at IS NULL)
		 ORDER BY dm.source_entity_id ASC, dm.source_comment_id ASC`,
		targetEntityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list mention sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []MentionSourceRow
	for rows.Next() {
		var row MentionSourceRow
		if err := rows.Scan(&row.SourceEntityID, &row.SourceCommentID); err != nil {
			return nil, fmt.Errorf("scan mention source: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetEntityKindByID resolves an entity's kind from its internal
// surrogate id, or ErrNotFound if it's missing or soft-deleted. Used
// to dispatch a mention target (or any other bare entity id) to the
// right Get*ByEntityID lookup without the caller needing to already
// know the kind.
func GetEntityKindByID(ctx context.Context, q Querier, entityID int64) (domain.EntityKind, error) {
	var kind string
	err := q.QueryRowContext(ctx,
		`SELECT kind FROM entities WHERE id = ? AND deleted_at IS NULL`,
		entityID,
	).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get entity kind for id %d: %w", entityID, err)
	}
	return domain.EntityKind(kind), nil
}
