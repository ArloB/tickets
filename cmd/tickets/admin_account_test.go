package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAccountCreateListChangePassword(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	captureStdout(t, func() {
		if err := runSetup([]string{"--username", "arlo", "--password", "hunter2hunter2", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runSetup: %v", err)
		}
	})

	createOut := captureStdout(t, func() {
		if err := runAdmin([]string{
			"account", "create", "--username", "bob", "--password", "bobs-password-here",
			"--as", "arlo", "--data-dir", dataDir, "--json",
		}); err != nil {
			t.Fatalf("runAdmin account create: %v", err)
		}
	})
	var created map[string]any
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("decode admin account create --json output: %v (raw: %s)", err, createOut)
	}
	if created["username"] != "bob" || created["is_admin"] != false {
		t.Errorf("admin account create output = %v, want username=bob is_admin=false", created)
	}

	listOut := captureStdout(t, func() {
		if err := runAdmin([]string{"account", "list", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runAdmin account list: %v", err)
		}
	})
	if !strings.Contains(listOut, "arlo") || !strings.Contains(listOut, "bob") {
		t.Errorf("admin account list output = %q, want it to contain both arlo and bob", listOut)
	}

	changeOut := captureStdout(t, func() {
		if err := runAdmin([]string{
			"account", "change-password", "--username", "bob", "--new-password", "bobs-new-password",
			"--as", "arlo", "--data-dir", dataDir,
		}); err != nil {
			t.Fatalf("runAdmin account change-password: %v", err)
		}
	})
	if !strings.Contains(changeOut, "bob") {
		t.Errorf("admin account change-password output = %q, want it to mention bob", changeOut)
	}
}

func TestAdminAccountCreateRequiresUsernamePasswordAndAs(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	captureStdout(t, func() {
		if err := runSetup([]string{"--username", "arlo", "--password", "hunter2hunter2", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runSetup: %v", err)
		}
	})

	if err := runAdmin([]string{"account", "create", "--password", "x", "--as", "arlo", "--data-dir", dataDir}); err == nil {
		t.Error("admin account create with no --username: want an error, got nil")
	}
	if err := runAdmin([]string{"account", "create", "--username", "carol", "--as", "arlo", "--data-dir", dataDir}); err == nil {
		t.Error("admin account create with no --password: want an error, got nil")
	}
	if err := runAdmin([]string{"account", "create", "--username", "carol", "--password", "x", "--data-dir", dataDir}); err == nil {
		t.Error("admin account create with no --as: want an error, got nil")
	}
}

func TestAdminAccountRequiresSubcommand(t *testing.T) {
	if err := runAdmin([]string{"account"}); err == nil {
		t.Error("admin account with no subcommand: want an error, got nil")
	}
}
