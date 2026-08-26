package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestSearchOverHTTP is Phase 5 Step 6's route-wiring exit check: a
// ticket, a decision, and a comment are all findable through GET
// /search, validated against api/openapi.yaml by ts.do.
func TestSearchOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "Reticulate the splines", "general": true}))
	ts.do(http.MethodPost, "/projects/ABC/decisions", nil, mustJSON(t, map[string]string{"title": "Spline policy", "decision": "we reticulate quadratically"}))
	ts.do(http.MethodPost, "/tickets/ABC-1/comments", nil, mustJSON(t, map[string]string{"body": "reticulation looks fixed now"}))

	resp, body := ts.do(http.MethodGet, "/search?q=reticulate", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal search page: %v", err)
	}
	if len(page.Hits) != 3 {
		t.Fatalf("hits = %+v, want 3 (ticket, decision, comment)", page.Hits)
	}
	kinds := map[string]bool{}
	for _, h := range page.Hits {
		kinds[h["kind"].(string)] = true
	}
	for _, want := range []string{"ticket", "decision", "comment"} {
		if !kinds[want] {
			t.Errorf("hits missing kind %q: %+v", want, page.Hits)
		}
	}
}

// TestSearchFindsAttachmentAndLinkOverHTTP is Step 9 close-out's
// route-wiring check for product spec §6.3's "attachment names and
// link metadata" requirement — validated against api/openapi.yaml by
// ts.do the same way TestSearchOverHTTP is, so a SearchHit.kind enum
// that forgot "attachment"/"link" would fail this on the response
// side, not just silently pass a Go-only test.
func TestSearchFindsAttachmentAndLinkOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "Host ticket", "general": true}))
	ts.do(http.MethodPost, "/tickets/ABC-1/attachments", nil, mustJSON(t, map[string]string{"title": "gronkulator spec", "path": "/docs/gronkulator.pdf"}))
	ts.do(http.MethodPost, "/tickets/ABC-1/links", nil, mustJSON(t, map[string]string{"title": "gronkulator tracker", "url": "https://example.com/gronkulator"}))

	resp, body := ts.do(http.MethodGet, "/search?q=gronkulator", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal search page: %v", err)
	}
	kinds := map[string]bool{}
	for _, h := range page.Hits {
		kinds[h["kind"].(string)] = true
		if h["ref"] != "ABC-1" {
			t.Errorf("hit ref = %v, want ABC-1 (the owning ticket) for every hit here: %+v", h["ref"], h)
		}
		if h["comment_id"] != nil {
			t.Errorf("attachment/link hit has a comment_id: %+v", h)
		}
	}
	for _, want := range []string{"attachment", "link"} {
		if !kinds[want] {
			t.Errorf("hits missing kind %q: %+v", want, page.Hits)
		}
	}
}

// TestSearchRequiresQuery proves a request with no q at all is
// rejected — OpenAPI's own required-parameter check catches this
// before the handler runs, so this goes through doUnvalidated (a
// request that fails schema validation has no schema-conformant
// response to check ts.do against).
func TestSearchRequiresQuery(t *testing.T) {
	ts := newTestServer(t)
	resp, body := ts.doUnvalidated(http.MethodGet, "/search", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("search with no q status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

// TestSearchRejectsWhitespaceOnlyQuery proves a present-but-blank q
// (which satisfies OpenAPI's required check, since it's a non-empty
// string) is still a service-layer validation error once sanitized
// down to nothing.
func TestSearchRejectsWhitespaceOnlyQuery(t *testing.T) {
	ts := newTestServer(t)
	resp, body := ts.do(http.MethodGet, "/search?q=+++", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("search with whitespace-only q status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

// TestSearchRejectsInvalidKindFilter proves a bogus kind value is a
// validation error rather than silently matching nothing.
func TestSearchRejectsInvalidKindFilter(t *testing.T) {
	ts := newTestServer(t)
	resp, body := ts.do(http.MethodGet, "/search?q=widget&kind=bogus", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("search with invalid kind status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

// TestSearchQuerySyntaxCharactersDoNotError proves a raw FTS5
// metacharacter in q (a colon, an unbalanced quote) is sanitized
// rather than surfacing as a 500 — domain.SanitizeFTSQuery's whole
// reason to exist.
func TestSearchQuerySyntaxCharactersDoNotError(t *testing.T) {
	ts := newTestServer(t)
	resp, body := ts.do(http.MethodGet, `/search?q=`+`foo%3A+bar%22+AND+%2A`, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search with FTS5 metacharacters status = %d, want 200, body=%s", resp.StatusCode, body)
	}
}
