package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/httpapi"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var testActor = domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}

const testCorrelationID = "test-correlation-id"

// newTestBackend creates a fresh store + service and seeds one project
// and one ticket, returning the InProcessBackend and the ticket's ref.
func newTestBackend(t *testing.T) (*InProcessBackend, string) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)

	ctx := context.Background()
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "Fix the parser",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	return &InProcessBackend{Svc: svc}, ticket.Ref
}

// TestInProcessBackendCreateTicketUsesContextActor is mcpActor's
// direct unit test: a Principal attached to ctx (the same shape
// withCallerActor builds from a verified bearer token's TokenInfo)
// makes CreateTicket attribute the ticket to that actor, and a ctx
// with no Principal at all makes it fail with ErrUnauthorized rather
// than reaching internal/service with a zero-value actor.
func TestInProcessBackendCreateTicketUsesContextActor(t *testing.T) {
	backend, _ := newTestBackend(t)
	_, agentActor := mustIssueAgentToken(t, backend, "codex")
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{Actor: agentActor, Permission: auth.PermissionEditor, AuthMethod: "bearer"})

	ticket, err := backend.CreateTicket(ctx, CreateTicketInput{ProjectKey: "ABC", Type: "task", Title: "Created with a context actor"})
	if err != nil {
		t.Fatalf("CreateTicket with a Principal on ctx: %v", err)
	}
	if ticket.Ref == "" {
		t.Fatalf("CreateTicket returned no ref")
	}

	if _, err := backend.CreateTicket(context.Background(), CreateTicketInput{ProjectKey: "ABC", Type: "task", Title: "Should be rejected"}); err == nil {
		t.Fatal("CreateTicket with no Principal on ctx: want an error, got nil")
	} else {
		var svcErr *service.Error
		if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrUnauthorized {
			t.Errorf("CreateTicket with no Principal on ctx: got %v, want a *service.Error with code %q", err, domain.ErrUnauthorized)
		}
	}
}

// TestInProcessBackendAddCommentOnEveryEntityKind is Phase 6 Step 2's
// regression test for parseCommentRef: ticket_comment is ref-agnostic
// now, so this drives InProcessBackend.AddComment against a feature, a
// decision, and a bare project key (the one form domain.Parse itself
// can't handle — see parseCommentRef's doc) to confirm none of them
// still hit the old "reference must be a ticket reference" rejection.
func TestInProcessBackendAddCommentOnEveryEntityKind(t *testing.T) {
	backend, _ := newTestBackend(t)
	_, agentActor := mustIssueAgentToken(t, backend, "codex")
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{Actor: agentActor, Permission: auth.PermissionEditor, AuthMethod: "bearer"})

	decision, err := backend.Svc.CreateDecision(ctx, service.CreateDecisionRequest{ProjectKey: "ABC", Title: "D", Decision: "x"}, agentActor, service.NewCorrelationID(), "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}

	for _, ref := range []string{"ABC-F1", decision.Ref, "ABC"} {
		t.Run(ref, func(t *testing.T) {
			result, err := backend.AddComment(ctx, ref, "hello "+ref, "")
			if err != nil {
				t.Fatalf("AddComment(%q): %v", ref, err)
			}
			if result.ID == 0 {
				t.Errorf("AddComment(%q) result = %+v, want a nonzero id", ref, result)
			}
		})
	}
}

func decodeTicketResult(t *testing.T, res *mcp.CallToolResult) domain.Ticket {
	t.Helper()
	return decodeResult[domain.Ticket](t, res)
}

// decodeResult re-marshals a tool call's StructuredContent (a
// map[string]any, since that's how the MCP SDK represents it) back
// into T — every _get/_list/_create tool's typed output.
func decodeResult[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	var out T
	if res == nil || res.StructuredContent == nil {
		t.Fatalf("no structured content in result: %+v", res)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-marshal structured content: %v", err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, b)
	}
	return out
}

// TestListToolsOverInMemoryTransport proves projects_list/tickets_list
// reach the same backend/service as project_get/ticket_get, returning
// compact rows for the one seeded project and ticket.
func TestListToolsOverInMemoryTransport(t *testing.T) {
	backend, ref := newTestBackend(t)
	server := newServer(backend)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = server.Run(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	projRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "projects_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool projects_list: %v", err)
	}
	if projRes.IsError {
		t.Fatalf("projects_list returned a tool error: %+v", projRes.Content)
	}
	projects := decodeResult[ProjectsListOutput](t, projRes)
	if len(projects.Projects) != 1 || projects.Projects[0].Key != "ABC" {
		t.Errorf("projects_list = %+v, want exactly one project ABC", projects)
	}

	ticketsRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tickets_list", Arguments: map[string]any{"project_key": "ABC"}})
	if err != nil {
		t.Fatalf("CallTool tickets_list: %v", err)
	}
	if ticketsRes.IsError {
		t.Fatalf("tickets_list returned a tool error: %+v", ticketsRes.Content)
	}
	tickets := decodeResult[TicketsListOutput](t, ticketsRes)
	if len(tickets.Tickets) != 1 || tickets.Tickets[0].Ref != ref {
		t.Errorf("tickets_list = %+v, want exactly ticket %q", tickets, ref)
	}
}

