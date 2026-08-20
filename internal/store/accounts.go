package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/google/uuid"
)

// CreateActor inserts a new actor row — the actor-creation surface ADR
// 0012 flagged as missing until Phase 2's identity work landed
// (GetActorIDByRef/GetActorRefByID, the only actor functions that
// existed before this, are read-only). description is product spec
// §4.1's optional agent description (meaningless for human/system
// actors, which pass ""); ownerID is an agent's owning human; nil for
// a human or system actor.
func CreateActor(ctx context.Context, q Querier, kind domain.ActorKind, name, description string, ownerID *int64, now string) (int64, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return 0, fmt.Errorf("generate actor uuid: %w", err)
	}
	res, err := q.ExecContext(ctx,
		`INSERT INTO actors(uuid, kind, name, description, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u[:], string(kind), name, description, ownerID, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert actor: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// CreateHumanAccount attaches password credentials to an existing
// human actor (see CreateActor) — the same 1:1 extension pattern ADR
// 0002 uses for entities/projects/etc., applied here to
// actors/human_accounts.
func CreateHumanAccount(ctx context.Context, q Querier, actorID int64, username, passwordHash string, isAdmin bool, now string) error {
	admin := 0
	if isAdmin {
		admin = 1
	}
	if _, err := q.ExecContext(ctx,
		`INSERT INTO human_accounts(actor_id, username, password_hash, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		actorID, username, passwordHash, admin, now, now,
	); err != nil {
		return fmt.Errorf("insert human account: %w", err)
	}
	return nil
}

// HumanAccountRow is what GetHumanAccountByUsername returns.
type HumanAccountRow struct {
	ActorID      int64
	Username     string
	PasswordHash string
	IsAdmin      bool
}

// GetHumanAccountByUsername resolves a login's account row, or
// ErrNotFound.
func GetHumanAccountByUsername(ctx context.Context, q Querier, username string) (HumanAccountRow, error) {
	var row HumanAccountRow
	var admin int
	err := q.QueryRowContext(ctx,
		`SELECT actor_id, username, password_hash, is_admin FROM human_accounts WHERE username = ?`,
		username,
	).Scan(&row.ActorID, &row.Username, &row.PasswordHash, &admin)
	if errors.Is(err, sql.ErrNoRows) {
		return HumanAccountRow{}, ErrNotFound
	}
	if err != nil {
		return HumanAccountRow{}, fmt.Errorf("get human account %q: %w", username, err)
	}
	row.IsAdmin = admin != 0
	return row, nil
}

// GetHumanAccountByActorID resolves a human account by its actor id —
// the lookup internal/service.ResolveSession needs, since a sessions
// row stores actor_id, not username.
func GetHumanAccountByActorID(ctx context.Context, q Querier, actorID int64) (HumanAccountRow, error) {
	var row HumanAccountRow
	var admin int
	err := q.QueryRowContext(ctx,
		`SELECT actor_id, username, password_hash, is_admin FROM human_accounts WHERE actor_id = ?`,
		actorID,
	).Scan(&row.ActorID, &row.Username, &row.PasswordHash, &admin)
	if errors.Is(err, sql.ErrNoRows) {
		return HumanAccountRow{}, ErrNotFound
	}
	if err != nil {
		return HumanAccountRow{}, fmt.Errorf("get human account by actor id %d: %w", actorID, err)
	}
	row.IsAdmin = admin != 0
	return row, nil
}

// CountHumanAccounts backs `tickets setup`'s first-run check: setup
// refuses to run once any human account exists, so an installation
// only ever gets its admin created once.
func CountHumanAccounts(ctx context.Context, q Querier) (int, error) {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM human_accounts`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count human accounts: %w", err)
	}
	return count, nil
}

// AgentRow is an agent actor's admin-view detail row (product spec
// §4.1: "a name, optional description, owning human").
type AgentRow struct {
	Name        string
	Description string
	OwnerName   string // "" if the agent has no resolvable owner
	CreatedAt   string
}

// GetAgentByName resolves one agent's detail row by name, or
// ErrNotFound.
func GetAgentByName(ctx context.Context, q Querier, name string) (AgentRow, error) {
	var row AgentRow
	var ownerName sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT a.description, a.created_at, o.name
		 FROM actors a LEFT JOIN actors o ON o.id = a.owner_id
		 WHERE a.kind = 'agent' AND a.name = ? AND a.deleted_at IS NULL`,
		name,
	).Scan(&row.Description, &row.CreatedAt, &ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentRow{}, ErrNotFound
	}
	if err != nil {
		return AgentRow{}, fmt.Errorf("get agent %q: %w", name, err)
	}
	row.Name = name
	row.OwnerName = ownerName.String
	return row, nil
}

// ListAgents returns every non-deleted agent's detail row, ordered by
// name — the admin agent-management view's data source.
func ListAgents(ctx context.Context, q Querier) ([]AgentRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT a.name, a.description, a.created_at, o.name
		 FROM actors a LEFT JOIN actors o ON o.id = a.owner_id
		 WHERE a.kind = 'agent' AND a.deleted_at IS NULL ORDER BY a.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AgentRow
	for rows.Next() {
		var row AgentRow
		var ownerName sql.NullString
		if err := rows.Scan(&row.Name, &row.Description, &row.CreatedAt, &ownerName); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		row.OwnerName = ownerName.String
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
