package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// preMigrationBackupDir is the subdirectory of a data directory that
// holds pre-migration snapshots — kept inside the data directory
// itself, alongside tickets.db, rather than a system temp directory,
// so it survives on the same disk/volume an operator is already
// responsible for backing up.
const preMigrationBackupDir = "backups"

// preMigrationBackupsKept bounds retention (product spec §10's
// pre-migration backup requirement doesn't specify a count; this
// generation errs conservative — a snapshot exists to be pulled out
// once, right after a bad upgrade, not accumulated indefinitely).
const preMigrationBackupsKept = 5

// backupBeforeMigration snapshots the database to
// <dataDir>/backups/pre-migration-v<fromVersion>-<timestamp>.db via
// SQLite's VACUUM INTO before migrate applies any pending migration
// (product spec §10: "create a pre-migration backup and fail safely
// if a migration cannot complete"). Called only when fromVersion > 0
// — see migrate's call site: a brand-new database (no schema_migrations
// row yet) has nothing to lose, so every store.Open(t.TempDir()) call
// across this codebase's test suite never pays this cost, and no
// separate disable-for-tests flag was needed to keep tests fast (the
// gate documented here does that on its own).
//
// VACUUM INTO refuses to overwrite an existing file, checkpoints WAL
// automatically, and produces a complete, valid, standalone database
// file — the same guarantee product spec §11's "online backup" wants,
// reused here for the pre-migration case. A failure here aborts the
// whole migrate call before any migration is applied, so "fails
// safely" holds: the caller sees an error and the database is
// untouched, rather than a migration racing ahead of a backup that
// never landed.
func backupBeforeMigration(ctx context.Context, db *sql.DB, dataDir string, fromVersion int) error {
	dir := filepath.Join(dataDir, preMigrationBackupDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create pre-migration backup dir: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("pre-migration-v%d-%s.db", fromVersion, time.Now().UTC().Format("20060102T150405.000000000Z")))
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("snapshot database to %s: %w", path, err)
	}

	// Retention pruning is best-effort: a stale backup file that can't
	// be removed (locked by another process, a permission change) must
	// never block startup when the snapshot that actually matters — the
	// one just taken above — succeeded. internal/store has no logger,
	// so this is swallowed with a stderr note rather than propagated.
	if err := pruneOldPreMigrationBackups(dir); err != nil {
		fmt.Fprintf(os.Stderr, "tickets: warning: prune old pre-migration backups: %v\n", err)
	}
	return nil
}

// pruneOldPreMigrationBackups keeps the most recent
// preMigrationBackupsKept snapshots in dir and removes the rest.
// Filenames sort lexically in chronological order (the timestamp
// component is fixed-width and zero-padded, same reasoning as
// TimeLayout), so a plain sort.Strings is enough — no need to parse
// each name back into a time.Time.
func pruneOldPreMigrationBackups(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "pre-migration-") && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) <= preMigrationBackupsKept {
		return nil
	}
	for _, name := range names[:len(names)-preMigrationBackupsKept] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}
