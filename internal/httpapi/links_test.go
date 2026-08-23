package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestExternalLinkLifecycleOverHTTP exercises add/list/remove for a
// ticket's external links end to end, mirroring
// TestAssociationLifecycleOverHTTP's shape (relationships_test.go).
func TestExternalLinkLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "A"}))

	addResp, addBody := ts.do(http.MethodPost, "/tickets/ABC-1/links", nil,
		mustJSON(t, map[string]string{"title": "Design doc", "url": "https://example.com/design"}))
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("add link status = %d, body=%s", addResp.StatusCode, addBody)
	}
	var added struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(addBody, &added); err != nil {
		t.Fatalf("unmarshal add response: %v", err)
	}
	if added.ID == 0 || added.Title != "Design doc" || added.URL != "https://example.com/design" {
		t.Fatalf("unexpected add response: %+v", added)
	}

	listResp, listBody := ts.do(http.MethodGet, "/tickets/ABC-1/links", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list links status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page struct {
		Links []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"links"`
	}
	if err := json.Unmarshal(listBody, &page); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(page.Links) != 1 || page.Links[0].ID != added.ID {
		t.Fatalf("list links = %+v, want exactly the one link just added", page.Links)
	}

	removeResp, removeBody := ts.do(http.MethodDelete, "/tickets/ABC-1/links/"+strconv.FormatInt(added.ID, 10), nil, nil)
	if removeResp.StatusCode != http.StatusOK {
		t.Fatalf("remove link status = %d, body=%s", removeResp.StatusCode, removeBody)
	}

	afterResp, afterBody := ts.do(http.MethodGet, "/tickets/ABC-1/links", nil, nil)
	if afterResp.StatusCode != http.StatusOK {
		t.Fatalf("list links after remove status = %d, body=%s", afterResp.StatusCode, afterBody)
	}
	var afterPage struct {
		Links []map[string]any `json:"links"`
	}
	_ = json.Unmarshal(afterBody, &afterPage)
	if len(afterPage.Links) != 0 {
		t.Errorf("links after remove = %v, want none", afterPage.Links)
	}
}

// TestExternalLinkRejectsBadURLSchemeOverHTTP confirms the scheme
// allow-list is enforced end to end, coming back as a clean 400.
func TestExternalLinkRejectsBadURLSchemeOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "A"}))

	resp, body := ts.do(http.MethodPost, "/tickets/ABC-1/links", nil,
		mustJSON(t, map[string]string{"title": "evil", "url": "javascript:alert(1)"}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("add link with javascript: URL: status = %d, body=%s, want 400", resp.StatusCode, body)
	}
}

// TestExternalLinkOnFeatureAndDecisionOverHTTP confirms the same
// generic handlers work for the other two supported entity kinds.
func TestExternalLinkOnFeatureAndDecisionOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/features", nil, mustJSON(t, map[string]string{"title": "Payments"}))
	ts.do(http.MethodPost, "/projects/ABC/decisions", nil, mustJSON(t, map[string]string{"title": "Use Postgres"}))

	featResp, featBody := ts.do(http.MethodPost, "/features/ABC-F2/links", nil, mustJSON(t, map[string]string{"title": "Spec", "url": "https://example.com/spec"}))
	if featResp.StatusCode != http.StatusCreated {
		t.Fatalf("add feature link status = %d, body=%s", featResp.StatusCode, featBody)
	}

	decResp, decBody := ts.do(http.MethodPost, "/decisions/ABC-D1/links", nil, mustJSON(t, map[string]string{"title": "RFC", "url": "https://example.com/rfc"}))
	if decResp.StatusCode != http.StatusCreated {
		t.Fatalf("add decision link status = %d, body=%s", decResp.StatusCode, decBody)
	}
}
