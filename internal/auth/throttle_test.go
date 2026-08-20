package auth

import (
	"context"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestThrottleBlocksAfterMaxFailures(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	db := s.DB()

	for range 5 {
		if err := RecordAttempt(ctx, db, "alice", "10.0.0.1", false, store.Now()); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}

	blocked, err := TooManyAttempts(ctx, db, "alice", "10.0.0.1", time.Hour, 5)
	if err != nil {
		t.Fatalf("TooManyAttempts: %v", err)
	}
	if !blocked {
		t.Error("TooManyAttempts = false after 5 failures with max=5, want true")
	}
}

func TestThrottleIgnoresAttemptsOutsideWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	db := s.DB()

	old := time.Now().UTC().Add(-2 * time.Hour).Format(store.TimeLayout)
	for range 10 {
		if err := RecordAttempt(ctx, db, "bob", "10.0.0.2", false, old); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}

	blocked, err := TooManyAttempts(ctx, db, "bob", "10.0.0.2", time.Hour, 5)
	if err != nil {
		t.Fatalf("TooManyAttempts: %v", err)
	}
	if blocked {
		t.Error("TooManyAttempts = true for failures outside the trailing window, want false")
	}
}

func TestThrottleIgnoresSuccessfulAttempts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	db := s.DB()

	for range 10 {
		if err := RecordAttempt(ctx, db, "carol", "10.0.0.3", true, store.Now()); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}

	blocked, err := TooManyAttempts(ctx, db, "carol", "10.0.0.3", time.Hour, 5)
	if err != nil {
		t.Fatalf("TooManyAttempts: %v", err)
	}
	if blocked {
		t.Error("TooManyAttempts = true after only successful attempts, want false")
	}
}

// TestThrottleBlocksByIPAcrossDifferentUsernames confirms the IP leg
// of the throttle triggers even when each attempt uses a different
// username - otherwise an attacker could dodge a per-username lock
// simply by rotating the username on every guess.
func TestThrottleBlocksByIPAcrossDifferentUsernames(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	db := s.DB()

	for range 5 {
		if err := RecordAttempt(ctx, db, "different-user", "10.0.0.4", false, store.Now()); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}

	blocked, err := TooManyAttempts(ctx, db, "yet-another-user", "10.0.0.4", time.Hour, 5)
	if err != nil {
		t.Fatalf("TooManyAttempts: %v", err)
	}
	if !blocked {
		t.Error("TooManyAttempts = false despite 5 failures from the same IP under different usernames, want true")
	}
}
