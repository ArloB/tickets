package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// TestExportThenImportRoundTrip is a thin wiring test — internal/backup
// owns the real coverage. This confirms tickets export/import's flags
// (--output, --input, --commit) reach it correctly and that import
// defaults to a dry run.
func TestExportThenImportRoundTrip(t *testing.T) {
	srcDir := filepath.Join(t.TempDir(), "src")
	st, err := store.Open(srcDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	blobs, err := blobstore.Open(srcDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)
	actor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
	ctx := context.Background()
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, actor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	_ = st.Close()

	exportFile := filepath.Join(t.TempDir(), "export.json")
	out := captureStdout(t, func() {
		if err := runExport([]string{"--data-dir", srcDir, "--output", exportFile}); err != nil {
			t.Fatalf("runExport: %v", err)
		}
	})
	if !strings.Contains(out, "exported") {
		t.Errorf("export output = %q, want a summary line", out)
	}

	dstDir := filepath.Join(t.TempDir(), "dst")
	dryOut := captureStdout(t, func() {
		if err := runImport([]string{"--data-dir", dstDir, "--input", exportFile}); err != nil {
			t.Fatalf("runImport (dry run): %v", err)
		}
	})
	if !strings.Contains(dryOut, "dry run") {
		t.Errorf("import output without --commit = %q, want it to say dry run", dryOut)
	}

	commitOut := captureStdout(t, func() {
		if err := runImport([]string{"--data-dir", dstDir, "--input", exportFile, "--commit"}); err != nil {
			t.Fatalf("runImport --commit: %v", err)
		}
	})
	if !strings.Contains(commitOut, "committed") {
		t.Errorf("import --commit output = %q, want it to say committed", commitOut)
	}

	dstSt, err := store.Open(dstDir)
	if err != nil {
		t.Fatalf("reopen dst store: %v", err)
	}
	defer func() { _ = dstSt.Close() }()
	dstSvc := service.New(dstSt, mustOpenTestBlobs(t, dstDir))
	if _, err := dstSvc.GetProject(ctx, "ABC"); err != nil {
		t.Fatalf("imported project not found: %v", err)
	}
}

func TestExportRequiresOutputFlag(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := runExport([]string{"--data-dir", dataDir}); err == nil {
		t.Fatal("export without --output: want an error, got nil")
	}
}

func TestImportRequiresInputFlag(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := runImport([]string{"--data-dir", dataDir}); err == nil {
		t.Fatal("import without --input: want an error, got nil")
	}
}

func mustOpenTestBlobs(t *testing.T, dataDir string) *blobstore.Store {
	t.Helper()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	return blobs
}