// TestToolsOverInMemoryTransport proves RegisterTools/InProcessBackend
// work correctly: a real MCP client (not a direct Go call) lists tools
// and calls ticket_get, reaching internal/service through the same
// backend the HTTP-mounted endpoint uses. It also confirms
// ticket_create fails cleanly here specifically because in-memory
// transport has no bearer-token layer at all (mcp.NewInMemoryTransports
// never runs sdkauth.RequireBearerToken, unlike NewStreamableHTTPHandler)
// — this is exactly what an unauthenticated caller looks like in
// production, not a gap in this test's coverage. The authenticated
// create path is TestTicketCreateOverRealStreamableHTTPWithBearerToken
// below; the context-threading mechanism itself
// (withCallerActor/mcpActor) has its own direct unit test,
// TestInProcessBackendCreateTicketUsesContextActor.
func TestToolsOverInMemoryTransport(t *testing.T) {
	backend, ref := newTestBackend(t)
	server := newServer(backend)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{
		"project_brief": false, "project_get": false, "projects_list": false,
		"ticket_get": false, "ticket_create": false, "tickets_list": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q not advertised", name)
		}
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ref}})
	if err != nil {
		t.Fatalf("CallTool ticket_get: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_get returned a tool error: %+v", res.Content)
	}
	got := decodeTicketResult(t, res)
	if got.Ref != ref {
		t.Errorf("ticket_get returned ref %q, want %q", got.Ref, ref)
	}

	createRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"project_key": "ABC", "type": "task", "title": "Created via MCP",
		},
	})
	if err != nil {
		t.Fatalf("CallTool ticket_create: %v", err)
	}
	if !createRes.IsError {
		t.Fatalf("ticket_create over an unauthenticated transport: want a tool error, got success: %+v", createRes)
	}
	var createErrText string
	for _, c := range createRes.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			createErrText += tc.Text
		}
	}
	if !strings.Contains(createErrText, string(domain.ErrUnauthorized)) {
		t.Errorf("ticket_create tool error content %q does not mention code %q (mcpActor should reject a zero-value actor as unauthorized, not let it reach a store lookup)", createErrText, domain.ErrUnauthorized)
	}

	_ = session.Close()
	if err := <-serverDone; err != nil {
		t.Errorf("server.Run returned error: %v", err)
	}
}

// TestToolErrorMapping confirms a not_found service error reaches the
// MCP client as a tool error whose text carries the domain.ErrorCode
// (ADR 0006: MCP tool errors reuse the HTTP error vocabulary).
func TestToolErrorMapping(t *testing.T) {
	backend, _ := newTestBackend(t)
	server := newServer(backend)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	go func() { _ = server.Run(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": "ABC-999"}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error for a missing ticket, got: %+v", res)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, string(domain.ErrNotFound)) {
		t.Errorf("tool error content %q does not mention code %q", text, domain.ErrNotFound)
	}
}

// TestToolOutputSchemaAllowsNonNilAssignee exercises the Assignee half
// of tools.go's actorRefSchemaOptions override: every other test in
// this file that populates an ActorRef field goes through Creator
// (always set, since Step 9), but none of them ever puts a non-nil
// Assignee through a real MCP tool call — the only path that actually
// remarshals domain.Ticket to JSON and validates it against the tool's
// declared OutputSchema (session.CallTool, not this package's own
// Go-level assertions). A pointer field's nil case never needs the
// override's "null" alternative in practice (encoding/json honors
// Assignee/Creator's omitempty and omits a nil pointer outright, it's
// never marshaled as a literal JSON null), but the present case still
// needs the override to have the right shape at all, and until this
// test, nothing exercised it for any field but Creator.
func TestToolOutputSchemaAllowsNonNilAssignee(t *testing.T) {
	backend, ref := newTestBackend(t)
	ctx := context.Background()

	parsed, err := domain.Parse(ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	ticket, err := backend.Svc.GetTicket(ctx, parsed)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if _, err := backend.Svc.AssignTicket(ctx, service.AssignTicketRequest{Ref: parsed, Assignee: &testActor, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AssignTicket: %v", err)
	}

	server := newServer(backend)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ref}})
	if err != nil {
		t.Fatalf("CallTool ticket_get: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_get with a non-nil Assignee returned a tool error (likely a schema validation failure): %+v", res.Content)
	}
	got := decodeTicketResult(t, res)
	if got.Assignee == nil || *got.Assignee != testActor {
		t.Errorf("ticket_get over MCP: Assignee = %v, want %v", got.Assignee, testActor)
	}
}

// bearerTransport injects a fixed Authorization: Bearer header on
// every outgoing request — mcp.StreamableClientTransport has no header
// field of its own, only HTTPClient, so this is how a test acts as an
// authenticated agent (ADR 0004/0006).
type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// mustIssueAgentToken creates an agent actor and a token for it
// directly through backend.Svc (no HTTP involved — the admin/token
// management endpoints are internal/httpapi's concern, exercised
// there), returning the raw token TestToolsOverRealStreamableHTTP and
// friends present as a bearer credential.
func mustIssueAgentToken(t *testing.T, backend *InProcessBackend, name string) (raw string, agentRef domain.ActorRef) {
	t.Helper()
	ctx := context.Background()
	agent, err := backend.Svc.CreateAgent(ctx, service.CreateAgentRequest{Name: name}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	raw, _, err = backend.Svc.CreateAgentToken(ctx, agent.Ref, "", nil, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}
	return raw, agent.Ref
}

// TestToolsOverRealStreamableHTTP closes the gap between "MCP over
// Streamable HTTP" (gate 7's literal wording) and the in-memory-
// transport test above: this one runs NewStreamableHTTPHandler behind
// a real httptest.Server (actual TCP, actual HTTP requests) and
// connects with mcp.StreamableClientTransport, exactly as
// cmd/tickets' `server` subcommand mounts it at /mcp — now requiring a
// real agent bearer token (ADR 0004/0006), which this test presents.
func TestToolsOverRealStreamableHTTP(t *testing.T) {
	backend, ref := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	httpHandler := NewStreamableHTTPHandler(backend)
	ts := httptest.NewServer(httpHandler)
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect over real Streamable HTTP: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ref}})
	if err != nil {
		t.Fatalf("CallTool over real HTTP: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_get over real HTTP returned a tool error: %+v", res.Content)
	}
	got := decodeTicketResult(t, res)
	if got.Ref != ref {
		t.Errorf("ticket_get over real HTTP returned ref %q, want %q", got.Ref, ref)
	}
}

