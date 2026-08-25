package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/store"
)

// runImport is `tickets import --input FILE [--attachments DIR]
// [--commit]` (product spec §7.3, §12). Dry run is the default
// posture: without --commit, nothing is written, and the printed
// report is exactly what a --commit run would attempt — the same
// validation runs either way (internal/backup.Import), so a dry run
// is not an approximation of the real thing.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	input := fs.String("input", "", "export JSON file produced by `tickets export`")
	attachments := fs.String("attachments", "", "directory produced by `tickets export --attachments`; required if the export references any attachment content")
	commit := fs.Bool("commit", false, "actually write the import; without this flag, only a validation report is produced")
	jsonOut := fs.Bool("json", false, "print the report as JSON instead of human-readable text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("import: --input is required")
	}

	var cfgArgs []string
	if *dataDir != "" {
		cfgArgs = []string{"--data-dir", *dataDir}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		return fmt.Errorf("import: read %s: %w", *input, err)
	}
	var env backup.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("import: parse %s: %w", *input, err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store at %s: %w", cfg.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	blobs, err := blobstore.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open blob store at %s: %w", cfg.DataDir, err)
	}

	report, err := backup.Import(context.Background(), st.DB(), env, *attachments, blobs, *commit)
	if err != nil {
		return err
	}

	if *jsonOut {
		if err := writeJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		printImportReport(os.Stdout, report, *commit)
	}

	if len(report.Problems) > 0 {
		return fmt.Errorf("import: %d problem(s) found; see report above", len(report.Problems))
	}
	if *commit && !report.Committed {
		return fmt.Errorf("import: --commit was set but nothing was committed")
	}
	return nil
}

func printImportReport(w *os.File, r backup.ImportReport, commitRequested bool) {
	if r.Committed {
		_, _ = fmt.Fprintln(w, "import committed:")
	} else if commitRequested {
		_, _ = fmt.Fprintln(w, "import NOT committed (problems found):")
	} else {
		_, _ = fmt.Fprintln(w, "dry run (pass --commit to actually import):")
	}
	for _, table := range []string{"entities", "actors", "projects", "features", "tickets", "decisions", "content_items", "comments", "attachments", "external_links", "notifications"} {
		_, _ = fmt.Fprintf(w, "  %s: %d\n", table, r.Counts[table])
	}
	if len(r.Problems) == 0 {
		_, _ = fmt.Fprintln(w, "problems: none")
		return
	}
	_, _ = fmt.Fprintf(w, "problems: %d\n", len(r.Problems))
	for _, p := range r.Problems {
		_, _ = fmt.Fprintf(w, "  %s\n", p)
	}
}
