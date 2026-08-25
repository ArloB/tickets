package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/config"
)

// runAdminBackup is `tickets admin backup --output DIR` (Phase 6 Step
// 4, product spec §12): an online snapshot of the database and blob
// store, safe to run against a live server. See internal/backup.Backup
// for the ordering guarantee (database first, then blobs) that makes
// this safe under concurrent writes.
func runAdminBackup(args []string) error {
	fs := flag.NewFlagSet("admin backup", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database and blob store (defaults to the same resolution `tickets server` uses)")
	output := fs.String("output", "", "directory to write the backup into; must not already exist")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("admin backup: --output is required")
	}

	var cfgArgs []string
	if *dataDir != "" {
		cfgArgs = []string{"--data-dir", *dataDir}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	manifest, err := backup.Backup(context.Background(), cfg.DataDir, *output)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "backed up %d file(s) to %s (schema version %d)\n",
		len(manifest.Files), *output, manifest.SchemaVersion)
	return nil
}

// runAdminRestore is `tickets admin restore --input DIR` (Phase 6 Step
// 4). The server must not be running against --data-dir — see
// internal/backup.Restore's doc comment for what that precondition
// checks and its known limitation (a stale pidfile after a crash,
// worked around with --force).
func runAdminRestore(args []string) error {
	fs := flag.NewFlagSet("admin restore", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database and blob store (defaults to the same resolution `tickets server` uses)")
	input := fs.String("input", "", "backup directory produced by `tickets admin backup`")
	force := fs.Bool("force", false, "skip the running-server check (only safe if tickets.pid is stale, left behind by a crash)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("admin restore: --input is required")
	}

	var cfgArgs []string
	if *dataDir != "" {
		cfgArgs = []string{"--data-dir", *dataDir}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := backup.Restore(context.Background(), cfg.DataDir, *input, *force); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "restored %s from %s\n", cfg.DataDir, *input)
	return nil
}
