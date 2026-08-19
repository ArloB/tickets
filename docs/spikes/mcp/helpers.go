package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerRoundTripper injects a static Authorization header. Standing in
// for whatever the real Tickets HTTP client does once ADR 0004 defines
// agent token storage/config.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (rt *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.base.RoundTrip(req)
}

func stringsReader(s string) *strings.Reader {
	return strings.NewReader(s)
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// decodeEchoOutput re-marshals StructuredContent (a map[string]any after
// the JSON round trip over either transport) back into echoOutput,
// rather than assuming it retained its concrete Go type.
func decodeEchoOutput(res *mcp.CallToolResult) (echoOutput, error) {
	var out echoOutput
	if res == nil || res.StructuredContent == nil {
		return out, fmt.Errorf("no structured content in tool result")
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return out, fmt.Errorf("re-marshal structured content: %w", err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("unmarshal into echoOutput: %w", err)
	}
	return out, nil
}