// TestToolsOverRealStreamableHTTPRejectsMissingToken confirms the
// bearer-token requirement is actually enforced, not just satisfiable
// — the positive case above proves a valid token works, this proves
// its absence is rejected before any tool ever runs.
func TestToolsOverRealStreamableHTTPRejectsMissingToken(t *testing.T) {
	backend, _ := newTestBackend(t)
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	_, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err == nil {
		t.Fatal("connect with no bearer token: want error, got nil")
	}
}

// TestTicketCreateOverRealStreamableHTTPWithBearerToken proves the
// actor-attribution wiring (withCallerActor/mcpActor) actually resolves
// a real, valid actor from the verified bearer token, end to end
// through the HTTP layer — not just that the plumbing compiles. This
// is the create-side counterpart to
// TestToolsOverInMemoryTransport's now-negative assertion: that test
// shows an unresolvable (zero-value) actor makes ticket_create fail;
// this one shows a properly resolved agent actor makes it succeed and
// is the actor domain.Ticket.Creator actually records — Step 9 added
// that field, so the specific-actor assertion this doc comment used to
// defer is no longer deferred.
func TestTicketCreateOverRealStreamableHTTPWithBearerToken(t *testing.T) {
	backend, _ := newTestBackend(t)
	raw, agentRef := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_create", Arguments: map[string]any{
		"project_key": "ABC", "type": "task", "title": "Created by an authenticated agent",
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_create: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_create returned a tool error: %+v", res.Content)
	}
	created := decodeTicketResult(t, res)
	if created.Ref == "" {
		t.Fatalf("ticket_create returned no ref")
	}
	if created.Creator == nil || *created.Creator != agentRef {
		t.Errorf("ticket_create over /mcp with a codex bearer token: Creator = %v, want %v", created.Creator, agentRef)
	}
}

// TestTicketUpdateOverRealStreamableHTTP proves ticket_update reaches
// the same UpdateTicketStatus/UpdateTicketFields service calls the
// HTTP API's PATCH/PUT routes use, returns a compact TicketWriteResult
// (not the full ticket ticket_create/ticket_get return), and rejects
// a stale expected_version as version_conflict rather than silently
// applying the change.
func TestTicketUpdateOverRealStreamableHTTP(t *testing.T) {
	backend, ref := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	getRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ref}})
	if err != nil {
		t.Fatalf("CallTool ticket_get: %v", err)
	}
	seeded := decodeTicketResult(t, getRes)

	status, priority := "in_progress", "high"
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_update", Arguments: map[string]any{
		"ref": ref, "status": status, "priority": priority, "expected_version": seeded.Version,
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_update: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_update returned a tool error: %+v", res.Content)
	}
	result := decodeResult[TicketWriteResult](t, res)
	// Two mutating calls happen here (UpdateTicketStatus then
	// UpdateTicketFields), each its own transaction that bumps
	// entities.version by 1 (docs/contracts/concurrency.md) — so
	// setting status *and* priority in one ticket_update call advances
	// the version by 2, not 1.
	wantVersion := seeded.Version + 2
	if result.Ref != ref || result.Status != status || result.Priority != priority || result.Version != wantVersion {
		t.Errorf("ticket_update result = %+v, want ref=%q status=%q priority=%q version=%d", result, ref, status, priority, wantVersion)
	}

	// A title left unset by this call must survive unchanged — proof
	// the merge actually read the ticket's current fields rather than
	// clobbering them with zero values.
	afterRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ref}})
	if err != nil {
		t.Fatalf("CallTool ticket_get after update: %v", err)
	}
	after := decodeTicketResult(t, afterRes)
	if after.Title != seeded.Title {
		t.Errorf("ticket title after update = %q, want unchanged %q", after.Title, seeded.Title)
	}

	// A stale expected_version must be rejected, not silently applied.
	staleRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_update", Arguments: map[string]any{
		"ref": ref, "status": "done", "expected_version": seeded.Version,
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_update with a stale version: %v", err)
	}
	if !staleRes.IsError {
		t.Fatalf("ticket_update with a stale expected_version: want a tool error, got success: %+v", staleRes)
	}
	var staleText string
	for _, c := range staleRes.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			staleText += tc.Text
		}
	}
	if !strings.Contains(staleText, string(domain.ErrVersionConflict)) {
		t.Errorf("stale ticket_update tool error %q does not mention code %q", staleText, domain.ErrVersionConflict)
	}
}

// TestTicketCommentOverRealStreamableHTTP proves ticket_comment reaches
// the same AddComment service call the HTTP API's POST
// /tickets/{ref}/comments route uses, attributes the comment to the
// bearer-token actor, and returns a compact CommentWriteResult.
func TestTicketCommentOverRealStreamableHTTP(t *testing.T) {
	backend, ref := newTestBackend(t)
	raw, agentRef := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_comment", Arguments: map[string]any{
		"ref": ref, "body": "Started looking into this",
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_comment: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_comment returned a tool error: %+v", res.Content)
	}
	result := decodeResult[CommentWriteResult](t, res)
	if result.ID == 0 || result.Version != 1 {
		t.Errorf("ticket_comment result = %+v, want a nonzero id and version=1", result)
	}

	comments, err := backend.Svc.ListComments(ctx, mustParseTicketRef(t, ref))
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "Started looking into this" || comments[0].Author != agentRef {
		t.Errorf("comments after ticket_comment = %+v, want one comment by %v with the given body", comments, agentRef)
	}
}

