package main

import (
	"context"
	"flag"
	"os"

	"github.com/ArloB/tickets/internal/mcpsrv"
)

// runMCPBridge is `tickets mcp`: it never opens SQLite directly (product
// spec §8.1). It talks to a running Tickets server's HTTP API via
// mcpsrv.HTTPBackend and serves the same tool set over stdio (ADR 0006).
func runMCPBridge(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	apiURL := fs.String("url", envOr("TICKETS_API_URL", "http://127.0.0.1:8080/api/v1"), "base URL of the Tickets HTTP API")
	if err := fs.Parse(args); err != nil {
		return err
	}

	backend := &mcpsrv.HTTPBackend{BaseURL: *apiURL}
	return mcpsrv.RunStdio(context.Background(), backend)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
