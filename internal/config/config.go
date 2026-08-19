package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the server's runtime configuration. Phase 0 implements
// only what the vertical slice needs: --data-dir and the loopback-by-
// default bind address (product spec §10's first security default).
// The full flags → env → config-file precedence chain (§7.3) is Phase
// 2 work.
type Config struct {
	DataDir string
	Host    string
	Port    string
}

func (c Config) Addr() string { return c.Host + ":" + c.Port }

// Load parses server flags. It never prompts and never reads stdin
// (§7.3's non-interactive requirement, applied early even though the
// rest of §7.3 lands later).
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "directory for the SQLite database and managed file storage")
	host := fs.String("host", "127.0.0.1", "bind address; anything non-loopback prints a warning (product spec §10)")
	port := fs.String("port", "8080", "port to listen on")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{DataDir: *dataDir, Host: *host, Port: *port}
	if !isLoopback(cfg.Host) {
		fmt.Fprintf(os.Stderr,
			"WARNING: binding to non-loopback address %q. Anonymous/unauthenticated "+
				"requests may be reachable from other hosts. See product spec §10.\n", cfg.Host)
	}
	return cfg, nil
}

func isLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "tickets-data"
	}
	return filepath.Join(base, "tickets")
}
