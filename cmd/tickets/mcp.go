package main

import (
	"context"
	"flag"
	"os"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/mcpsrv"
)

// runMCPBridge is `tickets mcp`: it never opens SQLite directly (product
// spec §8.1). It talks to a running Tickets server's HTTP API via
// mcpsrv.HTTPBackend (backed by internal/apiclient) and serves the
// same tool set over stdio (ADR 0006).
func runMCPBridge(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	apiURL := fs.String("url", envOr("TICKETS_API_URL", "http://127.0.0.1:8080/api/v1"), "base URL of the Tickets HTTP API")
	token := fs.String("token", envOr("TICKETS_API_TOKEN", ""), "agent bearer token forwarded to the Tickets HTTP API (ADR 0004)")
	project := fs.String("project", envOr("TICKETS_PROJECT", ""),
		"default project key filled in when a tool call omits one — a client-side convenience only, invisible to the server")
	if err := fs.Parse(args); err != nil {
		return err
	}

	backend := &mcpsrv.HTTPBackend{
		Client:         &apiclient.Client{BaseURL: *apiURL, Token: *token},
		DefaultProject: *project,
	}
	return mcpsrv.RunStdio(context.Background(), backend)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
