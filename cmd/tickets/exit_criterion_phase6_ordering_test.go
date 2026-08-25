package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/mcpsrv"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestExitCriterionPhase6SameOrderAcrossLayers is plan.md §16
// criterion 5, closed out at Phase 6 Step 11:
// "Priorities and manual positions produce the same deterministic
// order in API, CLI, MCP, and UI." docs/mvp-acceptance.md row 5 flagged
// this as implemented-but-untested — every layer's ordering was
// covered individually (internal/store/tickets_list_test.go,
// cmd/tickets/exit_criterion_phase3_test.go) but nothing asserted the
// *same* data comes back in the *same* order across layers in one run.
//
// This seeds three same-priority tickets, reorders one of them with a
// manual position move (so the ordering under test genuinely depends
// on ADR 0011's position column, not just priority rank), then reads
// the list back through the HTTP API, the CLI's --json output, and a
// real MCP client's tickets_list tool call, asserting all three
// return the tickets in the identical order.
//
// The web UI is not re-exercised here: it calls this exact same HTTP
// endpoint (web/src/api/tickets.ts) and renders the response rows
// as-is — grep confirms no client-side sort/reorder of the ticket list
// anywhere under web/src — so HTTP-order correctness is UI-order
// correctness by construction, not a fourth thing to independently
// verify.
func TestExitCriterionPhase6SameOrderAcrossLayers(t *testing.T) {
	isolateClientEnv(t)

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
	setupActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}

	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Ordering"}, setupActor, "setup-cid", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Three same-priority tickets, created in this order, then T3
	// manually reordered to sit right after T1 — so the expected final
	// order (T1, T3, T2) depends on the manual position move, not just
	// creation order or priority rank.
	refs := make([]string, 3)
	for i, title := range []string{"T1", "T2", "T3"} {
		tk, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
			ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: title, Priority: domain.PriorityHigh,
		}, setupActor, "setup-cid-"+title, "", "")
		if err != nil {
			t.Fatalf("create ticket %s: %v", title, err)
		}
		refs[i] = tk.Ref
	}
	t1, t2, t3 := refs[0], refs[1], refs[2]

	t3Ref, err := domain.Parse(t3)
	if err != nil {
		t.Fatalf("parse t3 ref: %v", err)
	}
	t3Ticket, err := svc.GetTicket(ctx, t3Ref)
	if err != nil {
		t.Fatalf("get t3: %v", err)
	}
	t1Ref, err := domain.Parse(t1)
	if err != nil {
		t.Fatalf("parse t1 ref: %v", err)
	}
	if _, err := svc.ReorderTicket(ctx, service.ReorderTicketRequest{
		Ref: t3Ref, AfterRef: &t1Ref, ExpectedVersion: t3Ticket.Version,
	}, setupActor, "setup-cid-reorder"); err != nil {
		t.Fatalf("reorder t3 after t1: %v", err)
	}
	want := []string{t1, t3, t2}

	agent, err := svc.CreateAgent(ctx, service.CreateAgentRequest{Name: "ordering-agent"}, setupActor, "setup-cid")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	token, _, err := svc.CreateAgentToken(ctx, agent.Ref, "", nil, setupActor, "setup-cid")
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	ts := httptest.NewServer(newRootHandler(svc, false))
	t.Cleanup(ts.Close)
	apiURL := ts.URL + "/api/v1"
	mcpURL := ts.URL + "/mcp"

	// --- HTTP API ---
	httpClient := &apiclient.Client{BaseURL: apiURL, Token: token}
	httpPage, err := httpClient.ListTickets(ctx, "ABC", "priority_queue", 0, "")
	if err != nil {
		t.Fatalf("HTTP ListTickets: %v", err)
	}
	assertSameOrder(t, "HTTP API", refsOf(httpPage.Tickets), want)

	// --- CLI JSON ---
	t.Setenv("TICKETS_API_TOKEN", token)
	out := captureStdout(t, func() {
		if err := runTicket([]string{"list", "--url", apiURL, "--project", "ABC", "--json"}); err != nil {
			t.Fatalf("runTicket list: %v", err)
		}
	})
	var cliPage apiclient.TicketsPage
	if err := json.Unmarshal([]byte(out), &cliPage); err != nil {
		t.Fatalf("unmarshal CLI output: %v (raw: %s)", err, out)
	}
	assertSameOrder(t, "CLI --json", refsOf(cliPage.Tickets), want)

	// --- MCP tickets_list, over a real protocol client ---
	client := mcp.NewClient(&mcp.Implementation{Name: "ordering-test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   mcpURL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	defer func() { _ = session.Close() }()
	listRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tickets_list", Arguments: map[string]any{"project_key": "ABC"}})
	if err != nil || listRes.IsError {
		t.Fatalf("tickets_list: err=%v isError=%v content=%+v", err, listRes != nil && listRes.IsError, listRes)
	}
	mcpOut := decodeToolResult[mcpsrv.TicketsListOutput](t, listRes)
	mcpRefs := make([]string, len(mcpOut.Tickets))
	for i, tk := range mcpOut.Tickets {
		mcpRefs[i] = tk.Ref
	}
	assertSameOrder(t, "MCP tickets_list", mcpRefs, want)
}

func refsOf(tickets []apiclient.TicketCompact) []string {
	refs := make([]string, len(tickets))
	for i, tk := range tickets {
		refs[i] = tk.Ref
	}
	return refs
}

func assertSameOrder(t *testing.T, layer string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d tickets %v, want %d %v", layer, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s order = %v, want %v (differs at index %d: got %s, want %s)", layer, got, want, i, got[i], want[i])
			return
		}
	}
}
