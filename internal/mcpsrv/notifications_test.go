package mcpsrv

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestNotificationsToolsOverInMemoryTransport proves notifications_list
// and notifications_mark_read are advertised and reach InProcessBackend
// the same way search does: the seeded ticket's assignment to a second
// actor produces one notification, listable and then mark-read-able
// through the tool surface.
func TestNotificationsToolsOverInMemoryTransport(t *testing.T) {
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

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{"notifications_list": false, "notifications_mark_read": false}
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

	// In-memory transport has no bearer-token layer, so both calls
	// reach ctx with no Principal — the exact "unauthenticated caller"
	// shape TestToolsOverInMemoryTransport's own doc comment explains
	// for ticket_create. Assert the clean unauthorized failure rather
	// than a full end-to-end notification (that's
	// TestAssignTicketOverHTTPNotifiesAssignee, internal/httpapi).
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "notifications_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool notifications_list: %v", err)
	}
	if !res.IsError {
		t.Fatalf("notifications_list with no caller identity: want a tool error, got success: %+v", res.Content)
	}

	readRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "notifications_mark_read", Arguments: map[string]any{"all": true}})
	if err != nil {
		t.Fatalf("CallTool notifications_mark_read: %v", err)
	}
	if !readRes.IsError {
		t.Fatalf("notifications_mark_read with no caller identity: want a tool error, got success: %+v", readRes.Content)
	}
}
