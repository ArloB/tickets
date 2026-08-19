package store

import (
	"context"
	"path/filepath"
	"testing"
)

// TestOpenUnusualPath is Phase 0 verification gate 6, as a regression
// test: a data directory whose path contains a space and a non-ASCII
// character must work, on whichever platform this test runs. Verified
// manually during Phase 0 (both raw-path DSN construction and a
// literal '?' in the directory name survived), codified here so it
// can't silently regress.
func TestOpenUnusualPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tickets tëst dir")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q): %v", dir, err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// Exercise an actual write, not just PRAGMA/ping — the regression
	// this guards against is the DSN's "?_pragma=..." suffix breaking
	// once the path portion itself contains reserved-looking characters.
	if _, _, err := InsertEntity(context.Background(), s.DB(), nil, "project"); err != nil {
		t.Fatalf("InsertEntity on unusual path: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening the same data dir must not fail or reapply migration 1.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open (same data dir): %v", err)
	}
	defer func() { _ = s2.Close() }()

	var count int
	if err := s2.DB().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_migrations has %d rows after two opens, want 1", count)
	}
}
