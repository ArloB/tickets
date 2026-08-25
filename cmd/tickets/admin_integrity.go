package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/store"
)

// gcMinOrphanAge is how old an unreferenced blob must be before --gc
// will remove it. CreateAttachment's blobstore.Put runs before its
// enclosing transaction commits (ADR 0007's Consequences), so a blob
// written moments ago that looks orphaned may just be mid-upload —
// its attachments row hasn't committed yet, not that it never will.
// An hour is generous relative to that window (bounded by
// _txlock=immediate's 5s busy_timeout, not minutes) while still being
// negligible next to how long a genuine orphan sits unnoticed; ADR
// 0007's own reasoning is that orphans are harmless, not urgent, so
// there's no cost to erring conservative here.
const gcMinOrphanAge = time.Hour

// integrityReport is `tickets admin integrity`'s --json shape — every
// finding this command can produce, always present (empty
// slices/false rather than omitted fields) so a script doesn't need
// to distinguish "not checked" from "checked, found nothing."
type integrityReport struct {
	DatabaseOK           bool                           `json:"database_ok"`
	DatabaseMessages     []string                       `json:"database_messages,omitempty"`
	ForeignKeyViolations []integrityForeignKeyViolation `json:"foreign_key_violations"`
	CorruptedBlobs       []integrityCorruptedBlob       `json:"corrupted_blobs"`
	OrphanedBlobs        []string                       `json:"orphaned_blobs"`
	RemovedBlobs         []string                       `json:"removed_blobs,omitempty"`
	RemoveErrors         []string                       `json:"remove_errors,omitempty"`
}

type integrityForeignKeyViolation struct {
	Table       string `json:"table"`
	RowID       *int64 `json:"row_id"`
	ParentTable string `json:"parent_table"`
}

type integrityCorruptedBlob struct {
	Hash  string `json:"hash"`
	Error string `json:"error"`
}

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
	report, err := buildIntegrityReport(ctx, st, blobs, *gc)
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

func buildIntegrityReport(ctx context.Context, st *store.Store, blobs *blobstore.Store, gc bool) (integrityReport, error) {
	ok, messages, err := store.IntegrityCheck(ctx, st.DB())
	if err != nil {
		return integrityReport{}, fmt.Errorf("integrity check: %w", err)
	}

	fkViolations, err := store.ForeignKeyCheck(ctx, st.DB())
	if err != nil {
		return integrityReport{}, fmt.Errorf("foreign key check: %w", err)
	}
	violations := make([]integrityForeignKeyViolation, len(fkViolations))
	for i, v := range fkViolations {
		violations[i] = integrityForeignKeyViolation{Table: v.Table, RowID: v.RowID, ParentTable: v.ParentTable}
	}

	referenced, err := store.ListReferencedBlobHashes(ctx, st.DB())
	if err != nil {
		return integrityReport{}, fmt.Errorf("list referenced blob hashes: %w", err)
	}
	verifyResults, err := blobs.Verify()
	if err != nil {
		return integrityReport{}, fmt.Errorf("verify blobs: %w", err)
	}

	report := integrityReport{
		DatabaseOK:           ok,
		ForeignKeyViolations: violations,
		CorruptedBlobs:       []integrityCorruptedBlob{},
		OrphanedBlobs:        []string{},
	}
	if !ok {
		// messages is the literal ["ok"] singleton when ok is true —
		// no point surfacing that as a "message."
		report.DatabaseMessages = messages
	}

	orphans := []string{}
	for _, r := range verifyResults {
		if r.Err != nil {
			report.CorruptedBlobs = append(report.CorruptedBlobs, integrityCorruptedBlob{Hash: r.Hash, Error: r.Err.Error()})
			continue
		}
		if !referenced[r.Hash] {
			orphans = append(orphans, r.Hash)
		}
	}
	report.OrphanedBlobs = orphans

	if gc {
		for _, hash := range orphans {
			modTime, err := blobs.ModTime(hash)
			if err != nil {
				report.RemoveErrors = append(report.RemoveErrors, fmt.Sprintf("%s: %v", hash, err))
				continue
			}
			if time.Since(modTime) < gcMinOrphanAge {
				continue // too recent to safely distinguish from a mid-upload blob
			}
			if err := blobs.Remove(hash); err != nil {
				report.RemoveErrors = append(report.RemoveErrors, fmt.Sprintf("%s: %v", hash, err))
				continue
			}
			report.RemovedBlobs = append(report.RemovedBlobs, hash)
		}
	}

	return report, nil
}

func printIntegrityReport(w *os.File, r integrityReport) {
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
