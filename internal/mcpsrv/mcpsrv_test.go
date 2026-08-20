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
	svc := service.New(st)

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

func decodeTicketResult(t *testing.T, res *mcp.CallToolResult) domain.Ticket {
	t.Helper()
	if res == nil || res.StructuredContent == nil {
		t.Fatalf("no structured content in result: %+v", res)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-marshal structured content: %v", err)
	}
	var ticket domain.Ticket
	if err := json.Unmarshal(b, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v (raw: %s)", err, b)
	}
	return ticket
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
	want := map[string]bool{"project_get": false, "ticket_get": false, "ticket_create": false}
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
	svc := service.New(st)

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
