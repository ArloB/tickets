package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// what was written to it — setup_test.go's way of confirming runSetup
// never echoes the password it was given (product spec §10).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestSetupCreatesAdminAccount(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSetup([]string{"--username", "admin", "--password", "hunter22-secret", "--data-dir", dataDir})
	})
	if runErr != nil {
		t.Fatalf("runSetup: %v", runErr)
	}
	if !strings.Contains(out, "admin") {
		t.Errorf("setup output = %q, want it to mention the created username", out)
	}
	if strings.Contains(out, "hunter22-secret") {
		t.Errorf("setup output leaked the password: %q", out)
	}
}

func TestSetupRefusesSecondRun(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	_ = captureStdout(t, func() {
		if err := runSetup([]string{"--username", "admin", "--password", "hunter22-secret", "--data-dir", dataDir}); err != nil {
			t.Fatalf("first runSetup: %v", err)
		}
	})

	err := runSetup([]string{"--username", "someone-else", "--password", "another-password", "--data-dir", dataDir})
	if err == nil {
		t.Fatal("second runSetup (an admin already exists): want error, got nil")
	}
}

func TestSetupRequiresUsernameAndPassword(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	if err := runSetup([]string{"--password", "x", "--data-dir", dataDir}); err == nil {
		t.Error("runSetup with no --username: want error, got nil")
	}
	if err := runSetup([]string{"--username", "admin", "--data-dir", dataDir}); err == nil {
		t.Error("runSetup with no --password: want error, got nil")
	}
}

func TestSetupReadsCredentialsFromEnv(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("TICKETS_ADMIN_USERNAME", "env-admin")
	t.Setenv("TICKETS_ADMIN_PASSWORD", "env-password-secret")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSetup([]string{"--data-dir", dataDir})
	})
	if runErr != nil {
		t.Fatalf("runSetup: %v", runErr)
	}
	if !strings.Contains(out, "env-admin") {
		t.Errorf("setup output = %q, want it to mention env-admin", out)
	}
	if strings.Contains(out, "env-password-secret") {
		t.Errorf("setup output leaked the password: %q", out)
	}
}
