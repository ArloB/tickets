package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCommentRoundTrip proves every comment method sends the right
// method/path/headers and decodes the response into this package's
// own Comment/CommentVersion DTOs.
func TestCommentRoundTrip(t *testing.T) {
	var gotIdempotencyKey, gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tickets/ABC-1/comments":
			gotIdempotencyKey = r.Header.Get("Idempotency-Key")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(Comment{ID: 1, Author: "human:alice", Body: "First pass", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/tickets/ABC-1/comments":
			_ = json.NewEncoder(w).Encode(CommentsPage{Comments: []Comment{{ID: 1, Author: "human:alice", Body: "First pass", Version: 1}}})
		case r.Method == http.MethodGet && r.URL.Path == "/comments/1":
			_ = json.NewEncoder(w).Encode(Comment{ID: 1, Author: "human:alice", Body: "First pass", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/comments/1/history":
			_ = json.NewEncoder(w).Encode(CommentHistoryPage{Versions: []CommentVersion{{Version: 1, Body: "First pass", EditedBy: "human:alice"}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/comments/1":
			gotIfMatch = r.Header.Get("If-Match")
			_ = json.NewEncoder(w).Encode(Comment{ID: 1, Author: "human:alice", Body: "Edited", Version: 2})
		case r.Method == http.MethodDelete && r.URL.Path == "/comments/1":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}

	created, err := c.CreateComment(t.Context(), "ABC-1", "First pass", "retry-key")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if created.ID != 1 || created.Body != "First pass" {
		t.Errorf("CreateComment = %+v, want id=1 body=%q", created, "First pass")
	}
	if gotIdempotencyKey != "retry-key" {
		t.Errorf("Idempotency-Key header = %q, want %q", gotIdempotencyKey, "retry-key")
	}

	page, err := c.ListComments(t.Context(), "ABC-1")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(page.Comments) != 1 || page.Comments[0].ID != 1 {
		t.Errorf("ListComments = %+v, want exactly comment 1", page)
	}

	got, err := c.GetComment(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetComment: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("GetComment = %+v, want id=1", got)
	}

	history, err := c.GetCommentHistory(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetCommentHistory: %v", err)
	}
	if len(history.Versions) != 1 || history.Versions[0].Body != "First pass" {
		t.Errorf("GetCommentHistory = %+v, want one version with body %q", history, "First pass")
	}

	edited, err := c.EditComment(t.Context(), 1, 1, "Edited")
	if err != nil {
		t.Fatalf("EditComment: %v", err)
	}
	if edited.Body != "Edited" || edited.Version != 2 {
		t.Errorf("EditComment = %+v, want body=Edited version=2", edited)
	}
	if gotIfMatch != `"1"` {
		t.Errorf("EditComment If-Match = %q, want %q", gotIfMatch, `"1"`)
	}

	if err := c.DeleteComment(t.Context(), 1, 2); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
}

// TestCommentsPathPrefixDispatchesOnRefKind is Phase 6 Step 2's
// regression test for commentsPathPrefix: CreateComment/ListComments
// must route to the right one of the six /kind/{ref}/comments routes
// based on ref's own shape, including a bare project key — the one
// form domain.Parse itself rejects (see commentsPathPrefix's doc).
func TestCommentsPathPrefixDispatchesOnRefKind(t *testing.T) {
	cases := map[string]string{
		"ABC-1":    "/tickets/ABC-1/comments",
		"ABC-F1":   "/features/ABC-F1/comments",
		"ABC-D1":   "/decisions/ABC-D1/comments",
		"ABC-P1":   "/plans/ABC-P1/comments",
		"ABC-DOC1": "/documents/ABC-DOC1/comments",
		"ABC":      "/projects/ABC/comments",
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Comment{ID: 1, Version: 1})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	for ref, wantPath := range cases {
		t.Run(ref, func(t *testing.T) {
			if _, err := c.CreateComment(t.Context(), ref, "x", ""); err != nil {
				t.Fatalf("CreateComment(%q): %v", ref, err)
			}
			if gotPath != wantPath {
				t.Errorf("CreateComment(%q) hit path %q, want %q", ref, gotPath, wantPath)
			}
		})
	}

	if _, err := c.CreateComment(t.Context(), "not a ref", "x", ""); err == nil {
		t.Error("CreateComment with an unparseable ref: want an error, got nil")
	}
}
