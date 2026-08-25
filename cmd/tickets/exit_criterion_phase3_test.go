package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/mcpsrv"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestExitCriterionPhase3Workflow is Phase 3's automated exit-criterion
// gate (plan.md §14's Phase 3 bullet, product spec §16's "find assigned
// work, read linked context, start ticket, comment, create decision,
// and complete ticket" workflow, and "two different agent tokens
// create separately attributed audit events"). It drives that workflow
// three separate ways against one shared server, each with its own
// agent token, and asserts each leg's writes are attributed to that
// leg's own actor.
//
// One deliberate substitution from the plan's literal wording: "find
// assigned work" implies filtering by assignee, but Phase 3 has no
// assignee filter on tickets_list/ticket list and no ticket_assign MCP
// tool (only the CLI has `ticket assign`, and nothing in the MCP tool
// surface reaches it). So this test seeds each leg's ticket already
// assigned to that leg's own agent (via *service.Service directly, as
// setup, not as a step under test), and "find" means what
// tickets_list/ticket list can actually do: list the project's queue
// and locate the ticket. If a future phase adds an assignee filter or
// an assign tool, this substitution is the first thing to revisit.
//
// The three legs intentionally operate on three separate tickets, not
// one shared ticket passed hand-to-hand — each leg's identity finds
// and completes *its own* assigned work, mirroring how three real
// agents would actually use the system concurrently, and avoiding the
// awkwardness of three different actors all being "the assignee" of
// one ticket at once. What's shared is the audit-attribution
// assertion: three distinct actors, not the same entity.
func TestExitCriterionPhase3Workflow(t *testing.T) {
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
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Exit Criterion"}, setupActor, "setup-cid", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ts := httptest.NewServer(newRootHandler(svc, false))
	t.Cleanup(ts.Close)
	apiURL := ts.URL + "/api/v1"
	mcpURL := ts.URL + "/mcp"

	httpToken, httpActor, httpTicket, httpLinkedDecision := seedAgentAndAssignedTicket(t, svc, setupActor, "http-agent", "Fix the HTTPBackend leg's bug")
	inProcessToken, inProcessActor, inProcessTicket, inProcessLinkedDecision := seedAgentAndAssignedTicket(t, svc, setupActor, "inprocess-agent", "Fix the InProcessBackend leg's bug")
	cliToken, cliActor, cliTicket, cliLinkedDecision := seedAgentAndAssignedTicket(t, svc, setupActor, "cli-agent", "Fix the CLI leg's bug")

	t.Run("HTTPBackend", func(t *testing.T) {
		runWorkflowViaHTTPBackend(t, apiURL, httpToken, httpTicket, httpLinkedDecision)
		assertSoleAttributedActor(t, st, httpTicket, httpActor)
	})

	t.Run("InProcessBackend_over_real_MCP_client", func(t *testing.T) {
		runWorkflowViaRealMCPClient(t, mcpURL, inProcessToken, inProcessTicket, inProcessLinkedDecision)
		assertSoleAttributedActor(t, st, inProcessTicket, inProcessActor)
	})

	t.Run("CLI_json", func(t *testing.T) {
		t.Setenv("TICKETS_API_TOKEN", cliToken)
		runWorkflowViaCLI(t, apiURL, cliTicket, cliLinkedDecision)
		assertSoleAttributedActor(t, st, cliTicket, cliActor)
	})
}

