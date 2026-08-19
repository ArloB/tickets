package mcpsrv

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newServer builds an *mcp.Server with the shared tool set registered
// against backend. Both entry points below call this — no tool is ever
// defined twice (verified structurally and operationally by the Step 2
// spike; see docs/spikes/mcp/REPORT.md).
func newServer(backend Backend) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "tickets", Version: "0.1.0"}, nil)
	RegisterTools(s, backend)
	return s
}

// NewStreamableHTTPHandler returns the MCP endpoint mounted on the
// running server, backed by an InProcessBackend (product spec §7.1).
// Phase 0 ships this unauthenticated behind the loopback bind; ADR
// 0006's auth.RequireBearerToken wiring is proven in the spike but not
// activated until Phase 2 defines real agent tokens (ADR 0004).
func NewStreamableHTTPHandler(backend Backend) http.Handler {
	server := newServer(backend)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}

// RunStdio runs the MCP stdio bridge until the client disconnects or
// ctx is cancelled. cmd/tickets' `mcp` subcommand calls this with an
// HTTPBackend pointed at the configured Tickets server (§8.1: the
// bridge never opens SQLite directly).
func RunStdio(ctx context.Context, backend Backend) error {
	server := newServer(backend)
	return server.Run(ctx, &mcp.StdioTransport{})
}
