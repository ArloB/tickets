package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestOpenTakesPreMigrationBackupWhenUpgradingExistingDatabase is
// Phase 6 Step 3's regression test for the actual upgrade path: a
// data directory whose database already has schema history (some
// migrations applied, at least one pending) must get a pre-migration
// snapshot before Open applies the pending migration. Mirrors
// audit_migration_test.go's applyMigrationsThrough helper to build a
// database stuck one version behind, then reopens it through the real
// Store.Open (not the raw-db harness) to exercise migrate's actual
// gating logic.
func TestOpenTakesPreMigrationBackupWhenUpgradingExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tickets.db")

	highest, err := highestEmbeddedMigrationVersion()
	if err != nil {
		t.Fatalf("highestEmbeddedMigrationVersion: %v", err)
	}
	if highest < 2 {
		t.Skip("needs at least two migrations to leave one pending")
	}
	stuckAt := highest - 1

	raw, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	applyMigrationsThrough(t, raw, stuckAt)
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (upgrading v%d -> v%d): %v", stuckAt, highest, err)
	}
	defer func() { _ = s.Close() }()

	backupDir := filepath.Join(dir, preMigrationBackupDir)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("pre-migration backups = %d, want exactly 1: %v", len(entries), entries)
	}
	wantPrefix := "pre-migration-v" + strconv.Itoa(stuckAt) + "-"
	if got := entries[0].Name(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("backup filename = %q, want prefix %q", got, wantPrefix)
	}

	// The snapshot must reflect the database as it stood *before* the
	// final migration — proving this ran before, not after, applying
	// it. audit_events.target_actor_id is migration 0013's marker
	// column; if this repo's highest migration isn't 0013 (a later
	// phase added more), this assertion becomes a no-op rather than a
	// false failure, since it only checks a column this specific
	// migration adds.
	if highest == 13 {
		backupPath := filepath.Join(backupDir, entries[0].Name())
		bdb, err := sql.Open("sqlite", backupPath)
		if err != nil {
			t.Fatalf("open backup snapshot: %v", err)
		}
		defer func() { _ = bdb.Close() }()
		var hasColumn int
		if err := bdb.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('audit_events') WHERE name = 'target_actor_id'`,
		).Scan(&hasColumn); err != nil {
			t.Fatalf("inspect backup schema: %v", err)
		}
		if hasColumn != 0 {
			t.Error("pre-migration backup already has migration 0013's target_actor_id column — snapshot was taken too late")
		}
	}

	// Reopening at the now-current version must not take a second backup.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("re-open at current version: %v", err)
	}
	_ = s2.Close()
	entriesAfter, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir after reopen: %v", err)
	}
	if len(entriesAfter) != 1 {
		t.Errorf("pre-migration backups after a no-op reopen = %d, want still exactly 1", len(entriesAfter))
	}
}

// TestOpenOnFreshDatabaseTakesNoPreMigrationBackup confirms the
// common case (every store.Open(t.TempDir()) call across this
// codebase's test suite) never pays the backup cost: a brand-new
// database has nothing to lose, so no backups/ directory is created
// at all.
func TestOpenOnFreshDatabaseTakesNoPreMigrationBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := os.Stat(filepath.Join(dir, preMigrationBackupDir)); !os.IsNotExist(err) {
		t.Errorf("backups dir exists after a fresh Open: %v", err)
	}
}

// TestPreMigrationSnapshotIsUsableForRecovery is Phase 6 Step 8's
// recovery drill for the pre-migration backup: build a database stuck
// one version behind with a real project row already in it, let Open
// take its pre-migration snapshot while upgrading, then prove that
// snapshot isn't just present with the right schema (that's what
// TestOpenTakesPreMigrationBackupWhenUpgradingExistingDatabase already
// checks) but is actually *usable* — a corrupted or truncated VACUUM
// INTO output would fail one of the checks below, not just look right
// from the outside.
func TestPreMigrationSnapshotIsUsableForRecovery(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tickets.db")

	highest, err := highestEmbeddedMigrationVersion()
	if err != nil {
		t.Fatalf("highestEmbeddedMigrationVersion: %v", err)
	}
	if highest < 2 {
		t.Skip("needs at least two migrations to leave one pending")
	}
	stuckAt := highest - 1

	raw, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	applyMigrationsThrough(t, raw, stuckAt)

	if _, err := raw.Exec(`INSERT INTO actors(uuid, kind, name, created_at, updated_at) VALUES (randomblob(16), 'human', 'recovery-drill', ?, ?)`, Now(), Now()); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	res, err := raw.Exec(`INSERT INTO entities(uuid, project_id, kind, created_at, updated_at) VALUES (randomblob(16), NULL, 'project', ?, ?)`, Now(), Now())
	if err != nil {
		t.Fatalf("seed project entity: %v", err)
	}
	entityID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("resolve seeded entity id: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO projects(id, key, title) VALUES (?, 'REC', 'Recovery Drill Project')`, entityID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (upgrading v%d -> v%d): %v", stuckAt, highest, err)
	}
	_ = s.Close()

	backupDir := filepath.Join(dir, preMigrationBackupDir)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("pre-migration backups = %d, want exactly 1: %v", len(entries), entries)
	}
	snapshotPath := filepath.Join(backupDir, entries[0].Name())

	// Simulate the actual recovery drill an operator would run: copy
	// the snapshot into a fresh data directory (never touch it
	// in-place) and open it exactly the way store.Open would open any
	// other data directory's database.
	recoveryDir := t.TempDir()
	recoveredDBPath := filepath.Join(recoveryDir, "tickets.db")
	snapshotBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if err := os.WriteFile(recoveredDBPath, snapshotBytes, 0o600); err != nil {
		t.Fatalf("write recovered db copy: %v", err)
	}

	recovered, err := sql.Open("sqlite", recoveredDBPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open recovered snapshot: %v", err)
	}
	defer func() { _ = recovered.Close() }()

	var integrityOK string
	if err := recovered.QueryRow(`PRAGMA integrity_check`).Scan(&integrityOK); err != nil {
		t.Fatalf("PRAGMA integrity_check on recovered snapshot: %v", err)
	}
	if integrityOK != "ok" {
		t.Fatalf("PRAGMA integrity_check = %q, want ok — the pre-migration snapshot is corrupted", integrityOK)
	}

	var gotVersion int
	if err := recovered.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&gotVersion); err != nil {
		t.Fatalf("query schema version in recovered snapshot: %v", err)
	}
	if gotVersion != stuckAt {
		t.Errorf("recovered snapshot's schema version = %d, want %d (the version before the migration that triggered the snapshot)", gotVersion, stuckAt)
	}

	var title string
	if err := recovered.QueryRow(`SELECT title FROM projects WHERE key = 'REC'`).Scan(&title); err != nil {
		t.Fatalf("query the seeded project in the recovered snapshot: %v", err)
	}
	if title != "Recovery Drill Project" {
		t.Errorf("recovered project title = %q, want %q", title, "Recovery Drill Project")
	}
}

// TestPruneOldPreMigrationBackupsKeepsOnlyTheMostRecent confirms
// retention: only preMigrationBackupsKept filenames survive, and
// they're the lexically-last (chronologically-last) ones.
func TestPruneOldPreMigrationBackupsKeepsOnlyTheMostRecent(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"pre-migration-v1-20260101T000000.000000000Z.db",
		"pre-migration-v1-20260102T000000.000000000Z.db",
		"pre-migration-v2-20260103T000000.000000000Z.db",
		"pre-migration-v2-20260104T000000.000000000Z.db",
		"pre-migration-v3-20260105T000000.000000000Z.db",
		"pre-migration-v3-20260106T000000.000000000Z.db",
		"pre-migration-v4-20260107T000000.000000000Z.db",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := pruneOldPreMigrationBackups(dir); err != nil {
		t.Fatalf("pruneOldPreMigrationBackups: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != preMigrationBackupsKept {
		t.Fatalf("remaining backups = %d, want %d", len(entries), preMigrationBackupsKept)
	}
	wantKept := names[len(names)-preMigrationBackupsKept:]
	for _, want := range wantKept {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("expected %q to survive pruning: %v", want, err)
		}
	}
}
