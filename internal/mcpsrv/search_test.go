package mcpsrv

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSearchToolOverInMemoryTransport proves the search tool is
// advertised and reaches InProcessBackend/internal/service the same
// way tickets_list does, finding the seeded ticket by a substring of
// its title.
func TestSearchToolOverInMemoryTransport(t *testing.T) {
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

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "search" {
			found = true
		}
	}
	if !found {
		t.Fatalf("search tool not advertised")
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "parser"}})
	if err != nil {
		t.Fatalf("CallTool search: %v", err)
	}
	if res.IsError {
		t.Fatalf("search returned a tool error: %+v", res.Content)
	}
	out := decodeResult[SearchOutput](t, res)
	if len(out.Hits) != 1 || out.Hits[0].Ref != ref {
		t.Errorf("search(%q) = %+v, want exactly one hit for %q", "parser", out, ref)
	}

	kindRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "parser", "kind": []string{"feature"}}})
	if err != nil {
		t.Fatalf("CallTool search with kind filter: %v", err)
	}
	kindOut := decodeResult[SearchOutput](t, kindRes)
	if len(kindOut.Hits) != 0 {
		t.Errorf("search(%q, kind=feature) = %+v, want 0 hits (the match is a ticket)", "parser", kindOut)
	}
}
