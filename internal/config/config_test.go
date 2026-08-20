package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfigFile(t *testing.T, dir string, fc fileConfig) string {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("marshal file config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func strp(s string) *string { return &s }

// TestPrecedenceFlagBeatsEnvBeatsFileBeatsDefault exercises §7.3's
// documented order end to end, one setting (Port) at a time so a
// regression pinpoints exactly which layer stopped winning.
func TestPrecedenceFlagBeatsEnvBeatsFileBeatsDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TICKETS_CONFIG_FILE", writeConfigFile(t, dir, fileConfig{Port: strp("9001")}))

	// File only: file's value wins over the built-in default.
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "9001" {
		t.Errorf("Port = %q, want 9001 (from file)", cfg.Port)
	}

	// Env beats file.
	t.Setenv("TICKETS_PORT", "9002")
	cfg, err = Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "9002" {
		t.Errorf("Port = %q, want 9002 (env over file)", cfg.Port)
	}

	// Flag beats env.
	cfg, err = Load([]string{"--port", "9003"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "9003" {
		t.Errorf("Port = %q, want 9003 (flag over env)", cfg.Port)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TICKETS_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != "8080" {
		t.Errorf("defaults = %+v, want host 127.0.0.1 port 8080", cfg)
	}
	if cfg.LogFormat != "console" {
		t.Errorf("LogFormat = %q, want console", cfg.LogFormat)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
	if !cfg.AnonymousRead {
		t.Errorf("AnonymousRead = false, want true (loopback host, no explicit override)")
	}
}

// TestAnonymousReadDefaultsFromHost is §4.2's actual requirement:
// enabled by default only for a loopback bind, not unconditionally.
func TestAnonymousReadDefaultsFromHost(t *testing.T) {
	t.Setenv("TICKETS_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))

	cfg, err := Load([]string{"--host", "0.0.0.0"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AnonymousRead {
		t.Errorf("AnonymousRead = true for a non-loopback host with no override, want false")
	}

	cfg, err = Load([]string{"--host", "127.0.0.1"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AnonymousRead {
		t.Errorf("AnonymousRead = false for a loopback host with no override, want true")
	}
}

// TestAnonymousReadExplicitOverrideWins confirms an explicit
// --anonymous-read=false is honored even on a loopback host, where the
// computed default would otherwise be true.
func TestAnonymousReadExplicitOverrideWins(t *testing.T) {
	t.Setenv("TICKETS_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))

	cfg, err := Load([]string{"--host", "127.0.0.1", "--anonymous-read=false"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AnonymousRead {
		t.Errorf("AnonymousRead = true despite explicit --anonymous-read=false")
	}

	cfg, err = Load([]string{"--host", "0.0.0.0", "--anonymous-read=true"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AnonymousRead {
		t.Errorf("AnonymousRead = false despite explicit --anonymous-read=true")
	}
}

func TestLoadRejectsInvalidLogFormat(t *testing.T) {
	t.Setenv("TICKETS_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	if _, err := Load([]string{"--log-format", "xml"}); err == nil {
		t.Fatalf("Load with --log-format=xml: want error, got nil")
	}
}

func TestLoadRejectsMalformedConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}
	t.Setenv("TICKETS_CONFIG_FILE", path)
	if _, err := Load(nil); err == nil {
		t.Fatalf("Load with malformed config file: want error, got nil")
	}
}

func TestLoadRejectsInvalidEnvDuration(t *testing.T) {
	t.Setenv("TICKETS_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	t.Setenv("TICKETS_SHUTDOWN_TIMEOUT", "not-a-duration")
	if _, err := Load(nil); err == nil {
		t.Fatalf("Load with invalid TICKETS_SHUTDOWN_TIMEOUT: want error, got nil")
	}
}
