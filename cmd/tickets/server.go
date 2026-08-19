package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/httpapi"
	"github.com/ArloB/tickets/internal/mcpsrv"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// runServer wires internal/config, internal/store, internal/service,
// internal/httpapi, and internal/mcpsrv into the Phase 0 vertical-slice
// server: /api/v1 plus an unauthenticated MCP Streamable HTTP endpoint
// at /mcp, both behind the 127.0.0.1-by-default listener (product spec
// §10). The web UI is Phase 4 work — web.Dist exists but isn't served
// yet.
func runServer(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store at %s: %w", cfg.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	svc := service.New(st)

	mux := http.NewServeMux()
	mux.Handle("/", httpapi.NewHandler(svc))
	mux.Handle("/mcp", mcpsrv.NewStreamableHTTPHandler(&mcpsrv.InProcessBackend{Svc: svc}))

	log.Printf("tickets server listening on http://%s (data dir: %s)", cfg.Addr(), cfg.DataDir)
	return http.ListenAndServe(cfg.Addr(), mux)
}
