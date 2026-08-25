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
const serverInstructions = `Tickets is a self-hosted issue tracker. References use the form
PROJECTKEY-N: a ticket is "ABC-123" (no letter code), a feature is
"ABC-F1", a decision is "ABC-D1". Writing "#ABC-123" inside a ticket's
description or a comment body creates a backlink to that entity — it
does not create a dependency; use ticket_link for that.

List tools (projects_list, tickets_list) return compact rows only —
no description/context/decision/rationale body text — to keep
responses small. Call the matching *_get tool (ticket_get, feature_get,
record_get) for an entity's full detail before acting on its content.

ticket_link's type is either "associated_with" (a loose reference for
context, e.g. linking a ticket to the decision that explains it — no
dependency implied) or one of 8 explicit relationship types
(parent_of, child_of, blocks, blocked_by, related_to, duplicate_of,
supersedes, superseded_by), which are ticket-to-ticket only.

ticket_update is a partial update: only the fields you set are
changed. feature_update and record_update are full-representation
updates instead, with no merge: their schema marks every text field
required, so a compliant client will refuse to send a call missing
one (rather than silently wiping it) — but the value each field
carries still replaces what is stored, unchanged or not. Call
feature_get/record_get first and resend every field's current value
(you need that call's version for expected_version anyway).
record_update's superseded_by is the one optional field: omit it (or
send "") to clear an existing supersession link, the same as any other
omitted field there — it isn't a partial-update exception.

record_* covers decisions, plans, and documents. record_create's kind
is "decision" (default), "plan", or "document". Decisions use
title/context/decision/rationale/consequences/status/superseded_by;
plans and documents use title plus representation ("markdown" default,
"path", or "url") to pick which of body/path/url applies — the other
kind's fields are simply omitted from that call. A plan or document's
representation is fixed at creation and can never be changed by
record_update; there is no file-upload representation over MCP at all
(a tool call has no multipart transport) — upload one via the HTTP API
or CLI instead. record_get/record_update infer which kind a reference
names from the reference itself (ABC-D1 is a decision, ABC-P1 a plan,
ABC-DOC1 a document), so neither needs a kind argument. ticket_comment
is the only comment tool, despite its name — ref accepts a ticket,
feature, decision, plan, or document reference, or a bare project key,
so it works on any of those six kinds.

ticket_comment and record_create accept an optional idempotency_key:
reusing the same key with identical arguments returns the original
result instead of creating a duplicate, and reusing it with different
content is rejected as idempotency_key_reused — useful when retrying
after a dropped connection.

search is a full-text search over tickets, features, decisions, plans,
documents, comments, attachment names, and external link titles/URLs,
ranked by relevance — use it to find a record when you don't already
have its reference, rather than paging through
tickets_list/features_list. project/kind/status narrow an otherwise
cross-project search; a comment, attachment, or link hit's ref names
its owning ticket/feature/decision/plan/document, not the comment or
attachment/link itself.

Creating or commenting on a record subscribes you to it automatically;
you are notified (assignment, an @kind:name mention, a reply/comment,
or a status/field change) on anything you're subscribed to, unless you
caused the change yourself. notifications_list/notifications_mark_read
read and clear your own notification inbox. There is no
subscribe/unsubscribe tool — manage subscriptions over HTTP or the CLI
(tickets subscribe / tickets unsubscribe <ref>) if you need to opt in
or out of something you didn't create or comment on.`

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
