package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

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
// and calls ticket_get and ticket_create, reaching internal/service
// through the same backend the HTTP-mounted endpoint uses.
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
	created := decodeTicketResult(t, createRes)
	if created.Ref != "ABC-2" {
		t.Errorf("ticket_create returned ref %q, want ABC-2", created.Ref)
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

// TestToolsOverRealStreamableHTTP closes the gap between "MCP over
// Streamable HTTP" (gate 7's literal wording) and the in-memory-
// transport test above: this one runs NewStreamableHTTPHandler behind
// a real httptest.Server (actual TCP, actual HTTP requests) and
// connects with mcp.StreamableClientTransport, exactly as
// cmd/tickets' `server` subcommand mounts it at /mcp.
func TestToolsOverRealStreamableHTTP(t *testing.T) {
	backend, ref := newTestBackend(t)
	httpHandler := NewStreamableHTTPHandler(backend)
	ts := httptest.NewServer(httpHandler)
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
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

// TestStdioBridgeReachesSameService is Phase 0 verification gate 7,
// literally: an MCP client reaches ticket_get over a real stdio
// subprocess (mcpsrv.RunStdio + mcp.CommandTransport, same pattern the
// Step 2 spike proved), backed by mcpsrv.HTTPBackend against a real
// httptest server running internal/httpapi + internal/service - the
// same service code the in-memory test above exercises via
// InProcessBackend, just reached through HTTP as ADR 0006 requires for
// the real bridge.
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

	apiServer := httptest.NewServer(httpapi.NewHandler(svc))
	defer apiServer.Close()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(self, "-test.run=^TestStdioHelperProcess$")
	cmd.Env = append(os.Environ(),
		"TICKETS_MCP_STDIO_HELPER=1",
		"TICKETS_MCP_HELPER_API_URL="+apiServer.URL+"/api/v1",
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
}

// TestStdioHelperProcess is not a real test: it's the subprocess body
// TestStdioBridgeReachesSameService launches via `-test.run`. It skips
// immediately unless TICKETS_MCP_STDIO_HELPER=1 is set, so `go test
// ./...` never runs it as an ordinary test.
func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("TICKETS_MCP_STDIO_HELPER") != "1" {
		t.Skip("only runs as a stdio-bridge subprocess helper")
	}
	backend := &HTTPBackend{BaseURL: os.Getenv("TICKETS_MCP_HELPER_API_URL")}
	if err := RunStdio(context.Background(), backend); err != nil {
		fmt.Fprintln(os.Stderr, "helper RunStdio failed:", err)
		os.Exit(1)
	}
}
