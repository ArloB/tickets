package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestIdempotentCommentReplayOverHTTP is comments' counterpart to
// TestIdempotentCreateReplayOverHTTP (server_test.go): Phase 3 wires
// Idempotency-Key into AddComment for the first time (Phase 1's
// comment.go originally skipped it — nothing called it over the
// network yet), so this proves a retried POST with the same key and
// body returns the same comment rather than creating a duplicate.
func TestIdempotentCommentReplayOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "T"}))

	body := mustJSON(t, map[string]string{"body": "First pass"})
	headers := map[string]string{"Idempotency-Key": "comment-retry-1"}

	first, firstBody := ts.do(http.MethodPost, "/tickets/ABC-1/comments", headers, body)
	second, secondBody := ts.do(http.MethodPost, "/tickets/ABC-1/comments", headers, body)
	if first.StatusCode != http.StatusCreated || second.StatusCode != http.StatusCreated {
		t.Fatalf("replay statuses = %d, %d, want both 201", first.StatusCode, second.StatusCode)
	}
	var c1, c2 map[string]any
	_ = json.Unmarshal(firstBody, &c1)
	_ = json.Unmarshal(secondBody, &c2)
	if c1["id"] != c2["id"] {
		t.Errorf("idempotent replay created two comments: %v vs %v", c1["id"], c2["id"])
	}

	listResp, listBody := ts.do(http.MethodGet, "/tickets/ABC-1/comments", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list comments status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal(listBody, &page); err != nil {
		t.Fatalf("unmarshal comments page: %v", err)
	}
	if len(page.Comments) != 1 {
		t.Errorf("comments after replay = %d, want exactly 1", len(page.Comments))
	}
}

// TestCommentLifecycleOverHTTP exercises Step 11's full route set —
// create/list/get/edit/delete/history — each validated against
// api/openapi.yaml. This is also the first place comment-author
// attribution becomes independently verifiable over HTTP, alongside
// ticketDetail's still-pending Creator exposure (Step 13).
func TestCommentLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "T"}))

	// --- create ---
	createResp, createBody := ts.do(http.MethodPost, "/tickets/ABC-1/comments", nil,
		mustJSON(t, map[string]string{"body": "First pass"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create comment status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var comment map[string]any
	if err := json.Unmarshal(createBody, &comment); err != nil {
		t.Fatalf("unmarshal comment: %v", err)
	}
	if comment["body"] != "First pass" {
		t.Errorf("comment body = %v, want %q", comment["body"], "First pass")
	}
	if comment["author"] != "human:test-admin" {
		t.Errorf("comment author = %v, want human:test-admin", comment["author"])
	}
	id := int64(comment["id"].(float64))
	version := int64(comment["version"].(float64))

	// --- list ---
	listResp, listBody := ts.do(http.MethodGet, "/tickets/ABC-1/comments", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list comments status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page map[string]any
	_ = json.Unmarshal(listBody, &page)
	if comments, _ := page["comments"].([]any); len(comments) != 1 {
		t.Errorf("list comments returned %d, want 1", len(comments))
	}

	// --- get ---
	idStr := strconv.FormatInt(id, 10)
	getResp, getBody := ts.do(http.MethodGet, "/comments/"+idStr, nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get comment status = %d, body=%s", getResp.StatusCode, getBody)
	}

	// --- edit ---
	editResp, editBody := ts.do(http.MethodPatch, "/comments/"+idStr,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"body": "Revised"}))
	if editResp.StatusCode != http.StatusOK {
		t.Fatalf("edit comment status = %d, body=%s", editResp.StatusCode, editBody)
	}
	var edited map[string]any
	_ = json.Unmarshal(editBody, &edited)
	if edited["body"] != "Revised" {
		t.Errorf("edited comment body = %v, want %q", edited["body"], "Revised")
	}
	version = int64(edited["version"].(float64))

	// --- history ---
	historyResp, historyBody := ts.do(http.MethodGet, "/comments/"+idStr+"/history", nil, nil)
	if historyResp.StatusCode != http.StatusOK {
		t.Fatalf("comment history status = %d, body=%s", historyResp.StatusCode, historyBody)
	}
	var history map[string]any
	_ = json.Unmarshal(historyBody, &history)
	versions, _ := history["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("comment history = %d entries, want 1 (the pre-edit body)", len(versions))
	}
	firstVersion, _ := versions[0].(map[string]any)
	if firstVersion["body"] != "First pass" {
		t.Errorf("archived version body = %v, want %q", firstVersion["body"], "First pass")
	}

	// --- delete (tombstone stays visible, per §5.10) ---
	deleteResp, deleteBody := ts.do(http.MethodDelete, "/comments/"+idStr,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`}, nil)
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete comment status = %d, body=%s", deleteResp.StatusCode, deleteBody)
	}

	tombstoneResp, tombstoneBody := ts.do(http.MethodGet, "/comments/"+idStr, nil, nil)
	if tombstoneResp.StatusCode != http.StatusOK {
		t.Fatalf("get deleted comment status = %d, want 200 (visible tombstone), body=%s", tombstoneResp.StatusCode, tombstoneBody)
	}
	var tombstone map[string]any
	_ = json.Unmarshal(tombstoneBody, &tombstone)
	if tombstone["deleted_at"] == nil {
		t.Errorf("tombstoned comment deleted_at is nil, want set")
	}
	if tombstone["body"] != "Revised" {
		t.Errorf("tombstoned comment body = %v, want the intact last body %q (§5.10: body stays intact in storage)", tombstone["body"], "Revised")
	}

	// An already-deleted comment cannot be edited again — not_found,
	// same as if it never existed (comment.go's doc explains why).
	reEditResp, reEditBody := ts.do(http.MethodPatch, "/comments/"+idStr,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version+1, 10) + `"`},
		mustJSON(t, map[string]string{"body": "too late"}))
	if reEditResp.StatusCode != http.StatusNotFound {
		t.Fatalf("edit a deleted comment status = %d, want 404, body=%s", reEditResp.StatusCode, reEditBody)
	}
}

// TestCreateCommentOnMissingTicket confirms the ticket lookup failure
// translates to 404, not a confusing validation error.
func TestCreateCommentOnMissingTicket(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	resp, body := ts.do(http.MethodPost, "/tickets/ABC-999/comments", nil, mustJSON(t, map[string]string{"body": "x"}))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("create comment on missing ticket status = %d, want 404, body=%s", resp.StatusCode, body)
	}
}