// TestTicketLinkOverRealStreamableHTTP proves ticket_link dispatches
// an explicit relationship type to AddRelationship and
// "associated_with" to AddAssociation, reaching the same
// service-level edges the HTTP API's relationship/association routes
// create.
func TestTicketLinkOverRealStreamableHTTP(t *testing.T) {
	backend, ref := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ctx := context.Background()

	other, err := backend.Svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Blocking work",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create second ticket: %v", err)
	}

	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	relRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_link", Arguments: map[string]any{
		"ref": ref, "type": "blocked_by", "target": other.Ref,
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_link (relationship): %v", err)
	}
	if relRes.IsError {
		t.Fatalf("ticket_link relationship call returned a tool error: %+v", relRes.Content)
	}
	relResult := decodeResult[LinkWriteResult](t, relRes)
	if relResult.Type != "blocked_by" || relResult.Target != other.Ref {
		t.Errorf("ticket_link relationship result = %+v, want type=blocked_by target=%q", relResult, other.Ref)
	}

	assocRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_link", Arguments: map[string]any{
		"ref": ref, "type": "associated_with", "target": other.Ref,
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_link (association): %v", err)
	}
	if assocRes.IsError {
		t.Fatalf("ticket_link association call returned a tool error: %+v", assocRes.Content)
	}

	parsedRef, err := domain.Parse(ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	views, err := backend.Svc.GetTicketRelationships(ctx, parsedRef)
	if err != nil {
		t.Fatalf("GetTicketRelationships: %v", err)
	}
	foundRel := false
	for _, v := range views {
		if formatted, ferr := domain.Format(v.Other); ferr == nil && string(v.Type) == "blocked_by" && formatted == other.Ref {
			foundRel = true
		}
	}
	if !foundRel {
		t.Errorf("relationships for %s = %+v, want a blocked_by edge to %s", ref, views, other.Ref)
	}

	associated, err := backend.Svc.GetAssociations(ctx, parsedRef)
	if err != nil {
		t.Fatalf("GetAssociations: %v", err)
	}
	foundAssoc := false
	for _, a := range associated {
		if formatted, ferr := domain.Format(a); ferr == nil && formatted == other.Ref {
			foundAssoc = true
		}
	}
	if !foundAssoc {
		t.Errorf("associations for %s = %+v, want %s", ref, associated, other.Ref)
	}
}

// TestProjectBriefOverRealStreamableHTTP proves project_brief reaches
// the same service.ProjectBrief the HTTP API's GET .../brief route
// uses, over the real MCP transport InProcessBackend serves.
func TestProjectBriefOverRealStreamableHTTP(t *testing.T) {
	backend, ticketRef := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "project_brief", Arguments: map[string]any{"key": "ABC"}})
	if err != nil {
		t.Fatalf("CallTool project_brief: %v", err)
	}
	if res.IsError {
		t.Fatalf("project_brief returned a tool error: %+v", res.Content)
	}
	brief := decodeResult[ProjectBrief](t, res)
	if brief.Project.Key != "ABC" {
		t.Errorf("brief.Project.Key = %q, want %q", brief.Project.Key, "ABC")
	}
	found := false
	for _, ticket := range brief.IssueRegister {
		if ticket.Ref == ticketRef {
			found = true
		}
	}
	if !found {
		t.Errorf("IssueRegister = %+v, want it to contain the seeded bug %q", brief.IssueRegister, ticketRef)
	}
	if len(brief.Features) != 1 {
		t.Errorf("len(Features) = %d, want 1 (General)", len(brief.Features))
	}
}

// TestFeatureToolsOverRealStreamableHTTP proves feature_get/
// feature_create/feature_update reach the same service methods the
// HTTP API's feature routes use — full detail from feature_get,
// compact FeatureWriteResult from create/update, and a full-
// representation update (unlike ticket_update, every field is
// required, no partial merge).
func TestFeatureToolsOverRealStreamableHTTP(t *testing.T) {
	backend, _ := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	createRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "feature_create", Arguments: map[string]any{
		"project_key": "ABC", "title": "Payments", "priority": "high",
	}})
	if err != nil {
		t.Fatalf("CallTool feature_create: %v", err)
	}
	if createRes.IsError {
		t.Fatalf("feature_create returned a tool error: %+v", createRes.Content)
	}
	created := decodeResult[FeatureWriteResult](t, createRes)
	if created.Ref == "" || created.Priority != "high" || created.Version != 1 {
		t.Errorf("feature_create result = %+v, want a ref, priority=high, version=1", created)
	}

	getRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "feature_get", Arguments: map[string]any{"ref": created.Ref}})
	if err != nil {
		t.Fatalf("CallTool feature_get: %v", err)
	}
	if getRes.IsError {
		t.Fatalf("feature_get returned a tool error: %+v", getRes.Content)
	}
	got := decodeResult[domain.Feature](t, getRes)
	if got.Title != "Payments" {
		t.Errorf("feature_get result = %+v, want title=Payments", got)
	}

	updateRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "feature_update", Arguments: map[string]any{
		"ref": created.Ref, "title": "Payments (v2)", "description": got.Description, "priority": "medium", "expected_version": got.Version,
	}})
	if err != nil {
		t.Fatalf("CallTool feature_update: %v", err)
	}
	if updateRes.IsError {
		t.Fatalf("feature_update returned a tool error: %+v", updateRes.Content)
	}
	updated := decodeResult[FeatureWriteResult](t, updateRes)
	if updated.Priority != "medium" || updated.Version != got.Version+1 {
		t.Errorf("feature_update result = %+v, want priority=medium version=%d", updated, got.Version+1)
	}
}

