package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestSessionRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	actorID, err := CreateActor(ctx, db, domain.ActorHuman, "alice", "", nil, Now())
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}

	if err := CreateSession(ctx, db, "sess-1", actorID, "csrf-1", "2030-01-01T00:00:00.000000000Z", Now()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	row, err := GetSession(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if row.ActorID != actorID || row.CSRFToken != "csrf-1" || row.ExpiresAt != "2030-01-01T00:00:00.000000000Z" {
		t.Errorf("GetSession = %+v, want actor_id=%d csrf=csrf-1", row, actorID)
	}

	if err := TouchSession(ctx, db, "sess-1", Now()); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	if err := DeleteSession(ctx, db, "sess-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := GetSession(ctx, db, "sess-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession after delete = %v, want ErrNotFound", err)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := GetSession(context.Background(), s.DB(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession(does-not-exist) error = %v, want ErrNotFound", err)
	}
}
