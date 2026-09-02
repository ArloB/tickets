package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/store"
)

// runAdminIntegrity is `tickets admin integrity` (Phase 6 Step 3):
// PRAGMA integrity_check + PRAGMA foreign_key_check + a blobstore
// verify/orphan sweep, in one report. Orphaned blobs (ADR 0007's open
// item) are reported but only ever deleted with --gc — an operator
// action, not an automatic background one, matching this codebase's
// existing "reasonable future addition ... isn't built speculatively"
// stance on GC. A corrupted blob is reported but never auto-removed
// by --gc even then: content that fails its own checksum might still
// be partially recoverable, which an orphan (correct bytes, just
// unreferenced) never needs to be.
//
// Only genuine data-integrity findings (a failed PRAGMA check, a
// foreign-key violation, a corrupted blob) make this command exit
// non-zero — an orphan report without --gc is informational, the same
// distinction `tickets admin integrity` draws between "something is
// wrong" and "here's some reclaimable disk space."
func runAdminIntegrity(args []string) error {
	fs := flag.NewFlagSet("admin integrity", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database and blob store (defaults to the same resolution `tickets server` uses)")
	gc := fs.Bool("gc", false, "delete orphaned blobs found by this check (blobs written within the last hour are left alone even if orphaned, since they may just be mid-upload)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a human-readable report")
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

	blobs, err := blobstore.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open blob store at %s: %w", cfg.DataDir, err)
	}

	ctx := context.Background()
	report, err := backup.BuildIntegrityReport(ctx, st, blobs, *gc)
	if err != nil {
		return err
	}

	if *jsonOut {
		if err := writeJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		printIntegrityReport(os.Stdout, report)
	}

	if !report.DatabaseOK || len(report.ForeignKeyViolations) > 0 || len(report.CorruptedBlobs) > 0 || len(report.RemoveErrors) > 0 {
		return fmt.Errorf("integrity check found problems; see report above")
	}
	return nil
}

func printIntegrityReport(w *os.File, r backup.IntegrityReport) {
	if r.DatabaseOK {
		_, _ = fmt.Fprintln(w, "database: ok")
	} else {
		_, _ = fmt.Fprintln(w, "database: PROBLEMS FOUND")
		for _, m := range r.DatabaseMessages {
			_, _ = fmt.Fprintf(w, "  %s\n", m)
		}
	}

	if len(r.ForeignKeyViolations) == 0 {
		_, _ = fmt.Fprintln(w, "foreign keys: ok")
	} else {
		_, _ = fmt.Fprintln(w, "foreign keys: PROBLEMS FOUND")
		for _, v := range r.ForeignKeyViolations {
			_, _ = fmt.Fprintf(w, "  %s -> %s\n", v.Table, v.ParentTable)
		}
	}

	if len(r.CorruptedBlobs) == 0 {
		_, _ = fmt.Fprintln(w, "blob checksums: ok")
	} else {
		_, _ = fmt.Fprintln(w, "blob checksums: PROBLEMS FOUND")
		for _, c := range r.CorruptedBlobs {
			_, _ = fmt.Fprintf(w, "  %s: %s\n", c.Hash, c.Error)
		}
	}

	_, _ = fmt.Fprintf(w, "orphaned blobs: %d\n", len(r.OrphanedBlobs))
	for _, h := range r.OrphanedBlobs {
		_, _ = fmt.Fprintf(w, "  %s\n", h)
	}
	if len(r.RemovedBlobs) > 0 {
		_, _ = fmt.Fprintf(w, "removed (--gc): %d\n", len(r.RemovedBlobs))
	}
	for _, e := range r.RemoveErrors {
		_, _ = fmt.Fprintf(w, "  remove failed: %s\n", e)
	}
}
