package store

import (
	"context"
	"fmt"
)

// DeleteActorMentionsFromSource clears every actor_mentions row for
// one (sourceEntityID, sourceCommentID) body — the actor-mention
// counterpart of DeleteMentionsFromSource, called by the same
// rescanMentions delete-and-reinsert pass (ADR 0019).
func DeleteActorMentionsFromSource(ctx context.Context, q Querier, sourceEntityID, sourceCommentID int64) error {
	if _, err := q.ExecContext(ctx,
		`DELETE FROM actor_mentions WHERE source_entity_id = ? AND source_comment_id = ?`,
		sourceEntityID, sourceCommentID,
	); err != nil {
		return fmt.Errorf("delete actor mentions: %w", err)
	}
	return nil
}

// InsertActorMention records one actor_mentions edge, ignoring a
// duplicate — a body can technically mention the same actor twice in
// one scan pass only if ScanActorMentions' own dedup were bypassed,
// which it isn't, but this stays defensive at the SQL layer the same
// way a unique index generally would.
func InsertActorMention(ctx context.Context, q Querier, sourceEntityID, sourceCommentID, actorID int64, now string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT OR IGNORE INTO actor_mentions(source_entity_id, source_comment_id, actor_id, created_at) VALUES (?, ?, ?, ?)`,
		sourceEntityID, sourceCommentID, actorID, now,
	); err != nil {
		return fmt.Errorf("insert actor mention: %w", err)
	}
	return nil
}
