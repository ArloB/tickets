package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AgentTokenRow is what GetAgentTokenByHash/ListAgentTokens return.
type AgentTokenRow struct {
	ID          int64
	ActorID     int64
	Description string
	CreatedAt   string
	ExpiresAt   *string
	RevokedAt   *string
}

// CreateAgentToken inserts a new token row, returning its id — what a
// later revoke call names (product spec §4.1: one agent can hold
// several independently revocable tokens).
func CreateAgentToken(ctx context.Context, q Querier, actorID int64, tokenHash, description string, expiresAt *string, now string) (int64, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO agent_tokens(actor_id, token_hash, description, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		actorID, tokenHash, description, now, expiresAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert agent token: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// GetAgentTokenByHash resolves a presented bearer token's hash to its
// row, or ErrNotFound. The caller still has to check
// RevokedAt/ExpiresAt itself (see service.VerifyBearerToken) — a hash
// match alone isn't enough to accept the token.
func GetAgentTokenByHash(ctx context.Context, q Querier, hash string) (AgentTokenRow, error) {
	var row AgentTokenRow
	err := q.QueryRowContext(ctx,
		`SELECT id, actor_id, description, created_at, expires_at, revoked_at FROM agent_tokens WHERE token_hash = ?`,
		hash,
	).Scan(&row.ID, &row.ActorID, &row.Description, &row.CreatedAt, &row.ExpiresAt, &row.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentTokenRow{}, ErrNotFound
	}
	if err != nil {
		return AgentTokenRow{}, fmt.Errorf("get agent token by hash: %w", err)
	}
	return row, nil
}

// GetAgentTokenByID resolves one token row by its surrogate id, or
// ErrNotFound — used by callers (cmd/tickets admin token revoke) that
// need to distinguish "already revoked" from "no such token", since
// RevokeAgentToken's own idempotent UPDATE can't tell those apart from
// RowsAffected alone.
func GetAgentTokenByID(ctx context.Context, q Querier, id int64) (AgentTokenRow, error) {
	var row AgentTokenRow
	err := q.QueryRowContext(ctx,
		`SELECT id, actor_id, description, created_at, expires_at, revoked_at FROM agent_tokens WHERE id = ?`,
		id,
	).Scan(&row.ID, &row.ActorID, &row.Description, &row.CreatedAt, &row.ExpiresAt, &row.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentTokenRow{}, ErrNotFound
	}
	if err != nil {
		return AgentTokenRow{}, fmt.Errorf("get agent token by id: %w", err)
	}
	return row, nil
}

// RevokeAgentToken sets revoked_at once, permanently — there is no
// un-revoke (migration 0004's comment on agent_tokens.revoked_at). A
// no-op, not an error, if the token is already revoked, so a repeated
// revoke call is safely idempotent.
func RevokeAgentToken(ctx context.Context, q Querier, tokenID int64, now string) error {
	if _, err := q.ExecContext(ctx,
		`UPDATE agent_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now, tokenID,
	); err != nil {
		return fmt.Errorf("revoke agent token: %w", err)
	}
	return nil
}

// ListAgentTokens returns every token belonging to an agent actor —
// including revoked or expired ones, so an admin view can show full
// history — newest first.
func ListAgentTokens(ctx context.Context, q Querier, actorID int64) ([]AgentTokenRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, actor_id, description, created_at, expires_at, revoked_at FROM agent_tokens
		 WHERE actor_id = ? ORDER BY created_at DESC, id DESC`,
		actorID,
	)
	if err != nil {
		return nil, fmt.Errorf("list agent tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AgentTokenRow
	for rows.Next() {
		var row AgentTokenRow
		if err := rows.Scan(&row.ID, &row.ActorID, &row.Description, &row.CreatedAt, &row.ExpiresAt, &row.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan agent token: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