// seedAgentAndAssignedTicket creates one agent actor + bearer token, one
// pre-existing decision (standing in for context recorded before this
// ticket existed), and one ticket already assigned to the agent whose
// description references that decision via "#ABC-D1"-style Markdown
// (ADR 0015's mention scanning) — the out-of-band substitutions for
// "find assigned work" and "read linked context" described in
// TestExitCriterionPhase3Workflow's doc comment. Returns the raw
// token, the agent's ActorRef, the ticket's public reference, and the
// linked decision's public reference.
func seedAgentAndAssignedTicket(t *testing.T, svc *service.Service, setupActor domain.ActorRef, agentName, ticketTitle string) (token string, actor domain.ActorRef, ticketRef, linkedDecisionRef string) {
	t.Helper()
	ctx := context.Background()

	agent, err := svc.CreateAgent(ctx, service.CreateAgentRequest{Name: agentName}, setupActor, "setup-cid")
	if err != nil {
		t.Fatalf("CreateAgent(%s): %v", agentName, err)
	}
	raw, _, err := svc.CreateAgentToken(ctx, agent.Ref, "", nil, setupActor, "setup-cid")
	if err != nil {
		t.Fatalf("CreateAgentToken(%s): %v", agentName, err)
	}

	priorDecision, err := svc.CreateDecision(ctx, service.CreateDecisionRequest{
		ProjectKey: "ABC", Title: "Prior context for " + ticketTitle,
		Context: "Established before this ticket existed", Decision: "Use the existing approach", Rationale: "Already validated",
	}, setupActor, "setup-cid", "", "")
	if err != nil {
		t.Fatalf("CreateDecision(%s): %v", ticketTitle, err)
	}

	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: ticketTitle,
		Description: "See #" + priorDecision.Ref + " for prior context.", Priority: domain.PriorityMedium,
	}, setupActor, "setup-cid", "", "")
	if err != nil {
		t.Fatalf("CreateTicket(%s): %v", ticketTitle, err)
	}
	parsedRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse seeded ticket ref %q: %v", ticket.Ref, err)
	}
	assignee := agent.Ref
	if _, err := svc.AssignTicket(ctx, service.AssignTicketRequest{
		Ref: parsedRef, Assignee: &assignee, ExpectedVersion: ticket.Version,
	}, setupActor, "setup-cid"); err != nil {
		t.Fatalf("AssignTicket(%s): %v", ticketTitle, err)
	}

	return raw, agent.Ref, ticket.Ref, priorDecision.Ref
}

