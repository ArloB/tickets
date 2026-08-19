package mcpsrv

import (
	"context"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterTools is the single tool-registration function shared by the
// server's HTTP-mounted MCP endpoint and the `tickets mcp` stdio
// bridge (ADR 0006) — the spike at docs/spikes/mcp proved one
// registration function can feed both transports. Phase 0 ships three
// read/create tools mirroring the vertical slice's three HTTP
// endpoints; nothing else is registered until Phase 3.
func RegisterTools(s *mcp.Server, backend Backend) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "project_get",
		Description: "Get a project by its key (e.g. ABC).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectGetInput) (*mcp.CallToolResult, domain.Project, error) {
		proj, err := backend.GetProject(ctx, in.Key)
		if err != nil {
			return nil, domain.Project{}, toolError(err)
		}
		return nil, proj, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ticket_get",
		Description: "Get a ticket by its public reference (e.g. ABC-123).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketGetInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.GetTicket(ctx, in.Ref)
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ticket_create",
		Description: "Create a ticket in a project. It always lands in the project's General feature.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketCreateInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.CreateTicket(ctx, CreateTicketInput(in))
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})
}

type projectGetInput struct {
	Key string `json:"key" jsonschema:"the project key, e.g. ABC"`
}

type ticketGetInput struct {
	Ref string `json:"ref" jsonschema:"the ticket's public reference, e.g. ABC-123"`
}

type ticketCreateInput struct {
	ProjectKey  string `json:"project_key" jsonschema:"the project key, e.g. ABC"`
	Type        string `json:"type" jsonschema:"ticket type: task, bug, security, or chore"`
	Title       string `json:"title" jsonschema:"the ticket title"`
	Description string `json:"description,omitempty" jsonschema:"optional Markdown description"`
	Priority    string `json:"priority,omitempty" jsonschema:"optional priority: critical, high, medium, or low (default medium)"`
	Severity    string `json:"severity,omitempty" jsonschema:"optional severity for bug/security tickets: critical, high, medium, or low"`
}

// toolError formats a *service.Error as "<code>: <message>" so the
// domain.ErrorCode vocabulary (ADR 0006) survives being flattened into
// the plain-text error content the MCP SDK packs into a tool-error
// result. Any other error is reported as internal_error without
// leaking internals, matching docs/contracts/errors.md's HTTP
// behavior.
func toolError(err error) error {
	var svcErr *service.Error
	if errors.As(err, &svcErr) {
		return fmt.Errorf("%s: %s", svcErr.Code, svcErr.Message)
	}
	return fmt.Errorf("%s: an unexpected error occurred", domain.ErrInternal)
}
