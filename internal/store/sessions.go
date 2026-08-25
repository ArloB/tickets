package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SessionRow is what GetSession returns.
type SessionRow struct {
	ID        string
	ActorID   int64
	CSRFToken string
	ExpiresAt string
}

// CreateSession inserts a new session row. id is the session's raw
// cookie value, stored directly rather than hashed — see migration
// 0004's comment on sessions.id for why that asymmetry with
// agent_tokens is deliberate.
func CreateSession(ctx context.Context, q Querier, id string, actorID int64, csrfToken, expiresAt, now string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO sessions(id, actor_id, csrf_token, created_at, last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, actorID, csrfToken, now, now, expiresAt,
	); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetSession resolves a session by its raw id, or ErrNotFound. It does
// not itself check expiry — the caller compares ExpiresAt against the
// current time, so an expired session can be distinguished from a
// missing one if that ever matters (e.g. for logging).
func GetSession(ctx context.Context, q Querier, id string) (SessionRow, error) {
	var row SessionRow
	err := q.QueryRowContext(ctx,
		`SELECT id, actor_id, csrf_token, expires_at FROM sessions WHERE id = ?`,
		id,
	).Scan(&row.ID, &row.ActorID, &row.CSRFToken, &row.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRow{}, ErrNotFound
	}
	if err != nil {
		return SessionRow{}, fmt.Errorf("get session: %w", err)
	}
	return row, nil
}

// TouchSession updates last_seen_at only — expires_at (the hard
// expiry) is untouched, so touching a session on every authenticated
// request never silently extends its lifetime.
func TouchSession(ctx context.Context, q Querier, id, now string) error {
	if _, err := q.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, now, id); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// DeleteSession removes a session row outright (logout).
func DeleteSession(ctx context.Context, q Querier, id string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteSessionsByActor removes every session belonging to actorID —
// used on password change (Phase 7) so a changed password actually
// ends every session that used the old one, rather than leaving
// already-issued cookies valid until their own expiry.
func DeleteSessionsByActor(ctx context.Context, q Querier, actorID int64) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM sessions WHERE actor_id = ?`, actorID); err != nil {
		return fmt.Errorf("delete sessions by actor: %w", err)
	}
	return nil
}
