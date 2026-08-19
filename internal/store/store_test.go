package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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

// TestTimeLayoutIsFixedWidth guards against regressing to
// time.RFC3339Nano, whose "9" fractional digits strip trailing zeros —
// which broke ListProjects's cursor ordering: two rows with different
// fractional-digit counts compared lexicographically wrong (e.g.
// ".5967807Z" sorted after ".59678071Z", since 'Z' > '1'). This test
// crafts timestamps of deliberately different widths and inserts them
// directly (bypassing nowUTC, which can't produce mixed widths on its
// own) to prove the layout — and therefore ORDER BY / cursor
// comparisons over it — no longer depends on wall-clock luck.
func TestTimeLayoutIsFixedWidth(t *testing.T) {
	if got := len(time.Now().UTC().Format(TimeLayout)); got != len("2026-08-19T12:00:00.000000000Z") {
		t.Fatalf("TimeLayout produced a %d-char timestamp, want fixed 30 chars", got)
	}

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	// Same instant, formatted at two different (now impossible via
	// nowUTC, but historically produced by time.RFC3339Nano) widths.
	earlier := "2026-08-19T12:00:00.500000000Z" // fixed-width: fractional 5*10^8 ns
	later := "2026-08-19T12:00:00.510000000Z"   // fixed-width: fractional 5.1*10^8 ns, genuinely later

	projA, _, err := InsertEntity(ctx, s.DB(), nil, "project")
	if err != nil {
		t.Fatalf("insert project A: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE entities SET created_at = ?, updated_at = ? WHERE id = ?`, earlier, earlier, projA); err != nil {
		t.Fatalf("backdate project A: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO projects(id, key, title) VALUES (?, 'AAA', 'A')`, projA); err != nil {
		t.Fatalf("insert projects row A: %v", err)
	}

	projB, _, err := InsertEntity(ctx, s.DB(), nil, "project")
	if err != nil {
		t.Fatalf("insert project B: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE entities SET created_at = ?, updated_at = ? WHERE id = ?`, later, later, projB); err != nil {
		t.Fatalf("backdate project B: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO projects(id, key, title) VALUES (?, 'BBB', 'B')`, projB); err != nil {
		t.Fatalf("insert projects row B: %v", err)
	}

	page, err := ListProjects(ctx, s.DB(), 10, "", 0)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(page.Projects) != 2 || page.Projects[0].Key != "AAA" || page.Projects[1].Key != "BBB" {
		got := make([]string, len(page.Projects))
		for i, p := range page.Projects {
			got[i] = p.Key
		}
		t.Fatalf("ListProjects order = %v, want [AAA BBB] (created_at ordering must respect actual chronology, not string width)", got)
	}
}

// TestMigrationVersionParsedFromFilename guards against the version
// being re-derived from a migration file's sorted position in the glob
// result, which would silently re-map every later migration's identity
// in schema_migrations if a new file happened to sort before an
// already-applied one.
func TestMigrationVersionParsedFromFilename(t *testing.T) {
	cases := []struct {
		path    string
		want    int
		wantErr bool
	}{
		{path: "migrations/0001_initial.sql", want: 1},
		{path: "migrations/0002_core_domain.sql", want: 2},
		{path: "migrations/0010_something.sql", want: 10},
		{path: "noseparator.sql", wantErr: true},
		{path: "migrations/abc_bad.sql", wantErr: true},
	}
	for _, tc := range cases {
		got, err := migrationVersion(tc.path)
		if tc.wantErr {
			if err == nil {
				t.Errorf("migrationVersion(%q) = %d, nil; want an error", tc.path, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("migrationVersion(%q): unexpected error: %v", tc.path, err)
			continue
		}
		if got != tc.want {
			t.Errorf("migrationVersion(%q) = %d, want %d", tc.path, got, tc.want)
		}
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
