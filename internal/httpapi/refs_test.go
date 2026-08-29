package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

type resolvedRefsPage struct {
	Refs []struct {
		Ref    string `json:"ref"`
		Exists bool   `json:"exists"`
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"refs"`
}

func getResolved(t *testing.T, ts *testServer, query string) resolvedRefsPage {
	t.Helper()
	resp, body := ts.do(http.MethodGet, "/refs/resolve?refs="+query, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d, body=%s", resp.StatusCode, body)
	}
	var page resolvedRefsPage
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return page
}

// TestResolveRefsOverHTTP is the schema-validated round trip for the
// endpoint the Markdown renderer calls before turning a reference in
// prose into a hyperlink (ADR 0025): a live ticket comes back with
// its kind and title, a well-formed reference to nothing comes back
// exists=false in the same 200, and both entries keep the tokens'
// request order.
func TestResolveRefsOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "Real one", "general": true}))

	page := getResolved(t, ts, "ABC-1,ABC-99,ABC")
	if len(page.Refs) != 3 {
		t.Fatalf("refs = %+v, want 3 entries", page.Refs)
	}
	if !page.Refs[0].Exists || page.Refs[0].Ref != "ABC-1" || page.Refs[0].Kind != "ticket" || page.Refs[0].Title != "Real one" {
		t.Errorf("refs[0] = %+v, want the live ticket with kind and title", page.Refs[0])
	}
	if page.Refs[1].Exists || page.Refs[1].Kind != "" || page.Refs[1].Title != "" {
		t.Errorf("refs[1] = %+v, want exists=false with no kind or title", page.Refs[1])
	}
	if !page.Refs[2].Exists || page.Refs[2].Kind != "project" {
		t.Errorf("refs[2] = %+v, want the bare project key resolved", page.Refs[2])
	}
}

// TestResolveRefsEmptyQuery keeps the renderer's degenerate case — a
// body with no references at all — a well-formed empty answer rather
// than a 400 the caller has to special-case.
func TestResolveRefsEmptyQuery(t *testing.T) {
	ts := newTestServer(t)
	resp, body := ts.do(http.MethodGet, "/refs/resolve", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d, body=%s", resp.StatusCode, body)
	}
	var page resolvedRefsPage
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(page.Refs) != 0 {
		t.Errorf("refs = %+v, want empty", page.Refs)
	}
}
