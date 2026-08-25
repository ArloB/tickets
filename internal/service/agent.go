package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// CreateAgentRequest is CreateAgent's input.
type CreateAgentRequest struct {
	Name        string
	Description string
}

// CreateAgent creates a new agent actor, owned by the calling human
// (product spec §4.1: "an authenticated human creates... agent
// identities"). actor is that human; withTx resolves it the same way
// every other mutation does, and its resolved id becomes the new
// agent's owner_id.
//
// Emits an agent_created audit event with the new agent as
// target_actor_id (ADR 0012's amendment, Phase 6 Step 1) — resolving
// this ADR's previously-open question of whether product spec §5.12's
// "token operation" auditing is satisfied by agent_tokens' own
// columns: it wasn't, since those columns record when but not who
// performed the action.
func (s *Service) CreateAgent(ctx context.Context, req CreateAgentRequest, actor domain.ActorRef, correlationID string) (AgentDetail, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return AgentDetail{}, newValidationError("name", "agent name is required")
	}

	if _, err := store.GetActorIDByRef(ctx, s.store.DB(), domain.ActorAgent, name); err == nil {
		return AgentDetail{}, newAlreadyExistsError("name", "an agent named %q already exists", name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return AgentDetail{}, fmt.Errorf("service: check existing agent: %w", err)
	}

	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, ownerID int64, corrID, now string) error {
		agentID, err := store.CreateActor(ctx, tx, domain.ActorAgent, name, req.Description, &ownerID, now)
		if err != nil {
			return err
		}
		changes := auditChanges(map[string]any{"name": name})
		return store.InsertActorAuditEvent(ctx, tx, agentID, ownerID, eventAgentCreated, corrID, changes, now)
	})
	if err != nil {
		return AgentDetail{}, err
	}
	return s.GetAgentDetail(ctx, name)
}

// AgentDetail is an agent's admin-view detail (product spec §4.1: "a
// name, optional description, owning human"). Owner is nil only if the
// agent somehow has no resolvable owning human (shouldn't happen
// through CreateAgent, which always sets one, but a row is a row).
type AgentDetail struct {
	Ref         domain.ActorRef
	Description string
	Owner       *domain.ActorRef
	CreatedAt   time.Time
}

func agentRowToDetail(row store.AgentRow) (AgentDetail, error) {
	createdAt, err := time.Parse(store.TimeLayout, row.CreatedAt)
	if err != nil {
		return AgentDetail{}, fmt.Errorf("service: parse agent created_at: %w", err)
	}
	d := AgentDetail{
		Ref:         domain.ActorRef{Kind: domain.ActorAgent, Name: row.Name},
		Description: row.Description,
		CreatedAt:   createdAt,
	}
	if row.OwnerName != "" {
		owner := domain.ActorRef{Kind: domain.ActorHuman, Name: row.OwnerName}
		d.Owner = &owner
	}
	return d, nil
}

// GetAgentDetail resolves one agent's admin-view detail by name.
func (s *Service) GetAgentDetail(ctx context.Context, name string) (AgentDetail, error) {
	row, err := store.GetAgentByName(ctx, s.store.DB(), name)
	if errors.Is(err, store.ErrNotFound) {
		return AgentDetail{}, newNotFoundError("agent %q not found", name)
	}
	if err != nil {
		return AgentDetail{}, fmt.Errorf("service: get agent: %w", err)
	}
	return agentRowToDetail(row)
}

// ListAgents returns every agent's admin-view detail, ordered by name
// — the admin agent-management view's data source.
func (s *Service) ListAgents(ctx context.Context) ([]AgentDetail, error) {
	rows, err := store.ListAgents(ctx, s.store.DB())
	if err != nil {
		return nil, fmt.Errorf("service: list agents: %w", err)
	}
	out := make([]AgentDetail, len(rows))
	for i, row := range rows {
		d, err := agentRowToDetail(row)
		if err != nil {
			return nil, err
		}
		out[i] = d
	}
	return out, nil
}

// CreateAgentToken issues a fresh bearer token for agentRef, returning
// its raw value exactly once (ADR 0004) alongside the token's id (what
// a later RevokeAgentToken call names). actor is the admin performing
// the action, resolved the same way every mutation resolves its actor.
// expiresAt is nil for a token with no expiry (product spec §10:
// "support optional expiry"); when set, VerifyBearerToken rejects the
// token once it's in the past.
//
// Emits an agent_token_issued audit event (ADR 0012's amendment,
// Phase 6 Step 1) with the token's id and description only — never the
// raw value or its hash (product spec §10: "do not place token values
// in ... audit events").
func (s *Service) CreateAgentToken(ctx context.Context, agentRef domain.ActorRef, description string, expiresAt *time.Time, actor domain.ActorRef, correlationID string) (rawToken string, tokenID int64, err error) {
	if agentRef.Kind != domain.ActorAgent {
		return "", 0, newValidationError("agent", "tokens can only be issued to an agent actor, not %q", agentRef.Kind)
	}

	agentID, err := store.GetActorIDByRef(ctx, s.store.DB(), agentRef.Kind, agentRef.Name)
	if errors.Is(err, store.ErrNotFound) {
		return "", 0, newNotFoundError("agent %q not found", agentRef.Name)
	}
	if err != nil {
		return "", 0, fmt.Errorf("service: resolve agent: %w", err)
	}

	raw, hash, err := auth.GenerateToken()
	if err != nil {
		return "", 0, fmt.Errorf("service: generate token: %w", err)
	}

	var expiresAtStr *string
	if expiresAt != nil {
		v := expiresAt.UTC().Format(store.TimeLayout)
		expiresAtStr = &v
	}

	err = s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		id, err := store.CreateAgentToken(ctx, tx, agentID, hash, description, expiresAtStr, now)
		if err != nil {
			return err
		}
		tokenID = id
		changes := auditChanges(map[string]any{"token_id": id, "description": description})
		return store.InsertActorAuditEvent(ctx, tx, agentID, actorID, eventAgentTokenIssued, corrID, changes, now)
	})
	if err != nil {
		return "", 0, err
	}
	return raw, tokenID, nil
}

