package mcpsrv

import (
	"context"
	"net/http"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverInstructions is product spec §7.1's "MCP server's `instructions`
// field carries the essential cross-tool guidance" — the one place an
// agent reads this once, up front, instead of having to infer it from
// scattered per-tool descriptions. It only states things that are
// actually true of the current tool surface — no forward-looking
// claims about tools that don't exist yet.
const serverInstructions = `Tickets is a self-hosted issue tracker.

References are immutable. Projects use a bare key such as ABC. Other
entities use ABC-123 (ticket), ABC-F1 (feature), ABC-D1 (decision),
ABC-P1 (plan), or ABC-DOC1 (document).

References found in Markdown create backlinks. Both ABC-123 and
#ABC-123 are recognized; #123 also identifies a ticket in a
project-scoped comment. A backlink is only a mention, not a dependency
or other typed relationship.

Use project_brief when you need broad project context. If you already
have the exact entity reference and task, call its *_get tool directly.
List and search tools return compact rows or hits; use project_get,
ticket_get, feature_get, record_get, or comment_get before relying on
an entity's full content.

project_key may be omitted only when using a tickets mcp stdio bridge
configured with --project or TICKETS_PROJECT. Direct /mcp connections
must supply it.

Any tool with expected_version uses optimistic concurrency. Send the
latest version returned by a read or write. A stale version returns
version_conflict with current_version. project_update, ticket_update,
and feature_update are partial. Send status separately from content
fields; ticket assignee and feature moves are also separate operation
groups. Mixed groups are rejected so each call is atomic. record_update
replaces every applicable field, so read the record first and resend
unchanged values. Decision updates require context, decision, rationale,
consequences, and status. Omit or empty superseded_by to clear it.

ticket_create requires exactly one of feature or general:true. There
is no implicit General selection.

record_* handles decisions, plans, and documents. record_create kind
defaults to decision, and new decisions start as proposed. Plans and
documents use one immutable representation: markdown, path, url, or a
file created outside MCP. Set only body, path, or url as selected by
the representation. MCP cannot transfer binary content; attachment_*
tools expose metadata only.

associated_with is a symmetric contextual association between tickets,
features, decisions, plans, or documents. The other relationship types
are directional and ticket-to-ticket only. External URLs are bookmarks,
not entity relationships.

search covers projects, tickets, features, decisions, plans, documents,
comments, attachment names, and external-link titles and URLs. A
comment, attachment, or external-link hit uses ref for its owning
entity.

project_create, ticket_create, feature_create, comment_create, and
record_create accept an optional idempotency_key. Reuse a key only when
retrying identical input; different input with the same key returns
idempotency_key_reused.

Creating a ticket, feature, decision, plan, or document subscribes the
caller to it. Commenting subscribes the caller to the comment's owner.
Notifications are not generated for the caller's own changes. Use
subscription_update to opt in or out.`

// newServer builds an *mcp.Server with the shared tool set registered
// against backend. Both entry points below call this — no tool is ever
// defined twice (verified structurally and operationally by the Step 2
// spike; see docs/spikes/mcp/REPORT.md).
func newServer(backend Backend) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "tickets", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})
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
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
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
