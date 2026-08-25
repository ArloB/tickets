package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/store"
)

// runExport is `tickets export --output FILE` (product spec §7.3, §12
// — a top-level command, not under `admin`, matching the plan's own
// naming). Local, direct-to-store, the same trust boundary `tickets
// setup`/`admin` commands use, since export reads a data directory
// rather than talking to a running server. See internal/backup.Export
// and Envelope's doc comments for exactly what is and isn't included.
func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	output := fs.String("output", "", "file to write the export JSON into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("export: --output is required")
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

	env, err := backup.Export(context.Background(), st.DB())
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("export: marshal: %w", err)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return fmt.Errorf("export: write %s: %w", *output, err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "exported %d project(s), %d ticket(s) to %s\n", len(env.Projects), len(env.Tickets), *output)
	return nil
}
