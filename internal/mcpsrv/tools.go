package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// actorRefSchemaOptions overrides jsonschema-go's default struct
// reflection for domain.ActorRef: left alone, reflection describes it
// as an object with kind/name properties, but ActorRef.MarshalJSON
// (internal/domain/actor.go) actually renders it as the plain
// "kind:name" wire string everywhere in this codebase, HTTP responses
// included. mcp.AddTool derives each tool's OutputSchema from its Out
// type argument via this same reflection (github.com/google/jsonschema-go),
// with no knowledge of a custom MarshalJSON — every Out type below that
// embeds an ActorRef (Ticket/Feature/Project's Creator, Ticket's
// Assignee) needs this override passed explicitly, or the MCP client's
// own output-schema validation rejects the tool's real output.
//
// Types: []string{"null", "string"}, not Type: "string" — every use of
// ActorRef here is behind a pointer (Creator, Assignee both nil until
// set), and jsonschema-go's forType (infer.go) returns a TypeSchemas
// override immediately, before the "wrap with null for a pointer" step
// that every other type gets. Type: "string" alone would reject an
// explicit JSON null the moment a nil *ActorRef ever gets remarshaled
// as "null" rather than omitted (the MCP SDK's own
// StructuredContent -> JSON -> validate round trip, not just this
// codebase's own httpapi encoder, which relies on omitempty instead).
var actorRefSchemaOptions = &jsonschema.ForOptions{
	TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[domain.ActorRef](): {Types: []string{"null", "string"}},
	},
}

// outputSchemaFor builds T's output schema with actorRefSchemaOptions
// applied, for the mcp.Tool literals below. Panics on error, same as
// mcp.AddTool itself does for a schema it can't derive — this only
// ever runs at RegisterTools' fixed, non-request-triggered call time,
// so a failure here is a programming mistake, not a runtime condition
// to recover from.
func outputSchemaFor[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](actorRefSchemaOptions)
	if err != nil {
		panic(fmt.Sprintf("mcpsrv: build output schema for %T: %v", *new(T), err))
	}
	return s
}

// RegisterTools is the single tool-registration function shared by the
// server's HTTP-mounted MCP endpoint and the `tickets mcp` stdio
// bridge (ADR 0006) — the spike at docs/spikes/mcp proved one
// registration function can feed both transports. Phase 0 ships three
// read/create tools mirroring the vertical slice's three HTTP
// endpoints; nothing else is registered until Phase 3.
func RegisterTools(s *mcp.Server, backend Backend) {
	mcp.AddTool(s, &mcp.Tool{
		Name:         "project_get",
		Description:  "Get a project by its key (e.g. ABC).",
		OutputSchema: outputSchemaFor[domain.Project](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectGetInput) (*mcp.CallToolResult, domain.Project, error) {
		proj, err := backend.GetProject(ctx, in.Key)
		if err != nil {
			return nil, domain.Project{}, toolError(err)
		}
		return nil, proj, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_get",
		Description:  "Get a ticket by its public reference (e.g. ABC-123).",
		OutputSchema: outputSchemaFor[domain.Ticket](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketGetInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.GetTicket(ctx, in.Ref)
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_create",
		Description:  "Create a ticket in a project. It always lands in the project's General feature.",
		OutputSchema: outputSchemaFor[domain.Ticket](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketCreateInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.CreateTicket(withCallerActor(ctx, req), CreateTicketInput(in))
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})
}

// withCallerActor attaches the calling agent's Principal to ctx, based
// on the bearer token auth.RequireBearerToken (NewStreamableHTTPHandler)
// verified before this tool call ever reached here (ADR 0006/0004) —
// req.Extra.TokenInfo.UserID is the "kind:name" wire form
// tokenVerifier (auth.go) put there. InProcessBackend.mcpActor is the
// only reader of this; HTTPBackend never touches ctx's Principal at
// all, since the stdio bridge attributes nothing itself — it forwards
// an Authorization header, and the *server* on the other end resolves
// the actor when that HTTP request arrives (see internal/httpapi's
// requestActor). req.Extra or its TokenInfo can be nil for exactly
// that stdio path (no HTTP request, no bearer-token middleware exists
// there at all) — ctx is returned unchanged in that case, which is
// harmless precisely because nothing downstream reads it then.
func withCallerActor(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return ctx
	}
	actor, err := domain.ParseActorRef(req.Extra.TokenInfo.UserID)
	if err != nil {
		return ctx
	}
	return auth.WithPrincipal(ctx, auth.Principal{Actor: actor, Permission: auth.PermissionEditor, AuthMethod: "bearer"})
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
