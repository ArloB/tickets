package mcpsrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRecordCreateContentItemPathAndURLRepresentations proves
// record_create/record_update's representation/path/url fields
// (Phase 5 Step 5) reach the same service path the HTTP API and CLI
// use — no file-upload path over MCP (no multipart transport), so
// this only covers path/url, the two representations record_create
// actually accepts.
func TestRecordCreateContentItemPathAndURLRepresentations(t *testing.T) {
	backend, _ := newTestBackend(t)
	raw, _ := mustIssueAgentToken(t, backend, "codex")
	ts := httptest.NewServer(NewStreamableHTTPHandler(backend))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: raw}},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	pathRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
		"project_key": "ABC", "kind": "plan", "title": "External plan", "representation": "path", "path": "/srv/docs/plan.md",
	}})
	if err != nil {
		t.Fatalf("CallTool record_create (path): %v", err)
	}
	if pathRes.IsError {
		t.Fatalf("record_create (path) returned a tool error: %+v", pathRes.Content)
	}
	pathCreated := decodeResult[RecordWriteResult](t, pathRes)
	if pathCreated.Ref == "" || pathCreated.Kind != "plan" {
		t.Fatalf("record_create (path) result = %+v, want a plan ref", pathCreated)
	}

	pathGetRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": pathCreated.Ref}})
	if err != nil {
		t.Fatalf("CallTool record_get (path): %v", err)
	}
	pathGot := decodeResult[RecordDetail](t, pathGetRes)
	if pathGot.Representation != "path" || pathGot.PathValue != "/srv/docs/plan.md" {
		t.Errorf("record_get (path) result = %+v, want representation=path path_value=/srv/docs/plan.md", pathGot)
	}

	urlRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_create", Arguments: map[string]any{
		"project_key": "ABC", "kind": "document", "title": "Wiki page", "representation": "url", "url": "https://wiki.example.com/page",
	}})
	if err != nil {
		t.Fatalf("CallTool record_create (url): %v", err)
	}
	if urlRes.IsError {
		t.Fatalf("record_create (url) returned a tool error: %+v", urlRes.Content)
	}
	urlCreated := decodeResult[RecordWriteResult](t, urlRes)

	urlGetRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": urlCreated.Ref}})
	if err != nil {
		t.Fatalf("CallTool record_get (url): %v", err)
	}
	urlGot := decodeResult[RecordDetail](t, urlGetRes)
	if urlGot.Representation != "url" || urlGot.URLValue != "https://wiki.example.com/page" {
		t.Errorf("record_get (url) result = %+v, want representation=url url_value=https://wiki.example.com/page", urlGot)
	}

	// record_update on the path plan can change its path value, but
	// cannot switch its representation — there is no representation
	// field on record_update at all.
	updateRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_update", Arguments: map[string]any{
		"ref": pathCreated.Ref, "title": "External plan", "path": "/srv/docs/plan-v2.md", "expected_version": pathGot.Version,
	}})
	if err != nil {
		t.Fatalf("CallTool record_update (path): %v", err)
	}
	if updateRes.IsError {
		t.Fatalf("record_update (path) returned a tool error: %+v", updateRes.Content)
	}

	finalGetRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "record_get", Arguments: map[string]any{"ref": pathCreated.Ref}})
	if err != nil {
		t.Fatalf("CallTool record_get (final): %v", err)
	}
	finalGot := decodeResult[RecordDetail](t, finalGetRes)
	if finalGot.Representation != "path" || finalGot.PathValue != "/srv/docs/plan-v2.md" {
		t.Errorf("record_get (final) result = %+v, want representation=path path_value=/srv/docs/plan-v2.md", finalGot)
	}
}
