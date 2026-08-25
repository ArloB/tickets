package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAdminBackupThenRestoreRoundTrip is a thin wiring test over
// internal/backup, which owns the real behavior coverage — this just
// confirms the CLI flags reach it correctly (--data-dir/--output on
// backup, --data-dir/--input on restore).
func TestAdminBackupThenRestoreRoundTrip(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	// tickets project create needs a store; reuse admin integrity's
	// side effect of opening one to create the data dir/database.
	if err := runAdmin([]string{"integrity", "--data-dir", dataDir}); err != nil {
		t.Fatalf("initial runAdmin integrity: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "backup")
	out := captureStdout(t, func() {
		if err := runAdmin([]string{"backup", "--data-dir", dataDir, "--output", outputDir}); err != nil {
			t.Fatalf("runAdmin backup: %v", err)
		}
	})
	if !strings.Contains(out, "backed up") {
		t.Errorf("backup output = %q, want a summary line", out)
	}

	restoreOut := captureStdout(t, func() {
		if err := runAdmin([]string{"restore", "--data-dir", dataDir, "--input", outputDir}); err != nil {
			t.Fatalf("runAdmin restore: %v", err)
		}
	})
	if !strings.Contains(restoreOut, "restored") {
		t.Errorf("restore output = %q, want a summary line", restoreOut)
	}
}

func TestAdminBackupRequiresOutputFlag(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := runAdmin([]string{"backup", "--data-dir", dataDir}); err == nil {
		t.Fatal("admin backup without --output: want an error, got nil")
	}
}

func TestAdminRestoreRequiresInputFlag(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := runAdmin([]string{"restore", "--data-dir", dataDir}); err == nil {
		t.Fatal("admin restore without --input: want an error, got nil")
	}
}
