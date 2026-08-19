package mcpsrv

import (
	"context"

	"github.com/ArloB/tickets/internal/domain"
)

// Backend is what RegisterTools calls — not *service.Service directly.
// This is the resolution to a real tension in ADR 0006: the HTTP-
// mounted MCP endpoint should share internal/service in-process, but
// the `tickets mcp` stdio bridge must talk to the configured Tickets
// HTTP API, never open SQLite directly (product spec §8.1). One tool
// registration function, two Backend implementations:
//
//   - InProcessBackend (this package) wraps *service.Service directly,
//     used by the server's own mcp.NewStreamableHTTPHandler mount.
//   - HTTPBackend (this package) wraps an HTTP client against the
//     Tickets API, used by the `tickets mcp` stdio bridge.
//
// Both ultimately execute the same internal/service business logic —
// HTTPBackend just reaches it through internal/httpapi instead of a
// direct call — so §8.1's "neither interface duplicates business
// logic" holds either way tools are registered.
type Backend interface {
	GetProject(ctx context.Context, key string) (domain.Project, error)
	CreateTicket(ctx context.Context, req CreateTicketInput) (domain.Ticket, error)
	GetTicket(ctx context.Context, ref string) (domain.Ticket, error)
}

// CreateTicketInput mirrors service.CreateTicketRequest but with string
// fields, since it's the shape both Backend implementations (typed
// service call vs. JSON over HTTP) can produce uniformly.
type CreateTicketInput struct {
	ProjectKey  string
	Type        string
	Title       string
	Description string
	Priority    string
	Severity    string
}