// TestRecordToolsOverRealStreamableHTTP proves record_get/
// record_create/record_update reach the same decision service methods
// the HTTP API's decision routes use, and that ticket_link's
// associated_with dispatch works with a decision as the target — the
// exit-criterion workflow's "read linked context" step for a decision
// depends on both.
func TestRecordToolsOverRealStreamableHTTP(t *testing.T) {
	backend, ticketRef := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	createRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
		"project_key": "ABC", "title": "Use SQLite", "context": "We need a store", "decision": "Use SQLite", "rationale": "Simplicity",
		"consequences": "Simpler ops",
	}})
	if err != nil {
		t.Fatalf("CallTool record_create: %v", err)
	}
	if createRes.IsError {
		t.Fatalf("record_create returned a tool error: %+v", createRes.Content)
	}
	created := decodeResult[RecordWriteResult](t, createRes)
	if created.Ref == "" || created.Kind != "decision" || created.Status != "proposed" || created.Version != 1 {
		t.Errorf("record_create result = %+v, want a ref, kind=decision, status=proposed, version=1", created)
	}

	getRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": created.Ref}})
	if err != nil {
		t.Fatalf("CallTool record_get: %v", err)
	}
	if getRes.IsError {
		t.Fatalf("record_get returned a tool error: %+v", getRes.Content)
	}
	got := decodeResult[RecordDetail](t, getRes)
	if got.Kind != "decision" || got.Title != "Use SQLite" || got.Rationale != "Simplicity" || got.Consequences != "Simpler ops" {
		t.Errorf("record_get result = %+v, want kind=decision title=%q rationale=Simplicity consequences=%q", got, "Use SQLite", "Simpler ops")
	}

	// record_update is a full-representation update: every field
	// (including consequences/superseded_by) must be resent, and an
	// omitted one is cleared — the same contract PATCH /decisions/{ref}
	// has (docs/contracts/concurrency.md). This exercises both fields
	// through the MCP surface specifically because they are the two
	// UpdateDecisionInput gained in Phase 5 Step 2 and are easy to leave
	// unwired on this backend while the HTTP path already works.
	updateRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_update", Arguments: map[string]any{
		"ref": created.Ref, "title": got.Title, "context": got.Context, "decision": got.Decision, "rationale": got.Rationale,
		"consequences": got.Consequences, "status": "accepted", "expected_version": got.Version,
	}})
	if err != nil {
		t.Fatalf("CallTool record_update: %v", err)
	}
	if updateRes.IsError {
		t.Fatalf("record_update returned a tool error: %+v", updateRes.Content)
	}
	updated := decodeResult[RecordWriteResult](t, updateRes)
	if updated.Status != "accepted" || updated.Version != got.Version+1 {
		t.Errorf("record_update result = %+v, want status=accepted version=%d", updated, got.Version+1)
	}

	// A second decision, then supersede the first with it — proving
	// superseded_by reaches the service layer through this tool, not
	// just the HTTP route.
	secondRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
		"project_key": "ABC", "title": "Use Postgres instead",
	}})
	if err != nil {
		t.Fatalf("CallTool record_create (second): %v", err)
	}
	second := decodeResult[RecordWriteResult](t, secondRes)

	supersedeRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_update", Arguments: map[string]any{
		"ref": created.Ref, "title": got.Title, "context": got.Context, "decision": got.Decision, "rationale": got.Rationale,
		"consequences": got.Consequences, "status": "superseded", "superseded_by": second.Ref, "expected_version": updated.Version,
	}})
	if err != nil {
		t.Fatalf("CallTool record_update (supersede): %v", err)
	}
	if supersedeRes.IsError {
		t.Fatalf("record_update (supersede) returned a tool error: %+v", supersedeRes.Content)
	}

	finalGetRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": created.Ref}})
	if err != nil {
		t.Fatalf("CallTool record_get (final): %v", err)
	}
	final := decodeResult[RecordDetail](t, finalGetRes)
	if final.Consequences != "Simpler ops" {
		t.Errorf("final record_get consequences = %q, want %q (must survive the supersede update)", final.Consequences, "Simpler ops")
	}
	if final.SupersededBy == nil || *final.SupersededBy != second.Ref {
		t.Errorf("final record_get superseded_by = %v, want %q", final.SupersededBy, second.Ref)
	}

	linkRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_link", Arguments: map[string]any{
		"ref": ticketRef, "type": "associated_with", "target": created.Ref,
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_link with a decision target: %v", err)
	}
	if linkRes.IsError {
		t.Fatalf("ticket_link with a decision target returned a tool error: %+v", linkRes.Content)
	}

	ticketParsed, err := domain.Parse(ticketRef)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	associated, err := backend.Svc.GetAssociations(ctx, ticketParsed)
	if err != nil {
		t.Fatalf("GetAssociations: %v", err)
	}
	found := false
	for _, a := range associated {
		if formatted, ferr := domain.Format(a); ferr == nil && formatted == created.Ref {
			found = true
		}
	}
	if !found {
		t.Errorf("ticket associations = %+v, want it to include decision %s", associated, created.Ref)
	}
}

