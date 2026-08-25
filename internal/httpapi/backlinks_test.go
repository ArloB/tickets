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

// TestBacklinkFromProjectCommentOverHTTP is Phase 6 Step 2's HTTP
// regression test: a comment on a project (a new mention source since
// this phase) reports its bare project key as ref, not a formatted
// reference — a project has no seq-numbered reference token
// (domain.Format rejects KindProject). This is schema-validated
// against api/openapi.yaml's Backlink.ref (an unconstrained string,
// its description updated to note this case) via ts.do.
func TestBacklinkFromProjectCommentOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "Target"}))
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/comments", nil, mustJSON(t, map[string]string{"body": "See ABC-1"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project comment status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var comment struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(createBody, &comment); err != nil {
		t.Fatalf("unmarshal created comment: %v", err)
	}

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
	if len(page.Backlinks) != 1 || page.Backlinks[0].Ref != "ABC" {
		t.Fatalf("backlinks = %+v, want exactly one edge with the bare project key \"ABC\"", page.Backlinks)
	}
	if page.Backlinks[0].CommentID == nil || *page.Backlinks[0].CommentID != comment.ID {
		t.Errorf("backlinks[0].CommentID = %v, want %d", page.Backlinks[0].CommentID, comment.ID)
	}
}
