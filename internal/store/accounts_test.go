package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateActorAndHumanAccountRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()
	now := Now()

	actorID, err := CreateActor(ctx, db, domain.ActorHuman, "alice", "", nil, now)
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	if err := CreateHumanAccount(ctx, db, actorID, "alice", "hashed-password", true, now); err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}

	row, err := GetHumanAccountByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatalf("GetHumanAccountByUsername: %v", err)
	}
	if row.ActorID != actorID || row.Username != "alice" || row.PasswordHash != "hashed-password" || !row.IsAdmin {
		t.Errorf("GetHumanAccountByUsername = %+v, want actor_id=%d username=alice hash=hashed-password admin=true", row, actorID)
	}

	ref, err := GetActorRefByID(ctx, db, actorID)
	if err != nil {
		t.Fatalf("GetActorRefByID: %v", err)
	}
	if ref.Kind != domain.ActorHuman || ref.Name != "alice" {
		t.Errorf("GetActorRefByID = %v, want human:alice", ref)
	}
}

func TestGetHumanAccountByActorID(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()
	now := Now()

	actorID, err := CreateActor(ctx, db, domain.ActorHuman, "alice", "", nil, now)
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	if err := CreateHumanAccount(ctx, db, actorID, "alice", "hashed-password", true, now); err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}

	row, err := GetHumanAccountByActorID(ctx, db, actorID)
	if err != nil {
		t.Fatalf("GetHumanAccountByActorID: %v", err)
	}
	if row.Username != "alice" || !row.IsAdmin {
		t.Errorf("GetHumanAccountByActorID = %+v, want username=alice admin=true", row)
	}
}

func TestGetHumanAccountByActorIDNotFound(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := GetHumanAccountByActorID(context.Background(), s.DB(), 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetHumanAccountByActorID(999999) error = %v, want ErrNotFound", err)
	}
}

func TestGetHumanAccountByUsernameNotFound(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := GetHumanAccountByUsername(context.Background(), s.DB(), "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetHumanAccountByUsername(nobody) error = %v, want ErrNotFound", err)
	}
}

func TestCreateActorRejectsDuplicateKindName(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	if _, err := CreateActor(ctx, db, domain.ActorAgent, "codex", "", nil, Now()); err != nil {
		t.Fatalf("first CreateActor: %v", err)
	}
	if _, err := CreateActor(ctx, db, domain.ActorAgent, "codex", "", nil, Now()); err == nil {
		t.Errorf("second CreateActor with the same kind:name: want a unique-constraint error, got nil")
	}
}

func TestCreateActorWithOwner(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	ownerID, err := CreateActor(ctx, db, domain.ActorHuman, "owner", "", nil, Now())
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	agentID, err := CreateActor(ctx, db, domain.ActorAgent, "codex", "", &ownerID, Now())
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var gotOwner int64
	if err := db.QueryRow(`SELECT owner_id FROM actors WHERE id = ?`, agentID).Scan(&gotOwner); err != nil {
		t.Fatalf("query owner_id: %v", err)
	}
	if gotOwner != ownerID {
		t.Errorf("agent owner_id = %d, want %d", gotOwner, ownerID)
	}
}

func TestGetAgentByNameAndListAgents(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	ownerID, err := CreateActor(ctx, db, domain.ActorHuman, "owner", "", nil, Now())
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := CreateActor(ctx, db, domain.ActorAgent, "codex", "CI agent", &ownerID, Now()); err != nil {
		t.Fatalf("create agent codex: %v", err)
	}
	if _, err := CreateActor(ctx, db, domain.ActorAgent, "claude", "", nil, Now()); err != nil {
		t.Fatalf("create agent claude: %v", err)
	}
	if _, err := CreateActor(ctx, db, domain.ActorHuman, "alice", "", nil, Now()); err != nil {
		t.Fatalf("create human alice: %v", err)
	}

	row, err := GetAgentByName(ctx, db, "codex")
	if err != nil {
		t.Fatalf("GetAgentByName: %v", err)
	}
	if row.Description != "CI agent" || row.OwnerName != "owner" {
		t.Errorf("GetAgentByName(codex) = %+v, want description=%q owner=owner", row, "CI agent")
	}

	agents, err := ListAgents(ctx, db)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 || agents[0].Name != "claude" || agents[1].Name != "codex" {
		t.Fatalf("ListAgents = %+v, want [claude codex] (alphabetical, humans excluded)", agents)
	}
	if agents[0].OwnerName != "" {
		t.Errorf("agents[0] (claude, no owner) OwnerName = %q, want empty", agents[0].OwnerName)
	}
}

func TestGetAgentByNameNotFound(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := GetAgentByName(context.Background(), s.DB(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAgentByName(does-not-exist) error = %v, want ErrNotFound", err)
	}
}

func TestCountHumanAccounts(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	if count, err := CountHumanAccounts(ctx, db); err != nil || count != 0 {
		t.Fatalf("CountHumanAccounts before any account = (%d, %v), want (0, nil)", count, err)
	}

	actorID, err := CreateActor(ctx, db, domain.ActorHuman, "admin", "", nil, Now())
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	if err := CreateHumanAccount(ctx, db, actorID, "admin", "hash", true, Now()); err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}

	if count, err := CountHumanAccounts(ctx, db); err != nil || count != 1 {
		t.Fatalf("CountHumanAccounts after one account = (%d, %v), want (1, nil)", count, err)
	}
}
