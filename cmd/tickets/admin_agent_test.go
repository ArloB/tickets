package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAgentCreateGetList(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	captureStdout(t, func() {
		if err := runSetup([]string{"--username", "arlo", "--password", "hunter2hunter2", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runSetup: %v", err)
		}
	})

	createOut := captureStdout(t, func() {
		if err := runAdmin([]string{
			"agent", "create", "--name", "codex-1", "--description", "CI agent",
			"--as", "arlo", "--data-dir", dataDir, "--json",
		}); err != nil {
			t.Fatalf("runAdmin agent create: %v", err)
		}
	})
	var created map[string]any
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("decode admin agent create --json output: %v (raw: %s)", err, createOut)
	}
	if created["name"] != "codex-1" || created["description"] != "CI agent" {
		t.Errorf("admin agent create output = %v, want name=codex-1 description=%q", created, "CI agent")
	}
	if created["owner"] != "human:arlo" {
		t.Errorf("admin agent create output owner = %v, want %q", created["owner"], "human:arlo")
	}

	getOut := captureStdout(t, func() {
		if err := runAdmin([]string{"agent", "get", "codex-1", "--data-dir", dataDir, "--json"}); err != nil {
			t.Fatalf("runAdmin agent get: %v", err)
		}
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(getOut), &got); err != nil {
		t.Fatalf("decode admin agent get --json output: %v (raw: %s)", err, getOut)
	}
	if got["name"] != "codex-1" {
		t.Errorf("admin agent get output = %v, want name=codex-1", got)
	}

	listOut := captureStdout(t, func() {
		if err := runAdmin([]string{"agent", "list", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runAdmin agent list: %v", err)
		}
	})
	if !strings.Contains(listOut, "codex-1") {
		t.Errorf("admin agent list output = %q, want it to contain codex-1", listOut)
	}
}

func TestAdminAgentCreateRequiresNameAndAs(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	if err := runAdmin([]string{"agent", "create", "--as", "system:system", "--data-dir", dataDir}); err == nil {
		t.Error("admin agent create with no --name: want error, got nil")
	}
	if err := runAdmin([]string{"agent", "create", "--name", "x", "--data-dir", dataDir}); err == nil {
		t.Error("admin agent create with no --as: want error, got nil")
	}
}

// TestAdminTokenCreateListRevoke exercises the full token lifecycle:
// creation returns the raw token exactly once, list shows it active,
// and revoke flips its status without a second raw token ever
// appearing.
func TestAdminTokenCreateListRevoke(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	captureStdout(t, func() {
		if err := runAdmin([]string{
			"agent", "create", "--name", "codex-1", "--as", "system:system", "--data-dir", dataDir,
		}); err != nil {
			t.Fatalf("runAdmin agent create: %v", err)
		}
	})

	createOut := captureStdout(t, func() {
		if err := runAdmin([]string{
			"token", "create", "codex-1", "--description", "laptop", "--as", "system:system",
			"--data-dir", dataDir, "--json",
		}); err != nil {
			t.Fatalf("runAdmin token create: %v", err)
		}
	})
	var created map[string]any
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("decode admin token create --json output: %v (raw: %s)", err, createOut)
	}
	rawToken, _ := created["token"].(string)
	if rawToken == "" {
		t.Fatalf("admin token create output = %v, want a non-empty raw token", created)
	}
	tokenID, ok := created["id"].(float64)
	if !ok || tokenID <= 0 {
		t.Fatalf("admin token create output id = %v, want a positive id", created["id"])
	}

	listOut := captureStdout(t, func() {
		if err := runAdmin([]string{"token", "list", "codex-1", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runAdmin token list: %v", err)
		}
	})
	if !strings.Contains(listOut, "active") || !strings.Contains(listOut, "laptop") {
		t.Errorf("admin token list output = %q, want it to show the token as active with its description", listOut)
	}
	if strings.Contains(listOut, rawToken) {
		t.Errorf("admin token list output leaked the raw token value: %q", listOut)
	}

	revokeOut := captureStdout(t, func() {
		if err := runAdmin([]string{
			"token", "revoke", "1", "--as", "system:system", "--data-dir", dataDir,
		}); err != nil {
			t.Fatalf("runAdmin token revoke: %v", err)
		}
	})
	if !strings.Contains(revokeOut, "revoked") {
		t.Errorf("admin token revoke output = %q, want it to confirm revocation", revokeOut)
	}

	listAfterOut := captureStdout(t, func() {
		if err := runAdmin([]string{"token", "list", "codex-1", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runAdmin token list after revoke: %v", err)
		}
	})
	if !strings.Contains(listAfterOut, "revoked") {
		t.Errorf("admin token list output after revoke = %q, want it to show the token as revoked", listAfterOut)
	}
}

// TestAdminTokenRevokeRejectsUnknownID proves revoking a nonexistent
// token id fails rather than reporting a false "revoked" — the store
// layer's UPDATE is deliberately idempotent for a real token, which
// means it can't tell "already revoked" from "never existed" on its
// own.
func TestAdminTokenRevokeRejectsUnknownID(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	if err := runAdmin([]string{"token", "revoke", "999", "--as", "system:system", "--data-dir", dataDir}); err == nil {
		t.Error("admin token revoke with a nonexistent token id: want error, got nil")
	}
}

// TestAdminAgentCreateRejectsAgentAsActor proves --as rejects an agent
// actor ref: product spec §4.1 says a human creates agent identities,
// so an agent creating another agent would quietly break that model.
func TestAdminAgentCreateRejectsAgentAsActor(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	if err := runAdmin([]string{
		"agent", "create", "--name", "codex-2", "--as", "agent:codex-1", "--data-dir", dataDir,
	}); err == nil {
		t.Error("admin agent create with --as agent:codex-1: want error, got nil")
	}
}

func TestAdminTokenCreateRequiresAgentNameAndAs(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	if err := runAdmin([]string{"token", "create", "--as", "system:system", "--data-dir", dataDir}); err == nil {
		t.Error("admin token create with no leading agent name: want error, got nil")
	}
	if err := runAdmin([]string{"token", "create", "codex-1", "--data-dir", dataDir}); err == nil {
		t.Error("admin token create with no --as: want error, got nil")
	}
}

func TestAdminAgentRequiresSubcommand(t *testing.T) {
	if err := runAdminAgent(nil); err == nil {
		t.Error("runAdminAgent with no subcommand: want error, got nil")
	}
	if err := runAdminAgent([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runAdminAgent with an unknown subcommand: want error, got nil")
	}
}

func TestAdminTokenRequiresSubcommand(t *testing.T) {
	if err := runAdminToken(nil); err == nil {
		t.Error("runAdminToken with no subcommand: want error, got nil")
	}
	if err := runAdminToken([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runAdminToken with an unknown subcommand: want error, got nil")
	}
}
