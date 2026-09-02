package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/httpapi"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type routeToolMapping struct {
	method  string
	pattern string
	tool    string
	exempt  string
}

var routeToolMappings = []routeToolMapping{
	{method: http.MethodPost, pattern: "/api/v1/auth/logout", exempt: "session-only, not an agent/tool-call operation"},

	{method: http.MethodPost, pattern: "/api/v1/projects", tool: "project_create"},
	{method: http.MethodPatch, pattern: "/api/v1/projects/{key}", tool: "project_update"},
	{method: http.MethodPost, pattern: "/api/v1/projects/{key}/status", tool: "project_update"},
	{method: http.MethodPost, pattern: "/api/v1/projects/{key}/tickets", tool: "ticket_create"},
	{method: http.MethodPatch, pattern: "/api/v1/tickets/{ref}", tool: "ticket_update"},
	{method: http.MethodPut, pattern: "/api/v1/tickets/{ref}", tool: "ticket_update"},
	{method: http.MethodDelete, pattern: "/api/v1/tickets/{ref}", tool: "ticket_delete"},
	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/assign", tool: "ticket_update"},
	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/move", tool: "ticket_update"},
	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/reorder", tool: "ticket_reorder"},
	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/restore", tool: "ticket_restore"},

	{method: http.MethodPost, pattern: "/api/v1/projects/{key}/features", tool: "feature_create"},
	{method: http.MethodPatch, pattern: "/api/v1/features/{ref}", tool: "feature_update"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/status", tool: "feature_update"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/reorder", tool: "feature_reorder"},
	{method: http.MethodDelete, pattern: "/api/v1/features/{ref}", tool: "feature_delete"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/restore", tool: "feature_restore"},

	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/comments", tool: "comment_create"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/comments", tool: "comment_create"},
	{method: http.MethodPost, pattern: "/api/v1/decisions/{ref}/comments", tool: "comment_create"},
	{method: http.MethodPost, pattern: "/api/v1/plans/{ref}/comments", tool: "comment_create"},
	{method: http.MethodPost, pattern: "/api/v1/documents/{ref}/comments", tool: "comment_create"},
	{method: http.MethodPost, pattern: "/api/v1/projects/{key}/comments", tool: "comment_create"},
	{method: http.MethodPatch, pattern: "/api/v1/comments/{id}", tool: "comment_update"},
	{method: http.MethodDelete, pattern: "/api/v1/comments/{id}", tool: "comment_delete"},

	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/relationships", tool: "relationship_add"},
	{method: http.MethodDelete, pattern: "/api/v1/tickets/{ref}/relationships/{type}/{target}", tool: "relationship_remove"},
	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/associations", tool: "association_add"},
	{method: http.MethodDelete, pattern: "/api/v1/tickets/{ref}/associations/{target}", tool: "association_remove"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/associations", tool: "association_add"},
	{method: http.MethodDelete, pattern: "/api/v1/features/{ref}/associations/{target}", tool: "association_remove"},
	{method: http.MethodPost, pattern: "/api/v1/decisions/{ref}/associations", tool: "association_add"},
	{method: http.MethodDelete, pattern: "/api/v1/decisions/{ref}/associations/{target}", tool: "association_remove"},
	{method: http.MethodPost, pattern: "/api/v1/plans/{ref}/associations", tool: "association_add"},
	{method: http.MethodDelete, pattern: "/api/v1/plans/{ref}/associations/{target}", tool: "association_remove"},
	{method: http.MethodPost, pattern: "/api/v1/documents/{ref}/associations", tool: "association_add"},
	{method: http.MethodDelete, pattern: "/api/v1/documents/{ref}/associations/{target}", tool: "association_remove"},

	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/links", tool: "external_link_add"},
	{method: http.MethodDelete, pattern: "/api/v1/tickets/{ref}/links/{id}", tool: "external_link_remove"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/links", tool: "external_link_add"},
	{method: http.MethodDelete, pattern: "/api/v1/features/{ref}/links/{id}", tool: "external_link_remove"},
	{method: http.MethodPost, pattern: "/api/v1/decisions/{ref}/links", tool: "external_link_add"},
	{method: http.MethodDelete, pattern: "/api/v1/decisions/{ref}/links/{id}", tool: "external_link_remove"},
	{method: http.MethodPost, pattern: "/api/v1/plans/{ref}/links", tool: "external_link_add"},
	{method: http.MethodDelete, pattern: "/api/v1/plans/{ref}/links/{id}", tool: "external_link_remove"},
	{method: http.MethodPost, pattern: "/api/v1/documents/{ref}/links", tool: "external_link_add"},
	{method: http.MethodDelete, pattern: "/api/v1/documents/{ref}/links/{id}", tool: "external_link_remove"},

	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/attachments", exempt: "no multipart transport over MCP"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/attachments", exempt: "no multipart transport over MCP"},
	{method: http.MethodPost, pattern: "/api/v1/decisions/{ref}/attachments", exempt: "no multipart transport over MCP"},
	{method: http.MethodPost, pattern: "/api/v1/plans/{ref}/attachments", exempt: "no multipart transport over MCP"},
	{method: http.MethodPost, pattern: "/api/v1/documents/{ref}/attachments", exempt: "no multipart transport over MCP"},
	{method: http.MethodPost, pattern: "/api/v1/comments/{id}/attachments", exempt: "no multipart transport over MCP"},
	{method: http.MethodPut, pattern: "/api/v1/attachments/{id}", exempt: "no multipart transport over MCP"},
	{method: http.MethodDelete, pattern: "/api/v1/attachments/{id}", exempt: "no multipart transport over MCP"},

	{method: http.MethodPost, pattern: "/api/v1/tickets/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodDelete, pattern: "/api/v1/tickets/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodPost, pattern: "/api/v1/features/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodDelete, pattern: "/api/v1/features/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodPost, pattern: "/api/v1/decisions/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodDelete, pattern: "/api/v1/decisions/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodPost, pattern: "/api/v1/plans/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodDelete, pattern: "/api/v1/plans/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodPost, pattern: "/api/v1/documents/{ref}/subscribe", tool: "subscription_update"},
	{method: http.MethodDelete, pattern: "/api/v1/documents/{ref}/subscribe", tool: "subscription_update"},

	{method: http.MethodPost, pattern: "/api/v1/notifications/read", tool: "notifications_mark_read"},

	{method: http.MethodPost, pattern: "/api/v1/projects/{key}/decisions", tool: "record_create"},
	{method: http.MethodPatch, pattern: "/api/v1/decisions/{ref}", tool: "record_update"},
	{method: http.MethodPost, pattern: "/api/v1/projects/{key}/plans", tool: "record_create"},
	{method: http.MethodPatch, pattern: "/api/v1/plans/{ref}", tool: "record_update"},
	{method: http.MethodPost, pattern: "/api/v1/projects/{key}/documents", tool: "record_create"},
	{method: http.MethodPatch, pattern: "/api/v1/documents/{ref}", tool: "record_update"},

	{method: http.MethodPost, pattern: "/api/v1/accounts", exempt: "admin-only; InProcessBackend bypasses requireAdmin (architectural, not scope-trimming)"},
	{method: http.MethodPost, pattern: "/api/v1/accounts/{username}/password", exempt: "admin-only; InProcessBackend bypasses requireAdmin (architectural, not scope-trimming)"},
	{method: http.MethodPost, pattern: "/api/v1/agents", exempt: "admin-only; InProcessBackend bypasses requireAdmin (architectural, not scope-trimming)"},
	{method: http.MethodPost, pattern: "/api/v1/agents/{name}/tokens", exempt: "admin-only; InProcessBackend bypasses requireAdmin (architectural, not scope-trimming)"},
	{method: http.MethodDelete, pattern: "/api/v1/agents/{name}/tokens/{id}", exempt: "admin-only; InProcessBackend bypasses requireAdmin (architectural, not scope-trimming)"},
	{method: http.MethodPost, pattern: "/api/v1/admin/restore", exempt: "server administration, not a ticket-tracker operation an agent needs — no multipart transport over MCP either"},
	{method: http.MethodDelete, pattern: "/api/v1/admin/restore", exempt: "server administration, not a ticket-tracker operation an agent needs"},
	{method: http.MethodPost, pattern: "/api/v1/admin/integrity/gc", exempt: "server administration, not a ticket-tracker operation an agent needs"},
}

func TestMCPToolParityWithHTTPRoutes(t *testing.T) {
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

	ts := httptest.NewServer(newRootHandler(svc, false))
	t.Cleanup(ts.Close)

	ctx := context.Background()
	setupActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
	agent, err := svc.CreateAgent(ctx, service.CreateAgentRequest{Name: "parity-check"}, setupActor, "setup-cid")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	raw, _, err := svc.CreateAgentToken(ctx, agent.Ref, "", nil, setupActor, "setup-cid")
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "parity-test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	toolsRes, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	liveTools := make(map[string]bool, len(toolsRes.Tools))
	for _, tool := range toolsRes.Tools {
		liveTools[tool.Name] = true
	}

	mappingByRoute := make(map[[2]string]routeToolMapping, len(routeToolMappings))
	for _, m := range routeToolMappings {
		key := [2]string{m.method, m.pattern}
		if _, dup := mappingByRoute[key]; dup {
			t.Fatalf("duplicate mapping entry for %s %s", m.method, m.pattern)
		}
		if m.tool == "" && m.exempt == "" {
			t.Fatalf("mapping entry for %s %s has neither tool nor exempt", m.method, m.pattern)
		}
		if m.tool != "" && m.exempt != "" {
			t.Fatalf("mapping entry for %s %s has both tool and exempt", m.method, m.pattern)
		}
		mappingByRoute[key] = m
	}

	seenRoutes := make(map[[2]string]bool, len(routeToolMappings))
	for _, r := range httpapi.RouteList() {
		if r.Method == http.MethodGet {
			continue
		}
		key := [2]string{r.Method, r.Pattern}
		seenRoutes[key] = true
		m, ok := mappingByRoute[key]
		if !ok {
			t.Errorf("route %s %s has no entry in routeToolMappings — add a tool mapping or an exempt reason", r.Method, r.Pattern)
			continue
		}
		if m.tool != "" && !liveTools[m.tool] {
			t.Errorf("route %s %s maps to tool %q, which is not in the live MCP tool surface", r.Method, r.Pattern, m.tool)
		}
	}

	for _, m := range routeToolMappings {
		key := [2]string{m.method, m.pattern}
		if !seenRoutes[key] {
			t.Errorf("routeToolMappings has a stale entry for %s %s — no such route exists", m.method, m.pattern)
		}
	}
}
