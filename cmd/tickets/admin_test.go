package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TestAdminPurgeIdempotencyKeys seeds one old and one recent
// idempotency_keys row directly (there's no CLI path to control
// created_at otherwise) and confirms runAdmin's purge subcommand
// deletes only the row older than --older-than.
func TestAdminPurgeIdempotencyKeys(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	actorID, err := store.GetActorIDByRef(t.Context(), st.DB(), domain.ActorSystem, "system")
	if err != nil {
		t.Fatalf("GetActorIDByRef: %v", err)
	}

	old := time.Now().UTC().Add(-60 * 24 * time.Hour).Format(store.TimeLayout)
	recent := time.Now().UTC().Format(store.TimeLayout)
	if _, err := st.DB().Exec(
		`INSERT INTO idempotency_keys (key, actor_id, fingerprint, ref_key, created_at) VALUES (?, ?, 'fp', 'ABC-1', ?)`,
		"old-key", actorID, old,
	); err != nil {
		t.Fatalf("insert old key: %v", err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO idempotency_keys (key, actor_id, fingerprint, ref_key, created_at) VALUES (?, ?, 'fp', 'ABC-2', ?)`,
		"recent-key", actorID, recent,
	); err != nil {
		t.Fatalf("insert recent key: %v", err)
	}
	_ = st.Close()

	out := captureStdout(t, func() {
		if err := runAdmin([]string{"purge-idempotency-keys", "--older-than", "720h", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runAdmin purge-idempotency-keys: %v", err)
		}
	})
	if !strings.Contains(out, "purged 1") {
		t.Errorf("purge output = %q, want it to report purging 1 key", out)
	}

	st2, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("re-open store: %v", err)
	}
	defer func() { _ = st2.Close() }()
	var remaining int
	if err := st2.DB().QueryRow(`SELECT COUNT(*) FROM idempotency_keys`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining keys: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining idempotency_keys rows = %d, want 1", remaining)
	}
}

func TestAdminRequiresSubcommand(t *testing.T) {
	if err := runAdmin(nil); err == nil {
		t.Error("runAdmin with no subcommand: want error, got nil")
	}
}

func TestAdminRejectsUnknownSubcommand(t *testing.T) {
	if err := runAdmin([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runAdmin with an unknown subcommand: want error, got nil")
	}
}
