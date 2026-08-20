package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerTokenTransport injects a fixed Authorization: Bearer header on
// every outgoing request — mcp.StreamableClientTransport has no header
// field of its own, only HTTPClient (ADR 0004/0006).
type bearerTokenTransport struct{ token string }

func (t bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// TestRootHandlerComposition exercises the exact mux newRootHandler
// builds — the same one runServer ships — rather than a differently-
// shaped one. Confirms /healthz, /api/v1/*, and specifically /mcp (not
// the root, which is where an earlier, narrower test mounted the MCP
// handler) all work through one composed handler.
func TestRootHandlerComposition(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	svc := service.New(st)

	ctx := context.Background()
	actor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, actor, "test-correlation-id", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T",
	}, actor, "test-correlation-id", "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	// /healthz and GET /api/v1/tickets/{ref} only need anonymous read
	// enabled; /mcp requires a real agent bearer token regardless of
	// that toggle (ADR 0004/0006 — MCP's auth is independent of
	// httpapi's anonymous-read setting).
	agent, err := svc.CreateAgent(ctx, service.CreateAgentRequest{Name: "codex"}, actor, "test-correlation-id")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	rawToken, _, err := svc.CreateAgentToken(ctx, agent.Ref, "", nil, actor, "test-correlation-id")
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	ts := httptest.NewServer(newRootHandler(svc, true))
	defer ts.Close()

	// --- /healthz through the composed mux ---
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// --- /api/v1/tickets/{ref} through the composed mux ---
	resp, err = http.Get(ts.URL + "/api/v1/tickets/" + ticket.Ref)
	if err != nil {
		t.Fatalf("GET /api/v1/tickets/%s: %v", ticket.Ref, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/api/v1/tickets/%s status = %d, want 200", ticket.Ref, resp.StatusCode)
	}
	_ = resp.Body.Close()

	// --- /mcp specifically (not the root) through the composed mux ---
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTokenTransport{token: rawToken}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect to /mcp: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ticket.Ref}})
	if err != nil {
		t.Fatalf("CallTool ticket_get over /mcp: %v", err)
	}
	if res.IsError {
		t.Fatalf("ticket_get over /mcp returned a tool error: %+v", res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var got domain.Ticket
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	if got.Ref != ticket.Ref {
		t.Errorf("ticket_get over /mcp returned ref %q, want %q", got.Ref, ticket.Ref)
	}

	// --- confirm /mcp does NOT also answer at the root ---
	// (a regression here would mean the mux's precedence rules changed
	// and the "/" catch-all started swallowing the more specific "/mcp"
	// pattern, or vice versa)
	resp, err = http.Get(ts.URL + "/nonexistent-path")
	if err != nil {
		t.Fatalf("GET /nonexistent-path: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/nonexistent-path status = %d, want 404 (from httpapi's mux, not swallowed by /mcp)", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
