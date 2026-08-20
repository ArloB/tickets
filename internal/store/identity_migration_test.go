package store

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestIdentityTablesExist confirms migration 0004 actually created the
// tables Phase 2's identity work depends on — a cheap regression check
// against a typo in the migration SQL silently leaving one out.
func TestIdentityTablesExist(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	for _, table := range []string{"human_accounts", "sessions", "agent_tokens", "login_attempts", "idempotency_keys"} {
		var name string
		err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q: %v", table, err)
		}
	}
}

// insertActor inserts a bare actors row directly (there is no
// store-level CreateActor yet — that lands with Phase 2's account/agent
// service work), using randomblob(16) for the uuid exactly as
// migration 0002_core_domain.sql's own seed rows do.
func insertActor(t *testing.T, s *Store, kind domain.ActorKind, name string) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := s.DB().Exec(
		`INSERT INTO actors(uuid, kind, name, created_at, updated_at) VALUES (randomblob(16), ?, ?, ?, ?)`,
		string(kind), name, Now(), Now(),
	); err != nil {
		t.Fatalf("insert actor %s:%s: %v", kind, name, err)
	}
	id, err := GetActorIDByRef(ctx, s.DB(), kind, name)
	if err != nil {
		t.Fatalf("resolve inserted actor %s:%s: %v", kind, name, err)
	}
	return id
}

// TestIdempotencyKeyScopedByActor is the migration-level half of ADR
// 0008's actor-scoping fix: two different actors reusing the same
// client-chosen idempotency key must not collide, and one actor
// reusing its own key must still be rejected by the primary key.
func TestIdempotencyKeyScopedByActor(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	agentA := insertActor(t, s, domain.ActorAgent, "agent-a")
	agentB := insertActor(t, s, domain.ActorAgent, "agent-b")

	insert := func(actorID int64) error {
		_, err := s.DB().Exec(
			`INSERT INTO idempotency_keys(key, actor_id, fingerprint, ref_key, created_at) VALUES (?, ?, ?, ?, ?)`,
			"shared-key", actorID, "fp", "ABC-1", Now(),
		)
		return err
	}

	if err := insert(agentA); err != nil {
		t.Fatalf("agent A insert with a fresh key: %v", err)
	}
	if err := insert(agentB); err != nil {
		t.Fatalf("agent B reusing agent A's key: want success (scoped by actor), got %v", err)
	}
	if err := insert(agentA); err == nil {
		t.Fatalf("agent A reusing its own key: want a primary-key conflict, got nil")
	}
}
