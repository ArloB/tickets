package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Subscribe records actorID as a subscriber of entityID, idempotently
// (product spec §6.4: creating or commenting on an entity subscribes
// the actor by default — a second create-or-comment by the same actor
// must not error).
func Subscribe(ctx context.Context, q Querier, entityID, actorID int64, now string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT OR IGNORE INTO subscriptions(entity_id, actor_id, created_at) VALUES (?, ?, ?)`,
		entityID, actorID, now,
	); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return nil
}

// Unsubscribe removes actorID's subscription to entityID, if any —
// unsubscribing when not subscribed is a no-op, not an error.
func Unsubscribe(ctx context.Context, q Querier, entityID, actorID int64) error {
	if _, err := q.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE entity_id = ? AND actor_id = ?`,
		entityID, actorID,
	); err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	return nil
}

// IsSubscribed reports whether actorID is currently subscribed to
// entityID.
func IsSubscribed(ctx context.Context, q Querier, entityID, actorID int64) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1 FROM subscriptions WHERE entity_id = ? AND actor_id = ?`,
		entityID, actorID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check subscription: %w", err)
	}
	return true, nil
}

// ListSubscriberActorIDs returns every actor currently subscribed to
// entityID — the recipient list a "changed"/"commented" notification
// fans out to (internal/service excludes the acting actor itself
// before inserting notifications, not this query).
func ListSubscriberActorIDs(ctx context.Context, q Querier, entityID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT actor_id FROM subscriptions WHERE entity_id = ?`, entityID)
	if err != nil {
		return nil, fmt.Errorf("list subscribers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscribers: %w", err)
	}
	return ids, nil
}
