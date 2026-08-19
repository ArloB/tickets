package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store owns the single *sql.DB for the process. No other package opens
// the database directly (product spec §8.3, package doc.go).
type Store struct {
	db *sql.DB
}

// Open creates dataDir if needed, opens the SQLite database inside it,
// applies pragmas, and runs any pending migrations. See ADR 0003 for
// why busy_timeout is set via the DSN rather than a post-open PRAGMA:
// database/sql pools connections, and a pragma applied as a follow-up
// statement can land on a different pooled connection than the one a
// later statement uses.
//
// _txlock=immediate (see ADR 0009's implementation note) makes every
// read-write transaction issue BEGIN IMMEDIATE instead of the driver's
// default BEGIN DEFERRED. A deferred transaction takes no lock until
// its first write, so two goroutines can both finish their reads (e.g.
// internal/service's idempotency check, or the reference-counter read)
// before either writes — and the loser fails fast with SQLITE_BUSY
// instead of simply waiting its turn behind busy_timeout. BEGIN
// IMMEDIATE takes the write lock up front, so concurrent writers
// genuinely serialize through the busy_timeout wait rather than racing.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "tickets.db")
	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dbPath, err)
	}

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return s, nil
}

// DB returns the underlying connection pool for internal/service and
// internal/store's own query functions. It is not exported outside
// this module's internal tree.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }

// Ping confirms the database is actually reachable, for /readyz
// (product spec §9: readiness must reflect DB/storage state, not just
// process liveness).
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// migrationVersion parses the leading integer from a migration file's
// base name (e.g. "migrations/0002_core_domain.sql" -> 2). Versions
// come from the filename, not the sorted position in the glob result:
// deriving version from position meant inserting a file that happened
// to sort before an already-applied one would silently re-map every
// later version's identity in schema_migrations.
func migrationVersion(path string) (int, error) {
	base := filepath.Base(path)
	digits, _, found := strings.Cut(base, "_")
	if !found {
		return 0, fmt.Errorf("migration filename %q has no _ separator after its version prefix", base)
	}
	version, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("migration filename %q has a non-numeric version prefix: %w", base, err)
	}
	return version, nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("bootstrap schema_migrations: %w", err)
	}

	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)

	for _, name := range entries {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		var already int
		if err := s.db.QueryRowContext(ctx,
			`SELECT count(*) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&already); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if already > 0 {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC().Format(TimeLayout),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
	}
	return nil
}
