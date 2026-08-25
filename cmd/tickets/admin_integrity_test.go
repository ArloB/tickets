package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAdminIntegrityOnFreshDatabaseReportsClean confirms the golden
// path: a brand-new data directory has no problems and no orphans,
// and the command exits successfully.
func TestAdminIntegrityOnFreshDatabaseReportsClean(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	var out string
	var runErr error
	out = captureStdout(t, func() {
		runErr = runAdmin([]string{"integrity", "--data-dir", dataDir, "--json"})
	})
	if runErr != nil {
		t.Fatalf("runAdmin integrity: %v", runErr)
	}

	var report struct {
		DatabaseOK           bool     `json:"database_ok"`
		ForeignKeyViolations []any    `json:"foreign_key_violations"`
		CorruptedBlobs       []any    `json:"corrupted_blobs"`
		OrphanedBlobs        []string `json:"orphaned_blobs"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal report: %v (raw: %s)", err, out)
	}
	if !report.DatabaseOK {
		t.Error("DatabaseOK = false, want true on a fresh database")
	}
	if len(report.ForeignKeyViolations) != 0 || len(report.CorruptedBlobs) != 0 || len(report.OrphanedBlobs) != 0 {
		t.Errorf("report = %+v, want no findings", report)
	}
}

// TestAdminIntegrityFindsAndGCsOrphanedBlob is this command's core
// regression test: a blob file with no referencing database row is
// detected as an orphan, left alone without --gc, and removed with
// it — closing ADR 0007's open item as an operator action.
func TestAdminIntegrityFindsAndGCsOrphanedBlob(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	// Opening once first creates the data dir/blobs dir/database so
	// the manufactured orphan below has somewhere to live.
	if err := runAdmin([]string{"integrity", "--data-dir", dataDir}); err != nil {
		t.Fatalf("initial runAdmin integrity: %v", err)
	}

	content := []byte("orphan bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	shardDir := filepath.Join(dataDir, "blobs", hash[:2])
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		t.Fatalf("create shard dir: %v", err)
	}
	blobPath := filepath.Join(shardDir, hash)
	if err := os.WriteFile(blobPath, content, 0o600); err != nil {
		t.Fatalf("write orphan blob: %v", err)
	}
	// Backdate past gcMinOrphanAge: a freshly-written orphan is left
	// alone by --gc (see TestAdminIntegrityGCLeavesRecentOrphanAlone)
	// since it may just be mid-upload, so this test's orphan needs to
	// look old enough to safely collect.
	old := time.Now().Add(-2 * gcMinOrphanAge)
	if err := os.Chtimes(blobPath, old, old); err != nil {
		t.Fatalf("backdate orphan blob mtime: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runAdmin([]string{"integrity", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runAdmin integrity (no --gc): %v", err)
		}
	})
	if !strings.Contains(out, hash) {
		t.Fatalf("integrity output = %q, want it to name the orphaned blob %q", out, hash)
	}
	if _, err := os.Stat(filepath.Join(shardDir, hash)); err != nil {
		t.Fatalf("orphan blob removed without --gc: %v", err)
	}

	gcOut := captureStdout(t, func() {
		if err := runAdmin([]string{"integrity", "--data-dir", dataDir, "--gc"}); err != nil {
			t.Fatalf("runAdmin integrity --gc: %v", err)
		}
	})
	if !strings.Contains(gcOut, hash) {
		t.Errorf("integrity --gc output = %q, want it to name the removed blob", gcOut)
	}
	if _, err := os.Stat(filepath.Join(shardDir, hash)); !os.IsNotExist(err) {
		t.Errorf("orphan blob still present after --gc: %v", err)
	}
}

// TestAdminIntegrityGCLeavesRecentOrphanAlone confirms --gc skips a
// blob written within gcMinOrphanAge: CreateAttachment's
// blobstore.Put happens before its enclosing transaction commits (ADR
// 0007's Consequences), so a blob that's merely mid-upload looks
// identical to a genuine orphan for the seconds between Put and
// commit — --gc must never delete out from under a concurrent upload.
func TestAdminIntegrityGCLeavesRecentOrphanAlone(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := runAdmin([]string{"integrity", "--data-dir", dataDir}); err != nil {
		t.Fatalf("initial runAdmin integrity: %v", err)
	}

	content := []byte("freshly written bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	shardDir := filepath.Join(dataDir, "blobs", hash[:2])
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		t.Fatalf("create shard dir: %v", err)
	}
	blobPath := filepath.Join(shardDir, hash)
	if err := os.WriteFile(blobPath, content, 0o600); err != nil {
		t.Fatalf("write orphan blob: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runAdmin([]string{"integrity", "--data-dir", dataDir, "--gc"}); err != nil {
			t.Fatalf("runAdmin integrity --gc: %v", err)
		}
	})
	if !strings.Contains(out, hash) {
		t.Errorf("integrity --gc output = %q, want it to still report the recent orphan", out)
	}
	if _, err := os.Stat(blobPath); err != nil {
		t.Errorf("recently-written orphan blob removed by --gc, want it left in place: %v", err)
	}
}

// TestAdminIntegrityDetectsCorruptedBlobAndFailsExitCode confirms a
// corrupted blob is reported, survives --gc (corruption is never
// auto-removed, unlike a genuine orphan), and makes the command
// return a non-nil error — the "genuine problem" exit path, distinct
// from the informational orphan-report case.
func TestAdminIntegrityDetectsCorruptedBlobAndFailsExitCode(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := runAdmin([]string{"integrity", "--data-dir", dataDir}); err != nil {
		t.Fatalf("initial runAdmin integrity: %v", err)
	}

	// A filename that is itself a well-formed hash, but whose content
	// doesn't hash to it — corruption, not an unreferenced-but-valid
	// orphan.
	fakeHash := strings.Repeat("ab", 32)
	shardDir := filepath.Join(dataDir, "blobs", fakeHash[:2])
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		t.Fatalf("create shard dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shardDir, fakeHash), []byte("mismatched content"), 0o600); err != nil {
		t.Fatalf("write corrupted blob: %v", err)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runAdmin([]string{"integrity", "--data-dir", dataDir, "--gc"})
	})
	if runErr == nil {
		t.Fatal("runAdmin integrity with a corrupted blob: want a non-nil error, got nil")
	}
	if !strings.Contains(out, fakeHash) {
		t.Errorf("integrity output = %q, want it to name the corrupted blob", out)
	}
	if _, err := os.Stat(filepath.Join(shardDir, fakeHash)); err != nil {
		t.Errorf("corrupted blob was removed by --gc, want it left in place: %v", err)
	}
}