// TestGetCommentNotFound confirms a bad comment id is a plain
// not_found, not a validation error masquerading as one — id parses
// fine, it just doesn't name a real comment.
func TestGetCommentNotFound(t *testing.T) {
	ts := newTestServer(t)
	resp, body := ts.do(http.MethodGet, "/comments/999999", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get missing comment status = %d, want 404, body=%s", resp.StatusCode, body)
	}
}

// TestCreateAndListCommentsOnEveryEntityKind is Phase 6 Step 2's HTTP
// regression test for the five comment routes added alongside the
// original ticket-only pair: /features, /decisions, /plans,
// /documents, and /projects. Each response is schema-validated against
// api/openapi.yaml by ts.do.
func TestCreateAndListCommentsOnEveryEntityKind(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/features", nil, mustJSON(t, map[string]string{"title": "F"}))
	ts.do(http.MethodPost, "/projects/ABC/decisions", nil, mustJSON(t, map[string]string{"title": "D", "decision": "x"}))
	ts.do(http.MethodPost, "/projects/ABC/plans", nil, mustJSON(t, map[string]string{"title": "P", "body": "plan body"}))
	ts.do(http.MethodPost, "/projects/ABC/documents", nil, mustJSON(t, map[string]string{"title": "Doc", "body": "doc body"}))

	cases := []string{
		"/features/ABC-F2/comments",
		"/decisions/ABC-D1/comments",
		"/plans/ABC-P1/comments",
		"/documents/ABC-DOC1/comments",
		"/projects/ABC/comments",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			createResp, createBody := ts.do(http.MethodPost, path, nil, mustJSON(t, map[string]string{"body": "hello"}))
			if createResp.StatusCode != http.StatusCreated {
				t.Fatalf("create comment on %s status = %d, body=%s", path, createResp.StatusCode, createBody)
			}
			var created map[string]any
			if err := json.Unmarshal(createBody, &created); err != nil {
				t.Fatalf("unmarshal created comment: %v", err)
			}

			listResp, listBody := ts.do(http.MethodGet, path, nil, nil)
			if listResp.StatusCode != http.StatusOK {
				t.Fatalf("list comments on %s status = %d, body=%s", path, listResp.StatusCode, listBody)
			}
			var page struct {
				Comments []map[string]any `json:"comments"`
			}
			if err := json.Unmarshal(listBody, &page); err != nil {
				t.Fatalf("unmarshal comments page: %v", err)
			}
			if len(page.Comments) != 1 || page.Comments[0]["id"] != created["id"] {
				t.Fatalf("list comments on %s = %+v, want exactly the created comment (id %v)", path, page.Comments, created["id"])
			}
		})
	}
}
