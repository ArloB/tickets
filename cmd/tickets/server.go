package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/httpapi"
	"github.com/ArloB/tickets/internal/logging"
	"github.com/ArloB/tickets/internal/mcpsrv"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// newRootHandler builds exactly what runServer serves: /api/v1 and the
// embedded web UI (via internal/httpapi) at the root, and an
// unauthenticated MCP Streamable HTTP endpoint at /mcp, both backed by
// the same *service.Service. Extracted so server_test.go exercises the
// identical composition that ships — a test against a differently-
// shaped mux (e.g. MCP mounted at the root instead of /mcp) would
// prove nothing about what actually runs. /mcp is registered as an
// exact pattern, so it does not shadow deeper paths like
// "/mcp/anything" — those fall through to httpapi's "/" static/SPA
// handler like any other unmatched path.
func newRootHandler(svc *service.Service, anonymousRead bool) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.NewHandler(svc, anonymousRead))
	mux.Handle("/mcp", mcpsrv.NewStreamableHTTPHandler(&mcpsrv.InProcessBackend{Svc: svc}))
	return mux
}

// runServer wires internal/config, internal/store, internal/service,
// and newRootHandler into the running server, then hands off to serve
// for the actual listen/shutdown lifecycle.
func runServer(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.LogFormat)
	httpapi.SetLogger(logger)

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store at %s: %w", cfg.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	svc := service.New(st)
	srv := &http.Server{Handler: newRootHandler(svc, cfg.AnonymousRead), ReadHeaderTimeout: 10 * time.Second}

	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr(), err)
	}
	logger.Info("tickets server listening",
		"addr", ln.Addr().String(), "data_dir", cfg.DataDir, "anonymous_read", cfg.AnonymousRead)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, srv, ln, logger, cfg.ShutdownTimeout)
}

// serve runs srv on ln until ctx is cancelled, then performs a
// graceful shutdown bounded by shutdownTimeout before returning
// (product spec §11: "graceful shutdown stops accepting work,
// completes bounded in-flight requests, and closes the database
// cleanly" — the database close is the caller's job, via defer, once
// serve returns).
//
// Split from runServer so tests can drive shutdown by cancelling a
// context directly, rather than depending on OS signal delivery, which
// behaves differently enough across platforms — notably Windows — to
// make an in-process signal-based test unreliable.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, logger *slog.Logger, shutdownTimeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("shutting down", "timeout", shutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return <-serveErr
	}
}