// RevokeAgentToken permanently revokes a token by id (ADR 0004: there
// is no un-revoke). actor is the admin performing the revoke.
//
// Emits an agent_token_revoked audit event (ADR 0012's amendment,
// Phase 6 Step 1) targeting the owning agent, resolved from the token
// row itself so a caller doesn't need to already know which agent owns
// tokenID.
func (s *Service) RevokeAgentToken(ctx context.Context, tokenID int64, actor domain.ActorRef, correlationID string) error {
	tokenRow, err := store.GetAgentTokenByID(ctx, s.store.DB(), tokenID)
	if errors.Is(err, store.ErrNotFound) {
		return newNotFoundError("token %d not found", tokenID)
	}
	if err != nil {
		return fmt.Errorf("service: resolve token's agent: %w", err)
	}
	agentID := tokenRow.ActorID

	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if err := store.RevokeAgentToken(ctx, tx, tokenID, now); err != nil {
			return err
		}
		changes := auditChanges(map[string]any{"token_id": tokenID})
		return store.InsertActorAuditEvent(ctx, tx, agentID, actorID, eventAgentTokenRevoked, corrID, changes, now)
	})
}

// AgentTokenSummary is ListAgentTokens' element type — never carries a
// raw token value (only CreateAgentToken's direct return does, once).
// Timestamps are time.Time, matching every other service-layer type
// (domain.Ticket, CreateSession's return) rather than internal/store's
// raw TimeLayout strings — those are an internal/store-only concern.
type AgentTokenSummary struct {
	ID          int64
	Description string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
}

// ListAgentTokens returns every token (including revoked ones)
// belonging to agentRef, newest first.
func (s *Service) ListAgentTokens(ctx context.Context, agentRef domain.ActorRef) ([]AgentTokenSummary, error) {
	agentID, err := store.GetActorIDByRef(ctx, s.store.DB(), agentRef.Kind, agentRef.Name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("agent %q not found", agentRef.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("service: resolve agent: %w", err)
	}

	rows, err := store.ListAgentTokens(ctx, s.store.DB(), agentID)
	if err != nil {
		return nil, fmt.Errorf("service: list agent tokens: %w", err)
	}
	out := make([]AgentTokenSummary, len(rows))
	for i, row := range rows {
		createdAt, perr := time.Parse(store.TimeLayout, row.CreatedAt)
		if perr != nil {
			return nil, fmt.Errorf("service: parse agent token created_at: %w", perr)
		}
		expiresAt, perr := parseOptionalTime(row.ExpiresAt)
		if perr != nil {
			return nil, fmt.Errorf("service: parse agent token expires_at: %w", perr)
		}
		revokedAt, perr := parseOptionalTime(row.RevokedAt)
		if perr != nil {
			return nil, fmt.Errorf("service: parse agent token revoked_at: %w", perr)
		}
		out[i] = AgentTokenSummary{
			ID: row.ID, Description: row.Description, CreatedAt: createdAt,
			ExpiresAt: expiresAt, RevokedAt: revokedAt,
		}
	}
	return out, nil
}

// parseOptionalTime parses s (in store.TimeLayout) if non-nil, or
// returns a nil *time.Time unchanged.
func parseOptionalTime(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := time.Parse(store.TimeLayout, *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// VerifyBearerToken hashes rawToken and resolves it to the owning
// agent's ActorRef, or an unauthorized error if the token is unknown,
// expired, or revoked. This is the single source of truth both HTTP
// bearer-token middleware and MCP's bearer-token wrapper call (ADR
// 0005: one authorization boundary shared by both transports).
func (s *Service) VerifyBearerToken(ctx context.Context, rawToken string) (domain.ActorRef, error) {
	db := s.store.DB()
	hash := auth.HashToken(rawToken)

	row, err := store.GetAgentTokenByHash(ctx, db, hash)
	if errors.Is(err, store.ErrNotFound) {
		return domain.ActorRef{}, &Error{Code: domain.ErrUnauthorized, Message: "invalid bearer token"}
	}
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: look up bearer token: %w", err)
	}
	if row.RevokedAt != nil {
		return domain.ActorRef{}, &Error{Code: domain.ErrUnauthorized, Message: "bearer token has been revoked"}
	}
	if row.ExpiresAt != nil {
		expiresAt, perr := time.Parse(store.TimeLayout, *row.ExpiresAt)
		if perr != nil {
			return domain.ActorRef{}, fmt.Errorf("service: parse token expiry: %w", perr)
		}
		if time.Now().UTC().After(expiresAt) {
			return domain.ActorRef{}, &Error{Code: domain.ErrUnauthorized, Message: "bearer token has expired"}
		}
	}

	ref, err := store.GetActorRefByID(ctx, db, row.ActorID)
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: resolve token's actor: %w", err)
	}
	return ref, nil
}
