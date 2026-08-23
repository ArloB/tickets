package mcpsrv

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// forbiddenListBodyFields are full-body fields a compact list response
// must never carry (product spec §7.2's compact/detail split — see
// ProjectCompact/TicketCompact in types.go). This is a real structural
// guard, not just a naming convention: it walks the actual output
// JSON Schema every *_list tool advertises to clients, the same schema
// document an MCP client reads to decide what a response shape is.
var forbiddenListBodyFields = []string{"description", "context", "decision", "rationale", "body"}

// collectSchemaFieldNames walks a JSON Schema document (as decoded
// into map[string]any, the shape mcp.Tool.OutputSchema takes once it's
// round-tripped through the wire to a client — see mcp.Tool's doc
// comment on OutputSchema) and collects every property name it finds
// at any depth, following "properties" and "items" the way a real
// schema consumer would.
func collectSchemaFieldNames(t *testing.T, schema any) []string {
	t.Helper()
	var names []string
	var walk func(any)
	walk = func(node any) {
		m, ok := node.(map[string]any)
		if !ok {
			return
		}
		if props, ok := m["properties"].(map[string]any); ok {
			for name, sub := range props {
				names = append(names, name)
				walk(sub)
			}
		}
		if items, ok := m["items"]; ok {
			walk(items)
		}
	}
	walk(schema)
	return names
}

// TestListToolsOmitFullBodies iterates every tool whose name ends in
// "_list" and asserts its advertised output schema contains none of
// forbiddenListBodyFields at any depth. A regression here (someone
// widening ProjectCompact/TicketCompact to include a body field, or a
// future *_list tool skipping the compact type) fails this test
// without needing a human to notice the schema by eye.
func TestListToolsOmitFullBodies(t *testing.T) {
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

	checked := 0
	for _, tool := range tools.Tools {
		if !strings.HasSuffix(tool.Name, "_list") {
			continue
		}
		checked++
		fields := collectSchemaFieldNames(t, tool.OutputSchema)
		fieldSet := make(map[string]bool, len(fields))
		for _, f := range fields {
			fieldSet[f] = true
		}
		for _, forbidden := range forbiddenListBodyFields {
			if fieldSet[forbidden] {
				t.Errorf("tool %q output schema advertises full-body field %q — list tools must return compact rows only (product spec §7.2)", tool.Name, forbidden)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no *_list tool found — this test's premise (there is at least one list tool) no longer holds")
	}
}

// TestRegisterToolsInstructionsSet proves the MCP server actually
// advertises non-empty Instructions (product spec §7.1) rather than
// leaving new/reconnecting agents to infer cross-tool conventions
// (compact-by-default, associated_with vs. explicit relationships, the
// reference grammar) from scattered per-tool descriptions alone.
func TestRegisterToolsInstructionsSet(t *testing.T) {
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

	init := session.InitializeResult()
	if init == nil || init.Instructions == "" {
		t.Fatal("server's InitializeResult.Instructions is empty, want product spec §7.1's cross-tool guidance")
	}
	for _, want := range []string{"ABC-123", "associated_with", "idempotency_key"} {
		if !strings.Contains(init.Instructions, want) {
			t.Errorf("server instructions do not mention %q, want the reference grammar/associated_with distinction/idempotency guidance covered", want)
		}
	}
}
