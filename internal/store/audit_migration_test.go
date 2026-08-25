package store

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// TestActorAuditEventMigrationPreservesExistingRows is Phase 6 Step
// 1's regression test for migration 0013_actor_audit_events.sql's
// table-rebuild — the one part every other test in this package
// doesn't exercise, since Open(t.TempDir()) always applies all
// migrations to an empty schema and never copies a pre-existing row
// (identity_migration_test.go's TestIdentityTablesExist has the same
// gap for migration 0004, which is why this file mirrors its shape
// rather than reusing Open).
//
// This opens a raw database and applies only migrations 0001-0012 by
// hand (audit_events' pre-0013 shape: entity_id NOT NULL, no
// target_actor_id), inserts a row the way a pre-Phase-6 server would
// have, then applies 0013 and asserts: the row survived with its
// entity_id intact and target_actor_id NULL, the CHECK constraint
// accepts a real actor-scoped insert (target_actor_id set, entity_id
// NULL), and AUTOINCREMENT continues past the copied row's id rather
// than colliding with it. Mirrors identity_migration_test.go's raw-
// database-instead-of-Open shape, since neither file can use
// Open(t.TempDir()) — that applies every migration to an empty schema
// in one pass and never exercises copying a pre-existing row.
func TestActorAuditEventMigrationPreservesExistingRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration-test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	applyMigrationsThrough(t, db, 12)

	// Seed a project/entities/actors row so entity_id/actor_id's FKs
	// resolve, then one pre-0013-shape audit_events row exactly as
	// AddComment/CreateTicket etc. would have inserted it.
	if _, err := db.Exec(`INSERT INTO actors(uuid, kind, name, created_at, updated_at) VALUES (randomblob(16), 'human', 'audit-migration-test', ?, ?)`, Now(), Now()); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	var actorID int64
	if err := db.QueryRow(`SELECT id FROM actors WHERE name = 'audit-migration-test'`).Scan(&actorID); err != nil {
		t.Fatalf("resolve seeded actor: %v", err)
	}
	res0, err := db.Exec(`INSERT INTO entities(uuid, project_id, kind, created_at, updated_at) VALUES (randomblob(16), NULL, 'project', ?, ?)`, Now(), Now())
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	entityID, err := res0.LastInsertId()
	if err != nil {
		t.Fatalf("resolve seeded entity id: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO audit_events(entity_id, actor_id, event_type, correlation_id, changes, created_at) VALUES (?, ?, 'project_created', 'corr-1', '{}', ?)`,
		entityID, actorID, Now(),
	)
	if err != nil {
		t.Fatalf("seed pre-migration audit_events row: %v", err)
	}
	preMigrationID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	applyMigrationsThrough(t, db, 13)

	var gotEntityID sql.NullInt64
	var gotTargetActorID sql.NullInt64
	if err := db.QueryRow(
		`SELECT entity_id, target_actor_id FROM audit_events WHERE id = ?`, preMigrationID,
	).Scan(&gotEntityID, &gotTargetActorID); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if !gotEntityID.Valid || gotEntityID.Int64 != entityID {
		t.Errorf("migrated row entity_id = %+v, want %d", gotEntityID, entityID)
	}
	if gotTargetActorID.Valid {
		t.Errorf("migrated row target_actor_id = %v, want NULL", gotTargetActorID.Int64)
	}

	// A real actor-scoped insert (Phase 6 Step 1's new call shape) must
	// satisfy the CHECK constraint and land past the copied row's id.
	res, err = db.Exec(
		`INSERT INTO audit_events(target_actor_id, actor_id, event_type, correlation_id, changes, created_at) VALUES (?, ?, 'agent_created', 'corr-2', '{}', ?)`,
		actorID, actorID, Now(),
	)
	if err != nil {
		t.Fatalf("insert actor-scoped audit event after migration: %v", err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if newID <= preMigrationID {
		t.Errorf("new row id = %d, want it to continue past the copied row's id %d (AUTOINCREMENT sequence must survive the rebuild)", newID, preMigrationID)
	}

	// A row with both or neither set must be rejected by the CHECK.
	if _, err := db.Exec(
		`INSERT INTO audit_events(entity_id, target_actor_id, actor_id, event_type, correlation_id, changes, created_at) VALUES (?, ?, ?, 'x', 'corr-3', '{}', ?)`,
		entityID, actorID, actorID, Now(),
	); err == nil {
		t.Error("insert with both entity_id and target_actor_id set: want CHECK violation, got nil")
	}
	if _, err := db.Exec(
		`INSERT INTO audit_events(actor_id, event_type, correlation_id, changes, created_at) VALUES (?, 'x', 'corr-4', '{}', ?)`,
		actorID, Now(),
	); err == nil {
		t.Error("insert with neither entity_id nor target_actor_id set: want CHECK violation, got nil")
	}
}

// applyMigrationsThrough applies every embedded migration with
// version <= maxVersion, in order, skipping any already applied
// (tracked the same way Store.migrate does) — a minimal reimplementation
// scoped to this test file so it can stop partway through the real
// migration sequence, which Store.migrate itself never needs to do.
func applyMigrationsThrough(t *testing.T, db *sql.DB, maxVersion int) {
	t.Helper()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("bootstrap schema_migrations: %v", err)
	}

	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(entries)

	for _, name := range entries {
		version, err := migrationVersion(name)
		if err != nil {
			t.Fatalf("migrationVersion(%q): %v", name, err)
		}
		if version > maxVersion {
			continue
		}
		var already int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, version).Scan(&already); err != nil {
			t.Fatalf("check migration %d: %v", version, err)
		}
		if already > 0 {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, version, Now()); err != nil {
			t.Fatalf("record migration %d: %v", version, err)
		}
	}
}
