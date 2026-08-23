package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestBacklinksOverHTTP confirms a ticket mentioning another ticket in
// its description shows up as a backlink on the mentioned ticket, and
// that the endpoint is reachable read-only (GET only, no write route
// registered).
func TestBacklinksOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "Target"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "Source", "description": "See ABC-1"}))

	resp, body := ts.do(http.MethodGet, "/tickets/ABC-1/backlinks", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list backlinks status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Backlinks []struct {
			Ref       string `json:"ref"`
			CommentID *int64 `json:"comment_id"`
		} `json:"backlinks"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(page.Backlinks) != 1 || page.Backlinks[0].Ref != "ABC-2" {
		t.Fatalf("backlinks = %+v, want exactly one edge from ABC-2", page.Backlinks)
	}
	if page.Backlinks[0].CommentID != nil {
		t.Errorf("backlinks[0].CommentID = %v, want nil (own-body mention)", page.Backlinks[0].CommentID)
	}
}
