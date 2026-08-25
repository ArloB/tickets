package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/store"
)

// runAdmin is `tickets admin <subcommand>`: maintenance operations an
// operator runs directly against the data directory, not through the
// HTTP API — the same "open internal/store directly" pattern
// runSetup/runServer use, since these commands exist specifically for
// when there may be no running server to call. `agent`/`token` (see
// admin_agent.go) follow the same pattern for a different reason: see
// runAdminAgent's doc comment.
func runAdmin(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("admin: expected a subcommand (purge-idempotency-keys, search-reindex, integrity, backup, restore, agent, token)")
	}
	switch args[0] {
	case "purge-idempotency-keys":
		return runAdminPurgeIdempotencyKeys(args[1:])
	case "search-reindex":
		return runAdminSearchReindex(args[1:])
	case "integrity":
		return runAdminIntegrity(args[1:])
	case "backup":
		return runAdminBackup(args[1:])
	case "restore":
		return runAdminRestore(args[1:])
	case "agent":
		return runAdminAgent(args[1:])
	case "token":
		return runAdminToken(args[1:])
	default:
		return fmt.Errorf("admin: unknown subcommand %q", args[0])
	}
}

// runAdminSearchReindex clears and rebuilds the search index from
// scratch (store.RebuildSearchIndex) — the documented recovery path
// for anything the incremental UpsertSearchDocument call sites miss
// or get wrong (Phase 5 Step 6, ADR 0018).
func runAdminSearchReindex(args []string) error {
	fs := flag.NewFlagSet("admin search-reindex", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var cfgArgs []string
	if *dataDir != "" {
		cfgArgs = []string{"--data-dir", *dataDir}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store at %s: %w", cfg.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	tx, err := st.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	count, err := store.RebuildSearchIndex(context.Background(), tx)
	if err != nil {
		return fmt.Errorf("rebuild search index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true

	_, _ = fmt.Fprintf(os.Stdout, "reindexed %d search document(s)\n", count)
	return nil
}

// runAdminPurgeIdempotencyKeys closes the gap ADR 0008 flagged from
// the start: idempotency_keys retention was unbounded until this
// command existed to call the already-built but never-invoked
// store.PurgeIdempotencyKeysOlderThan. Default 720h (30 days) matches
// docs/contracts/concurrency.md's stated bounded-retention intent.
func runAdminPurgeIdempotencyKeys(args []string) error {
	fs := flag.NewFlagSet("admin purge-idempotency-keys", flag.ContinueOnError)
	olderThan := fs.Duration("older-than", 720*time.Hour, "delete idempotency keys created before this long ago")
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var cfgArgs []string
	if *dataDir != "" {
		cfgArgs = []string{"--data-dir", *dataDir}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store at %s: %w", cfg.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	cutoff := time.Now().UTC().Add(-*olderThan).Format(store.TimeLayout)
	purged, err := store.PurgeIdempotencyKeysOlderThan(context.Background(), st.DB(), cutoff)
	if err != nil {
		return fmt.Errorf("purge idempotency keys: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "purged %d idempotency key(s) older than %s\n", purged, olderThan)
	return nil
}
