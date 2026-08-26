package mcpsrv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEveryToolNamesTheProjectKeyIdentically(t *testing.T) {
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
	if len(tools.Tools) == 0 {
		t.Fatal("ListTools returned no tools")
	}

	banned := map[string]string{
		"key":     "project_key",
		"project": "project_key",
	}
	seen := 0
	for _, tool := range tools.Tools {
		if tool.InputSchema == nil {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s input schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s input schema: %v", tool.Name, err)
		}
		for name := range schema.Properties {
			if want, bad := banned[name]; bad {
				t.Errorf("tool %s exposes input property %q; the project key is spelled %q on every other tool, "+
					"and a live Codex agent has failed a call guessing the consistent name", tool.Name, name, want)
			}
			if name == "project_key" {
				seen++
			}
		}
	}
	if seen == 0 {
		t.Error("no tool exposes a project_key property; this test is not looking at the real tool surface")
	}
}
