package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// InsertNotification records one delivered notification for actorID.
// commentID/triggeredBy may be nil (see migrations/0012_notifications.sql
// for when each is set) — never deleted or rewritten by an ordinary
// application operation afterward (ADR 0019), only ever read or its
// read_at set by MarkNotificationsRead.
func InsertNotification(ctx context.Context, q Querier, actorID int64, kind string, entityID int64, commentID, triggeredBy *int64, now string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO notifications(actor_id, kind, entity_id, comment_id, triggered_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		actorID, kind, entityID, commentID, triggeredBy, now,
	); err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

// NotificationRow is one notifications row plus the joined
// entities.kind internal/service needs to resolve entity_id to a
// public reference (mirrors internal/service/activity.go's
// activityEntityRef dispatch) — kept off the notifications table
// itself since entities.kind already carries it (ADR 0002).
type NotificationRow struct {
	ID          int64
	Kind        string
	EntityID    int64
	EntityKind  string
	CommentID   *int64
	TriggeredBy *int64
	CreatedAt   string
	ReadAt      *string
}

// NotificationsPage is ListNotificationsForActor's cursor-paginated
// result, newest first (matching the activity feed's ordering).
type NotificationsPage struct {
	Notifications []NotificationRow
	NextCursor    string
}

// ListNotificationsForActor returns actorID's own notifications,
// newest first, optionally narrowed to unread only. Cursor shape is
// the same (created_at, id) descending tuple ListActivityPage uses.
func ListNotificationsForActor(ctx context.Context, q Querier, actorID int64, unreadOnly bool, limit int, beforeCreatedAt string, beforeID int64) (NotificationsPage, error) {
	var b strings.Builder
	b.WriteString(`
		SELECT n.id, n.kind, n.entity_id, e.kind, n.comment_id, n.triggered_by, n.created_at, n.read_at
		FROM notifications n
		JOIN entities e ON e.id = n.entity_id
		WHERE n.actor_id = ?`)
	args := []any{actorID}
	if unreadOnly {
		b.WriteString(" AND n.read_at IS NULL")
	}
	if beforeCreatedAt != "" {
		b.WriteString(" AND (n.created_at, n.id) < (?, ?)")
		args = append(args, beforeCreatedAt, beforeID)
	}
	b.WriteString(" ORDER BY n.created_at DESC, n.id DESC LIMIT ?")
	args = append(args, limit+1)

	rows, err := q.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return NotificationsPage{}, fmt.Errorf("list notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NotificationRow
	for rows.Next() {
		var (
			row         NotificationRow
			commentID   sql.NullInt64
			triggeredBy sql.NullInt64
			readAt      sql.NullString
		)
		if err := rows.Scan(&row.ID, &row.Kind, &row.EntityID, &row.EntityKind, &commentID, &triggeredBy, &row.CreatedAt, &readAt); err != nil {
			return NotificationsPage{}, fmt.Errorf("scan notification: %w", err)
		}
		if commentID.Valid {
			v := commentID.Int64
			row.CommentID = &v
		}
		if triggeredBy.Valid {
			v := triggeredBy.Int64
			row.TriggeredBy = &v
		}
		if readAt.Valid {
			v := readAt.String
			row.ReadAt = &v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return NotificationsPage{}, fmt.Errorf("iterate notifications: %w", err)
	}

	nextCursor := ""
	if len(out) > limit {
		last := out[limit-1]
		nextCursor = EncodeCreatedAtIDCursor(last.CreatedAt, last.ID)
		out = out[:limit]
	}
	return NotificationsPage{Notifications: out, NextCursor: nextCursor}, nil
}

// MarkNotificationsRead sets read_at on actorID's own notifications
// named by ids (or every currently-unread one, if all is true),
// returning how many rows were actually updated. Scoped to actorID in
// the WHERE clause itself, not just by the caller only ever passing
// their own ids — marking someone else's notification read must be
// structurally impossible, not merely unexercised by any current
// caller.
func MarkNotificationsRead(ctx context.Context, q Querier, actorID int64, ids []int64, all bool, now string) (int64, error) {
	if all {
		res, err := q.ExecContext(ctx,
			`UPDATE notifications SET read_at = ? WHERE actor_id = ? AND read_at IS NULL`,
			now, actorID,
		)
		if err != nil {
			return 0, fmt.Errorf("mark all notifications read: %w", err)
		}
		return res.RowsAffected()
	}
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, actorID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	res, err := q.ExecContext(ctx,
		`UPDATE notifications SET read_at = ? WHERE actor_id = ? AND read_at IS NULL AND id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("mark notifications read: %w", err)
	}
	return res.RowsAffected()
}
