package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestAgentTokenRoundTripAndRevoke(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	agentID, err := CreateActor(ctx, db, domain.ActorAgent, "codex", "", nil, Now())
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}

	tokenID, err := CreateAgentToken(ctx, db, agentID, "hash-1", "ci token", nil, Now())
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	row, err := GetAgentTokenByHash(ctx, db, "hash-1")
	if err != nil {
		t.Fatalf("GetAgentTokenByHash: %v", err)
	}
	if row.ID != tokenID || row.ActorID != agentID || row.RevokedAt != nil {
		t.Errorf("GetAgentTokenByHash = %+v, want id=%d actor_id=%d revoked_at=nil", row, tokenID, agentID)
	}

	if err := RevokeAgentToken(ctx, db, tokenID, Now()); err != nil {
		t.Fatalf("RevokeAgentToken: %v", err)
	}
	row, err = GetAgentTokenByHash(ctx, db, "hash-1")
	if err != nil {
		t.Fatalf("GetAgentTokenByHash after revoke: %v", err)
	}
	if row.RevokedAt == nil {
		t.Errorf("GetAgentTokenByHash after revoke: revoked_at is nil, want set")
	}

	// Revoking again must not error (idempotent) and must not clobber
	// the original revocation timestamp with a later one.
	firstRevokedAt := *row.RevokedAt
	if err := RevokeAgentToken(ctx, db, tokenID, "2099-01-01T00:00:00.000000000Z"); err != nil {
		t.Fatalf("second RevokeAgentToken: %v", err)
	}
	row, err = GetAgentTokenByHash(ctx, db, "hash-1")
	if err != nil {
		t.Fatalf("GetAgentTokenByHash after second revoke: %v", err)
	}
	if *row.RevokedAt != firstRevokedAt {
		t.Errorf("revoked_at changed on a second revoke: %q -> %q, want unchanged", firstRevokedAt, *row.RevokedAt)
	}
}

func TestGetAgentTokenByHashNotFound(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := GetAgentTokenByHash(context.Background(), s.DB(), "no-such-hash"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAgentTokenByHash(no-such-hash) error = %v, want ErrNotFound", err)
	}
}

func TestListAgentTokensNewestFirst(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	agentID, err := CreateActor(ctx, db, domain.ActorAgent, "codex", "", nil, Now())
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}

	if _, err := CreateAgentToken(ctx, db, agentID, "hash-old", "", nil, "2020-01-01T00:00:00.000000000Z"); err != nil {
		t.Fatalf("create old token: %v", err)
	}
	if _, err := CreateAgentToken(ctx, db, agentID, "hash-new", "", nil, "2030-01-01T00:00:00.000000000Z"); err != nil {
		t.Fatalf("create new token: %v", err)
	}

	tokens, err := ListAgentTokens(ctx, db, agentID)
	if err != nil {
		t.Fatalf("ListAgentTokens: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("len(tokens) = %d, want 2", len(tokens))
	}
	if tokens[0].CreatedAt != "2030-01-01T00:00:00.000000000Z" {
		t.Errorf("tokens[0].CreatedAt = %q, want the newer token first", tokens[0].CreatedAt)
	}
}
