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

// newRootHandler builds exactly what runServer serves: /api/v1 (via
// internal/httpapi) at the root and an unauthenticated MCP Streamable
// HTTP endpoint at /mcp, both backed by the same *service.Service.
// Extracted so server_test.go exercises the identical composition that
// ships — a test against a differently-shaped mux (e.g. MCP mounted at
// the root instead of /mcp) would prove nothing about what actually
// runs. The web UI is Phase 4 work — web.Dist exists but isn't served
// yet.
func newRootHandler(svc *service.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.NewHandler(svc))
	mux.Handle("/mcp", mcpsrv.NewStreamableHTTPHandler(&mcpsrv.InProcessBackend{Svc: svc}))
	return mux
}

// runServer wires internal/config, internal/store, internal/service,
// and newRootHandler into the Phase 0 vertical-slice server, behind the
// 127.0.0.1-by-default listener (product spec §10).
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

	log.Printf("tickets server listening on http://%s (data dir: %s)", cfg.Addr(), cfg.DataDir)
	return http.ListenAndServe(cfg.Addr(), newRootHandler(svc))
}