// TestRecordToolsContentItemKindOverRealStreamableHTTP proves record_*'s
// kind discriminator (Phase 5 Step 3) actually dispatches to
// CreateContentItem/GetContentItem/UpdateContentItem for kind="plan"
// and kind="document" — not just that the old decision path still
// works (TestRecordToolsOverRealStreamableHTTP covers that).
func TestRecordToolsContentItemKindOverRealStreamableHTTP(t *testing.T) {
	backend, _ := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	for _, kind := range []string{"plan", "document"} {
		t.Run(kind, func(t *testing.T) {
			createRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
				"project_key": "ABC", "kind": kind, "title": "Rollout", "body": "Step one",
			}})
			if err != nil {
				t.Fatalf("CallTool record_create: %v", err)
			}
			if createRes.IsError {
				t.Fatalf("record_create returned a tool error: %+v", createRes.Content)
			}
			created := decodeResult[RecordWriteResult](t, createRes)
			if created.Ref == "" || created.Kind != kind || created.Version != 1 || created.Status != "" {
				t.Errorf("record_create result = %+v, want a ref, kind=%s, version=1, no status", created, kind)
			}

			getRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": created.Ref}})
			if err != nil {
				t.Fatalf("CallTool record_get: %v", err)
			}
			if getRes.IsError {
				t.Fatalf("record_get returned a tool error: %+v", getRes.Content)
			}
			got := decodeResult[RecordDetail](t, getRes)
			if got.Kind != kind || got.Title != "Rollout" || got.Body != "Step one" || got.Context != "" {
				t.Errorf("record_get result = %+v, want kind=%s title=Rollout body=%q and no decision-only fields", got, kind, "Step one")
			}

			updateRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_update", Arguments: map[string]any{
				"ref": created.Ref, "title": "Rollout (final)", "body": "Step one\nStep two", "expected_version": got.Version,
			}})
			if err != nil {
				t.Fatalf("CallTool record_update: %v", err)
			}
			if updateRes.IsError {
				t.Fatalf("record_update returned a tool error: %+v", updateRes.Content)
			}
			updated := decodeResult[RecordWriteResult](t, updateRes)
			if updated.Kind != kind || updated.Version != got.Version+1 {
				t.Errorf("record_update result = %+v, want kind=%s version=%d", updated, kind, got.Version+1)
			}

			finalGetRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": created.Ref}})
			if err != nil {
				t.Fatalf("CallTool record_get (final): %v", err)
			}
			final := decodeResult[RecordDetail](t, finalGetRes)
			if final.Body != "Step one\nStep two" {
				t.Errorf("final record_get body = %q, want %q", final.Body, "Step one\nStep two")
			}
		})
	}
}

// TestRecordUpdateRejectsOmittedDecisionField is a regression test for
// a code-review finding: sharing recordUpdateInput's schema across
// decisions and content items meant its decision-only fields
// (context/decision/rationale/consequences/status) had to gain
// `omitempty` — and a first version of this change let an MCP client
// omit e.g. context on a decision update, silently wiping it instead
// of erroring (the "resend every field or it's cleared" contract this
// tool has always documented). requireDecisionUpdateFields (tools.go)
// fixes this by making those fields *string and rejecting a nil one
// with validation_failed — this proves an omitted field errors rather
// than reaching the service layer as "".
func TestRecordUpdateRejectsOmittedDecisionField(t *testing.T) {
	backend, _ := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	createRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
		"project_key": "ABC", "title": "Use SQLite", "context": "We need a store", "decision": "Use SQLite",
		"rationale": "Simplicity", "consequences": "Simpler ops",
	}})
	if err != nil {
		t.Fatalf("CallTool record_create: %v", err)
	}
	created := decodeResult[RecordWriteResult](t, createRes)

	// Omit "context" entirely — a real client bug this test guards
	// against reintroducing.
	updateRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_update", Arguments: map[string]any{
		"ref": created.Ref, "title": "Use SQLite", "decision": "Use SQLite", "rationale": "Simplicity",
		"consequences": "Simpler ops", "status": "accepted", "expected_version": created.Version,
	}})
	if err != nil {
		t.Fatalf("CallTool record_update: %v", err)
	}
	if !updateRes.IsError {
		t.Fatalf("record_update omitting context: want a tool error, got success: %+v", updateRes.Content)
	}

	// The decision's context must be untouched — no partial write
	// happened before the rejection.
	getRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": created.Ref}})
	if err != nil {
		t.Fatalf("CallTool record_get: %v", err)
	}
	got := decodeResult[RecordDetail](t, getRes)
	if got.Context != "We need a store" {
		t.Errorf("decision context after a rejected update = %q, want it unchanged at %q", got.Context, "We need a store")
	}
}

