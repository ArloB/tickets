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

	// --- an unmatched non-API path reaches the embedded web UI's SPA
	// fallback, not a 404 --- (Phase 4: httpapi's "/" now serves
	// web.Dist's build with a single-page-app fallback, so a
	// client-side route the Go server has never heard of, like a
	// deep-linked /projects/ABC, reaches the app shell instead of
	// erroring with 404). This test suite runs against whatever
	// web/dist/ happens to contain locally — .gitkeep only on a bare
	// `go test` checkout (200 is then impossible; a real build hasn't
	// been produced), a full build once `task web:build`/`npm run
	// build` has run (200). Both are the SPA-fallback code path
	// actually running; internal/httpapi's own newStaticHandler tests
	// (static_test.go), which inject a fake dist so they don't depend
	// on build state, are what actually verify serveIndex's content
	// and headers — this test only needs to prove the routing
	// precedence: reaching the fallback at all, never a 404.
	resp, err = http.Get(ts.URL + "/nonexistent-path")
	if err != nil {
		t.Fatalf("GET /nonexistent-path: %v", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("/nonexistent-path status = 404, want the SPA-fallback code path to run (200 with a build present, 500 without) — a 404 here would mean routing precedence regressed back to pre-Phase-4 behavior")
	}
	_ = resp.Body.Close()

	// --- confirm /mcp does NOT also answer at the root, and that an
	// unmatched /api/v1/* path is still a real 404 from httpapi's API
	// subtree, never silently swallowed by the "/mcp" pattern or by
	// the SPA fallback --- (a regression here would mean the mux's
	// precedence rules changed and either "/" or "/mcp" started
	// shadowing the more specific "/api/v1/" pattern, or vice versa)
	resp, err = http.Get(ts.URL + "/api/v1/nonexistent-path")
	if err != nil {
		t.Fatalf("GET /api/v1/nonexistent-path: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/api/v1/nonexistent-path status = %d, want 404 (from httpapi's protected mux, not swallowed by /mcp or the SPA fallback)", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
