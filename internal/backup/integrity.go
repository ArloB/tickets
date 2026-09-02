package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/store"
)

const GCMinOrphanAge = time.Hour

type IntegrityReport struct {
	DatabaseOK           bool                     `json:"database_ok"`
	DatabaseMessages     []string                 `json:"database_messages,omitempty"`
	ForeignKeyViolations []IntegrityFKViolation   `json:"foreign_key_violations"`
	CorruptedBlobs       []IntegrityCorruptedBlob `json:"corrupted_blobs"`
	OrphanedBlobs        []string                 `json:"orphaned_blobs"`
	RemovedBlobs         []string                 `json:"removed_blobs,omitempty"`
	RemoveErrors         []string                 `json:"remove_errors,omitempty"`
}

type IntegrityFKViolation struct {
	Table       string `json:"table"`
	RowID       *int64 `json:"row_id"`
	ParentTable string `json:"parent_table"`
}

type IntegrityCorruptedBlob struct {
	Hash  string `json:"hash"`
	Error string `json:"error"`
}

func BuildIntegrityReport(ctx context.Context, st *store.Store, blobs *blobstore.Store, gc bool) (IntegrityReport, error) {
	ok, messages, err := store.IntegrityCheck(ctx, st.DB())
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("integrity check: %w", err)
	}

	fkViolations, err := store.ForeignKeyCheck(ctx, st.DB())
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("foreign key check: %w", err)
	}
	violations := make([]IntegrityFKViolation, len(fkViolations))
	for i, v := range fkViolations {
		violations[i] = IntegrityFKViolation{Table: v.Table, RowID: v.RowID, ParentTable: v.ParentTable}
	}

	referenced, err := store.ListReferencedBlobHashes(ctx, st.DB())
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("list referenced blob hashes: %w", err)
	}
	verifyResults, err := blobs.Verify()
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("verify blobs: %w", err)
	}

	report := IntegrityReport{
		DatabaseOK:           ok,
		ForeignKeyViolations: violations,
		CorruptedBlobs:       []IntegrityCorruptedBlob{},
		OrphanedBlobs:        []string{},
	}
	if !ok {
		report.DatabaseMessages = messages
	}

	orphans := []string{}
	for _, r := range verifyResults {
		if r.Err != nil {
			report.CorruptedBlobs = append(report.CorruptedBlobs, IntegrityCorruptedBlob{Hash: r.Hash, Error: r.Err.Error()})
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
			if time.Since(modTime) < GCMinOrphanAge {
				continue
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