// TestIdempotencyKeyOverRealStreamableHTTP proves record_create and
// ticket_comment's idempotency_key input actually reaches
// InProcessBackend's fingerprint/checkIdempotency plumbing over a real
// MCP session: replaying the same key with identical arguments returns
// the original record instead of creating a second one, and reusing
// the same key with different content is rejected as
// idempotency_key_reused rather than silently overwriting or
// duplicating.
func TestIdempotencyKeyOverRealStreamableHTTP(t *testing.T) {
	backend, ticketRef := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	create := func(key, title string) (*mcp.CallToolResult, DecisionWriteResult) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
			"project_key": "ABC", "title": title, "context": "ctx", "decision": "dec", "rationale": "why",
			"idempotency_key": key,
		}})
		if err != nil {
			t.Fatalf("CallTool record_create: %v", err)
		}
		if res.IsError {
			return res, DecisionWriteResult{}
		}
		return res, decodeResult[DecisionWriteResult](t, res)
	}

	first, firstOut := create("dup-key-1", "Use SQLite")
	if first.IsError {
		t.Fatalf("record_create with a fresh idempotency_key: want success, got tool error: %+v", first.Content)
	}

	replay, replayOut := create("dup-key-1", "Use SQLite")
	if replay.IsError {
		t.Fatalf("record_create replayed with the same key and content: want the cached result, got tool error: %+v", replay.Content)
	}
	if replayOut.Ref != firstOut.Ref {
		t.Errorf("record_create replay ref = %q, want the original %q (a new decision was created instead of returning the cached one)", replayOut.Ref, firstOut.Ref)
	}

	reused, _ := create("dup-key-1", "Use Postgres instead")
	if !reused.IsError {
		t.Fatalf("record_create with a reused key but different content: want idempotency_key_reused, got success")
	}
	var reusedText string
	for _, c := range reused.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			reusedText += tc.Text
		}
	}
	if !strings.Contains(reusedText, string(domain.ErrIdempotencyKeyReused)) {
		t.Errorf("reused-key record_create tool error %q does not mention code %q", reusedText, domain.ErrIdempotencyKeyReused)
	}

	addComment := func(key, body string) *mcp.CallToolResult {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_comment", Arguments: map[string]any{
			"ref": ticketRef, "body": body, "idempotency_key": key,
		}})
		if err != nil {
			t.Fatalf("CallTool ticket_comment: %v", err)
		}
		return res
	}

	firstComment := addComment("comment-key-1", "hello")
	if firstComment.IsError {
		t.Fatalf("ticket_comment with a fresh idempotency_key: want success, got tool error: %+v", firstComment.Content)
	}
	firstCommentOut := decodeResult[CommentWriteResult](t, firstComment)

	replayComment := addComment("comment-key-1", "hello")
	if replayComment.IsError {
		t.Fatalf("ticket_comment replayed with the same key and content: want the cached result, got tool error: %+v", replayComment.Content)
	}
	replayCommentOut := decodeResult[CommentWriteResult](t, replayComment)
	if replayCommentOut.ID != firstCommentOut.ID {
		t.Errorf("ticket_comment replay id = %d, want the original %d (a new comment was created instead of returning the cached one)", replayCommentOut.ID, firstCommentOut.ID)
	}

	reusedComment := addComment("comment-key-1", "different body")
	if !reusedComment.IsError {
		t.Fatalf("ticket_comment with a reused key but different content: want idempotency_key_reused, got success")
	}

	createProject := func(key, title string) (*mcp.CallToolResult, domain.Project) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "project_create", Arguments: map[string]any{
			"key": "IDP", "title": title, "idempotency_key": key,
		}})
		if err != nil {
			t.Fatalf("CallTool project_create: %v", err)
		}
		if res.IsError {
			return res, domain.Project{}
		}
		return res, decodeResult[domain.Project](t, res)
	}

	firstProject, firstProjectOut := createProject("project-key-1", "Idempotent Project")
	if firstProject.IsError {
		t.Fatalf("project_create with a fresh idempotency_key: want success, got tool error: %+v", firstProject.Content)
	}

	replayProject, replayProjectOut := createProject("project-key-1", "Idempotent Project")
	if replayProject.IsError {
		t.Fatalf("project_create replayed with the same key and content: want the cached result, got tool error: %+v", replayProject.Content)
	}
	if replayProjectOut.Key != firstProjectOut.Key {
		t.Errorf("project_create replay key = %q, want the original %q (a new project was created instead of returning the cached one)", replayProjectOut.Key, firstProjectOut.Key)
	}

	reusedProject, _ := createProject("project-key-1", "A Different Project")
	if !reusedProject.IsError {
		t.Fatalf("project_create with a reused key but different content: want idempotency_key_reused, got success")
	}
}

// TestGapClosingToolsOverRealStreamableHTTP exercises the four tools
// added after Phase 3's live dogfood step found real gaps in the
// original tool surface: project_create, features_list,
// ticket_relationships, and ticket_associations. None of these were in
// the plan's original MCP tool table — they close usability gaps found
// by actually using the tools, not regressions against a committed
// scope.
func TestGapClosingToolsOverRealStreamableHTTP(t *testing.T) {
	backend, ticketRef := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	// project_create
	createRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "project_create", Arguments: map[string]any{
		"key": "DEF", "title": "Second Project", "description": "Created live via project_create",
	}})
	if err != nil {
		t.Fatalf("CallTool project_create: %v", err)
	}
	if createRes.IsError {
		t.Fatalf("project_create returned a tool error: %+v", createRes.Content)
	}
	createdProject := decodeResult[domain.Project](t, createRes)
	if createdProject.Key != "DEF" || createdProject.Title != "Second Project" {
		t.Errorf("project_create result = %+v, want key=DEF title=%q", createdProject, "Second Project")
	}

	// features_list — ABC already has its system-created General
	// feature (ADR 0001) from newTestBackend's CreateProject call.
	featuresRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "features_list", Arguments: map[string]any{"project_key": "ABC"}})
	if err != nil {
		t.Fatalf("CallTool features_list: %v", err)
	}
	if featuresRes.IsError {
		t.Fatalf("features_list returned a tool error: %+v", featuresRes.Content)
	}
	features := decodeResult[FeaturesListOutput](t, featuresRes)
	if len(features.Features) < 1 {
		t.Errorf("features_list result = %+v, want at least the project's General feature", features)
	}

	// ticket_relationships: create a second ticket, link it, then read
	// the relationship back.
	secondCreateRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_create", Arguments: map[string]any{
		"project_key": "ABC", "type": "bug", "title": "A second ticket",
	}})
	if err != nil || secondCreateRes.IsError {
		t.Fatalf("CallTool ticket_create (second ticket): err=%v isError=%v", err, secondCreateRes != nil && secondCreateRes.IsError)
	}
	secondTicket := decodeTicketResult(t, secondCreateRes)

	linkRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_link", Arguments: map[string]any{
		"ref": ticketRef, "type": "parent_of", "target": secondTicket.Ref,
	}})
	if err != nil || linkRes.IsError {
		t.Fatalf("CallTool ticket_link (parent_of): err=%v isError=%v", err, linkRes != nil && linkRes.IsError)
	}

	relsRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_relationships", Arguments: map[string]any{"ref": ticketRef}})
	if err != nil {
		t.Fatalf("CallTool ticket_relationships: %v", err)
	}
	if relsRes.IsError {
		t.Fatalf("ticket_relationships returned a tool error: %+v", relsRes.Content)
	}
	rels := decodeResult[RelationshipsOutput](t, relsRes)
	foundRel := false
	for _, r := range rels.Relationships {
		if r.Type == "parent_of" && r.Other == secondTicket.Ref {
			foundRel = true
		}
	}
	if !foundRel {
		t.Errorf("ticket_relationships result = %+v, want a parent_of relationship to %s", rels, secondTicket.Ref)
	}

	// ticket_associations: associate the ticket with the project's
	// General feature, then read the association back.
	assocRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_link", Arguments: map[string]any{
		"ref": ticketRef, "type": "associated_with", "target": features.Features[0].Ref,
	}})
	if err != nil || assocRes.IsError {
		t.Fatalf("CallTool ticket_link (associated_with): err=%v isError=%v", err, assocRes != nil && assocRes.IsError)
	}

	assocListRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_associations", Arguments: map[string]any{"ref": ticketRef}})
	if err != nil {
		t.Fatalf("CallTool ticket_associations: %v", err)
	}
	if assocListRes.IsError {
		t.Fatalf("ticket_associations returned a tool error: %+v", assocListRes.Content)
	}
	assocs := decodeResult[AssociationsOutput](t, assocListRes)
	foundAssoc := false
	for _, a := range assocs.Associated {
		if a == features.Features[0].Ref {
			foundAssoc = true
		}
	}
	if !foundAssoc {
		t.Errorf("ticket_associations result = %+v, want %s among them", assocs, features.Features[0].Ref)
	}
}