// assertSoleAttributedActor is this test's discriminating check
// (product spec §16: "two different agent tokens create separately
// attributed audit events"). It does not require every event on
// ticketRef to be wantActor — seedAgentAndAssignedTicket's own setup
// (CreateTicket/AssignTicket) legitimately runs as setupActor
// (human:local) and leaves its own audit trail first. What it does
// require: wantActor's own writes are present (the bearer token's
// identity actually reached the audit log — withCallerActor/mcpActor
// threaded it through, not silently attributing to setupActor or
// falling back to some default), and no *other* agent identity ever
// appears on this ticket — each leg seeded its own separate ticket
// specifically so a misattribution to the wrong leg's agent would show
// up here as agent cross-contamination, not just be masked by shared
// history.
func assertSoleAttributedActor(t *testing.T, st *store.Store, ticketRef string, wantActor domain.ActorRef) {
	t.Helper()
	ctx := context.Background()
	parsedRef, err := domain.Parse(ticketRef)
	if err != nil {
		t.Fatalf("parse ticket ref %q: %v", ticketRef, err)
	}
	row, err := store.GetTicketByRef(ctx, st.DB(), parsedRef)
	if err != nil {
		t.Fatalf("GetTicketByRef(%s): %v", ticketRef, err)
	}
	events, err := store.ListAuditEvents(ctx, st.DB(), row.ID)
	if err != nil {
		t.Fatalf("ListAuditEvents(%s): %v", ticketRef, err)
	}
	if len(events) == 0 {
		t.Fatalf("ticket %s has no audit events, want at least the workflow's own writes", ticketRef)
	}
	seen := make(map[string]bool)
	sawWantActor := false
	for _, e := range events {
		actorRef, err := store.GetActorRefByID(ctx, st.DB(), e.ActorID)
		if err != nil {
			t.Fatalf("resolve actor for audit event %d: %v", e.ID, err)
		}
		seen[actorRef.String()] = true
		if actorRef == wantActor {
			sawWantActor = true
		}
		if actorRef.Kind == domain.ActorAgent && actorRef != wantActor {
			t.Errorf("ticket %s has an audit event attributed to agent %s, want only %s (or non-agent setup activity)", ticketRef, actorRef, wantActor)
		}
	}
	if !sawWantActor {
		t.Errorf("ticket %s audit events are attributed to %v, want %s to appear among them", ticketRef, mapKeys(seen), wantActor)
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// decodeToolResult mirrors internal/mcpsrv's own test helper of the
// same shape (unexported there, so not importable from this package).
func decodeToolResult[T any](t *testing.T, res *mcp.CallToolResult) T {
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

// runWorkflowViaHTTPBackend drives the representative workflow through
// mcpsrv.HTTPBackend's Go methods directly — the same backend
// `tickets mcp`'s stdio bridge wraps (RunStdio), called here without
// the stdio/subprocess machinery around it, since TestStdioBridgeReachesSameService
// in internal/mcpsrv already covers that transport layer; this test's
// job is the workflow, not re-proving the bridge connects.
func runWorkflowViaHTTPBackend(t *testing.T, apiURL, token, ticketRef, linkedDecisionRef string) {
	t.Helper()
	backend := &mcpsrv.HTTPBackend{Client: &apiclient.Client{BaseURL: apiURL, Token: token}}
	ctx := context.Background()

	// 1. find assigned work
	list, err := backend.ListTickets(ctx, "ABC", "priority_queue", mcpsrv.TicketListFilters{}, 0, "")
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	found := false
	for _, tk := range list.Tickets {
		if tk.Ref == ticketRef {
			found = true
		}
	}
	if !found {
		t.Fatalf("tickets_list did not include the seeded assigned ticket %s: %+v", ticketRef, list)
	}

	// 2. read linked context: the ticket's description names a prior
	// decision by reference; follow it.
	full, err := backend.GetTicket(ctx, ticketRef)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if !strings.Contains(full.Description, linkedDecisionRef) {
		t.Fatalf("ticket %s description = %q, want it to reference %s", ticketRef, full.Description, linkedDecisionRef)
	}
	linked, err := backend.GetDecision(ctx, linkedDecisionRef)
	if err != nil {
		t.Fatalf("GetDecision(%s): %v", linkedDecisionRef, err)
	}
	if linked.Ref != linkedDecisionRef {
		t.Fatalf("GetDecision(%s) returned ref %q", linkedDecisionRef, linked.Ref)
	}

	// 3. start ticket
	inProgress := "in_progress"
	started, err := backend.UpdateTicket(ctx, mcpsrv.UpdateTicketInput{Ref: ticketRef, Status: &inProgress, ExpectedVersion: full.Version})
	if err != nil {
		t.Fatalf("UpdateTicket(in_progress): %v", err)
	}

	// 4. comment
	if _, err := backend.AddComment(ctx, ticketRef, "Starting work via the HTTPBackend leg.", ""); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	// 5. create decision, link it to the ticket
	dec, err := backend.CreateDecision(ctx, mcpsrv.CreateDecisionInput{
		ProjectKey: "ABC", Title: "Use approach A", Context: "Two approaches considered", Decision: "Approach A", Rationale: "Simpler",
	})
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	if err := backend.AddAssociation(ctx, ticketRef, dec.Ref); err != nil {
		t.Fatalf("AddAssociation: %v", err)
	}

	// 6. complete ticket
	done := "done"
	if _, err := backend.UpdateTicket(ctx, mcpsrv.UpdateTicketInput{Ref: ticketRef, Status: &done, ExpectedVersion: started.Version}); err != nil {
		t.Fatalf("UpdateTicket(done): %v", err)
	}
}

// runWorkflowViaRealMCPClient drives the same workflow through a real
// mcp.Client connected over Streamable HTTP to the server's InProcessBackend-
// backed /mcp endpoint — a protocol-conformance stand-in for a real
// Codex/Claude Code host, both of which are plain MCP clients (plan.md's
// own framing).
func runWorkflowViaRealMCPClient(t *testing.T, mcpURL, token, ticketRef, linkedDecisionRef string) {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "exit-criterion-test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   mcpURL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	// 1. find assigned work
	listRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tickets_list", Arguments: map[string]any{"project_key": "ABC"}})
	if err != nil || listRes.IsError {
		t.Fatalf("tickets_list: err=%v isError=%v content=%+v", err, listRes != nil && listRes.IsError, listRes)
	}
	list := decodeToolResult[mcpsrv.TicketsListOutput](t, listRes)
	found := false
	for _, tk := range list.Tickets {
		if tk.Ref == ticketRef {
			found = true
		}
	}
	if !found {
		t.Fatalf("tickets_list did not include the seeded assigned ticket %s: %+v", ticketRef, list)
	}

	// 2. read linked context: the ticket's description names a prior
	// decision by reference; follow it.
	getRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_get", Arguments: map[string]any{"ref": ticketRef}})
	if err != nil || getRes.IsError {
		t.Fatalf("ticket_get: err=%v isError=%v content=%+v", err, getRes != nil && getRes.IsError, getRes)
	}
	full := decodeToolResult[domain.Ticket](t, getRes)
	if !strings.Contains(full.Description, linkedDecisionRef) {
		t.Fatalf("ticket %s description = %q, want it to reference %s", ticketRef, full.Description, linkedDecisionRef)
	}
	recordGetRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": linkedDecisionRef}})
	if err != nil || recordGetRes.IsError {
		t.Fatalf("record_get(%s): err=%v isError=%v content=%+v", linkedDecisionRef, err, recordGetRes != nil && recordGetRes.IsError, recordGetRes)
	}
	linked := decodeToolResult[domain.Decision](t, recordGetRes)
	if linked.Ref != linkedDecisionRef {
		t.Fatalf("record_get(%s) returned ref %q", linkedDecisionRef, linked.Ref)
	}

	// 3. start ticket
	startRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_update", Arguments: map[string]any{
		"ref": ticketRef, "status": "in_progress", "expected_version": full.Version,
	}})
	if err != nil || startRes.IsError {
		t.Fatalf("ticket_update(in_progress): err=%v isError=%v content=%+v", err, startRes != nil && startRes.IsError, startRes)
	}
	started := decodeToolResult[mcpsrv.TicketWriteResult](t, startRes)

	// 4. comment
	commentRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_comment", Arguments: map[string]any{
		"ref": ticketRef, "body": "Starting work via the real MCP client leg.",
	}})
	if err != nil || commentRes.IsError {
		t.Fatalf("ticket_comment: err=%v isError=%v content=%+v", err, commentRes != nil && commentRes.IsError, commentRes)
	}

	// 5. create decision, link it to the ticket
	createRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
		"project_key": "ABC", "title": "Use approach B", "context": "Two approaches considered", "decision": "Approach B", "rationale": "Faster",
	}})
	if err != nil || createRes.IsError {
		t.Fatalf("record_create: err=%v isError=%v content=%+v", err, createRes != nil && createRes.IsError, createRes)
	}
	dec := decodeToolResult[mcpsrv.DecisionWriteResult](t, createRes)

	linkRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_link", Arguments: map[string]any{
		"ref": ticketRef, "type": "associated_with", "target": dec.Ref,
	}})
	if err != nil || linkRes.IsError {
		t.Fatalf("ticket_link: err=%v isError=%v content=%+v", err, linkRes != nil && linkRes.IsError, linkRes)
	}

	// 6. complete ticket
	doneRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_update", Arguments: map[string]any{
		"ref": ticketRef, "status": "done", "expected_version": started.Version,
	}})
	if err != nil || doneRes.IsError {
		t.Fatalf("ticket_update(done): err=%v isError=%v content=%+v", err, doneRes != nil && doneRes.IsError, doneRes)
	}
}

