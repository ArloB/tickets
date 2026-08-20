package mcpsrv

import (
	"context"
	"net/http"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
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
// running server, backed by backend and requiring a valid agent
// bearer token on every request (ADR 0004/0006 — Phase 0 shipped this
// unauthenticated; the auth.RequireBearerToken wiring the spike proved
// is activated here). backend.Svc backs tokenVerifier's
// service.VerifyBearerToken calls — the same single source of truth
// internal/httpapi's bearer-token branch uses (ADR 0005).
//
// There is no ResourceMetadataURL set (the spike's note that its
// absence leaves WWW-Authenticate's header value empty on a 401, not
// that auth itself breaks): Phase 2 doesn't build the RFC 9728
// protected-resource-metadata endpoint that URL would point at — MCP
// bearer tokens here are pre-shared secrets issued by POST
// /agents/{name}/tokens, not OAuth-issued, so there is nothing real to
// link to yet.
func NewStreamableHTTPHandler(backend *InProcessBackend) http.Handler {
	server := newServer(backend)
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	requireToken := sdkauth.RequireBearerToken(tokenVerifier(backend.Svc), &sdkauth.RequireBearerTokenOptions{})
	return requireToken(streamable)
}

// RunStdio runs the MCP stdio bridge until the client disconnects or
// ctx is cancelled. cmd/tickets' `mcp` subcommand calls this with an
// HTTPBackend pointed at the configured Tickets server (§8.1: the
// bridge never opens SQLite directly).
func RunStdio(ctx context.Context, backend Backend) error {
	server := newServer(backend)
	return server.Run(ctx, &mcp.StdioTransport{})
}