func mustParseTicketRef(t *testing.T, ref string) domain.Reference {
	t.Helper()
	parsed, err := domain.Parse(ref)
	if err != nil {
		t.Fatalf("parse ref %q: %v", ref, err)
	}
	return parsed
}

// TestStdioBridgeReachesSameService is Phase 0 verification gate 7,
// literally: an MCP client reaches ticket_get over a real stdio
// subprocess (mcpsrv.RunStdio + mcp.CommandTransport, same pattern the
// Step 2 spike proved), backed by mcpsrv.HTTPBackend against a real
// httptest server running internal/httpapi + internal/service - the
// same service code the in-memory test above exercises via
// InProcessBackend, just reached through HTTP as ADR 0006 requires for
// the real bridge. It also drives a ticket_create through the same
// subprocess, which is the default-install "tickets mcp" workflow
// product spec §16 describes (Codex/Claude Code using MCP for the
// representative ticket workflow) — without this, the bridge's write
// path had no coverage at all.
//
// It re-executes this test binary as a subprocess (the standard Go
// idiom for testing subprocess behavior - see os/exec's own tests),
// rather than requiring cmd/tickets to be built first.
func TestStdioBridgeReachesSameService(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)

	ctx := context.Background()
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeChore, Title: "Ticket for stdio bridge test",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	// Anonymous read enabled: ticket_get exercises the bridge's
	// unauthenticated read path. ticket_create below needs a real agent
	// bearer token regardless of this setting (ADR 0004 forbids
	// anonymous writes) — that token is what proves apiclient.Client's
	// Token field actually reaches the wire as a correctly-named
	// Authorization header, not just that the field compiles. Without
	// this, a misspelled header name would
	// still pass every other test in this package: the in-memory tests
	// never touch HTTPBackend, and the real-HTTP tests
	// (TestToolsOverRealStreamableHTTP*) exercise InProcessBackend
	// through NewStreamableHTTPHandler, not the stdio bridge.
	apiServer := httptest.NewServer(httpapi.NewHandler(svc, true))
	defer apiServer.Close()

	agent, err := svc.CreateAgent(ctx, service.CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	rawToken, _, err := svc.CreateAgentToken(ctx, agent.Ref, "", nil, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(self, "-test.run=^TestStdioHelperProcess$")
	cmd.Env = append(os.Environ(),
		"TICKETS_MCP_STDIO_HELPER=1",
		"TICKETS_MCP_HELPER_API_URL="+apiServer.URL+"/api/v1",
		"TICKETS_MCP_HELPER_API_TOKEN="+rawToken,
	)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect over stdio: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ticket.Ref}})
	if err != nil {
		t.Fatalf("CallTool over stdio: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_get over stdio returned a tool error: %+v", res.Content)
	}
	got := decodeTicketResult(t, res)
	if got.Ref != ticket.Ref || got.Title != ticket.Title {
		t.Errorf("stdio ticket_get = %+v, want ref=%q title=%q", got, ticket.Ref, ticket.Title)
	}

	// This is the write path — it needs the agent bearer token above to
	// pass httpapi's requireEditor gate. If apiclient.Client's Token
	// weren't wired onto the outgoing Authorization header correctly,
	// this would fail as an unauthenticated write, not as a decoding
	// error.
	createRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_create", Arguments: map[string]any{
		"project_key": "ABC", "type": "task", "title": "Created over the stdio bridge",
	}})
	if err != nil {
		t.Fatalf("CallTool ticket_create over stdio: %v", err)
	}
	if createRes.IsError {
		t.Fatalf("ticket_create over stdio returned a tool error: %+v", createRes.Content)
	}
	created := decodeTicketResult(t, createRes)
	if created.Ref == "" {
		t.Fatalf("ticket_create over stdio returned no ref")
	}
}

// TestStdioHelperProcess is not a real test: it's the subprocess body
// TestStdioBridgeReachesSameService launches via `-test.run`. It skips
// immediately unless TICKETS_MCP_STDIO_HELPER=1 is set, so `go test
// ./...` never runs it as an ordinary test.
func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("TICKETS_MCP_STDIO_HELPER") != "1" {
		t.Skip("only runs as a stdio-bridge subprocess helper")
	}
	backend := &HTTPBackend{Client: &apiclient.Client{
		BaseURL: os.Getenv("TICKETS_MCP_HELPER_API_URL"),
		Token:   os.Getenv("TICKETS_MCP_HELPER_API_TOKEN"),
	}}
	if err := RunStdio(context.Background(), backend); err != nil {
		fmt.Fprintln(os.Stderr, "helper RunStdio failed:", err)
		os.Exit(1)
	}
}