// runWorkflowViaCLI drives the same workflow through cmd/tickets' own
// run* functions with --json, the same entry points `tickets` the
// binary dispatches to from main() — proving product spec §16's "the
// same workflow is possible through CLI JSON" against the identical
// server the other two legs used. Assumes TICKETS_API_TOKEN is already
// set in the test's environment (t.Setenv, by the caller) — there is
// no --token flag on any client-mode command (docs/contracts/cli.md).
func runWorkflowViaCLI(t *testing.T, apiURL, ticketRef, linkedDecisionRef string) {
	t.Helper()

	// 1. find assigned work
	listOut := captureStdout(t, func() {
		if err := runTicket([]string{"list", "--url", apiURL, "--project", "ABC", "--json"}); err != nil {
			t.Fatalf("ticket list: %v", err)
		}
	})
	var list struct {
		Tickets []struct {
			Ref string `json:"ref"`
		} `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(listOut), &list); err != nil {
		t.Fatalf("decode ticket list --json: %v (raw: %s)", err, listOut)
	}
	found := false
	for _, tk := range list.Tickets {
		if tk.Ref == ticketRef {
			found = true
		}
	}
	if !found {
		t.Fatalf("ticket list did not include the seeded assigned ticket %s: %s", ticketRef, listOut)
	}

	// 2. read linked context: the ticket's description names a prior
	// decision by reference; follow it.
	getOut := captureStdout(t, func() {
		if err := runTicket([]string{"get", ticketRef, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("ticket get: %v", err)
		}
	})
	var full struct {
		Version     int64  `json:"version"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(getOut), &full); err != nil {
		t.Fatalf("decode ticket get --json: %v (raw: %s)", err, getOut)
	}
	if !strings.Contains(full.Description, linkedDecisionRef) {
		t.Fatalf("ticket %s description = %q, want it to reference %s", ticketRef, full.Description, linkedDecisionRef)
	}
	linkedOut := captureStdout(t, func() {
		if err := runDecision([]string{"get", linkedDecisionRef, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("decision get(%s): %v", linkedDecisionRef, err)
		}
	})
	var linked struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(linkedOut), &linked); err != nil {
		t.Fatalf("decode decision get --json: %v (raw: %s)", err, linkedOut)
	}
	if linked.Ref != linkedDecisionRef {
		t.Fatalf("decision get(%s) returned ref %q", linkedDecisionRef, linked.Ref)
	}

	// 3. start ticket
	startOut := captureStdout(t, func() {
		if err := runTicket([]string{"update", ticketRef, "--status", "in_progress", "--if-version", strconv.FormatInt(full.Version, 10), "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("ticket update(in_progress): %v", err)
		}
	})
	var started struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal([]byte(startOut), &started); err != nil {
		t.Fatalf("decode ticket update --json: %v (raw: %s)", err, startOut)
	}

	// 4. comment
	captureStdout(t, func() {
		if err := runComment([]string{"add", ticketRef, "--url", apiURL, "--body", "Starting work via the CLI leg.", "--json"}); err != nil {
			t.Fatalf("comment add: %v", err)
		}
	})

	// 5. create decision, link it to the ticket
	decOut := captureStdout(t, func() {
		if err := runDecision([]string{
			"create", "--url", apiURL, "--project", "ABC", "--title", "Use approach C",
			"--context", "Two approaches considered", "--decision", "Approach C", "--rationale", "Cheapest", "--json",
		}); err != nil {
			t.Fatalf("decision create: %v", err)
		}
	})
	var dec struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(decOut), &dec); err != nil {
		t.Fatalf("decode decision create --json: %v (raw: %s)", err, decOut)
	}
	captureStdout(t, func() {
		if err := runTicket([]string{"associate", ticketRef, "--target", dec.Ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("ticket associate: %v", err)
		}
	})

	// 6. complete ticket
	captureStdout(t, func() {
		if err := runTicket([]string{"update", ticketRef, "--status", "done", "--if-version", strconv.FormatInt(started.Version, 10), "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("ticket update(done): %v", err)
		}
	})
}

// bearerTransport mirrors internal/mcpsrv's own test helper of the
// same shape (unexported there, so not importable from this package).
type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}
