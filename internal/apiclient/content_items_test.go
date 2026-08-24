package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentItemRoundTrip(t *testing.T) {
	var gotIdempotencyKey, gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/ABC/plans":
			gotIdempotencyKey = r.Header.Get("Idempotency-Key")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(ContentItem{Ref: "ABC-P1", Project: "ABC", Kind: "plan", Title: "Rollout", Representation: "markdown", Body: "Step one", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/plans/ABC-P1":
			_ = json.NewEncoder(w).Encode(ContentItem{Ref: "ABC-P1", Project: "ABC", Kind: "plan", Title: "Rollout", Representation: "markdown", Body: "Step one", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/ABC/plans":
			_ = json.NewEncoder(w).Encode(ContentItemsPage{Items: []ContentItemCompact{{Ref: "ABC-P1", Title: "Rollout", Kind: "plan", Version: 1}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/plans/ABC-P1":
			gotIfMatch = r.Header.Get("If-Match")
			_ = json.NewEncoder(w).Encode(ContentItem{Ref: "ABC-P1", Project: "ABC", Kind: "plan", Title: "Rollout (final)", Representation: "markdown", Body: "Step one\nStep two", Version: 2})
		case r.Method == http.MethodGet && r.URL.Path == "/plans/ABC-P1/versions":
			_ = json.NewEncoder(w).Encode(ContentItemVersionsPage{Versions: []ContentItemVersion{
				{Version: 1, Representation: "markdown", Title: "Rollout", Body: "Step one", EditedBy: "human:local"},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/plans/ABC-P1/diff":
			if r.URL.Query().Get("from") != "1" || r.URL.Query().Get("to") != "2" {
				t.Errorf("diff query = %q, want from=1&to=2", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(ContentItemDiff{
				FromVersion: 1, ToVersion: 2,
				Title: []DiffLine{{Op: "equal", Text: "Rollout"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}

	created, err := c.CreateContentItem(t.Context(), "plans", "ABC", CreateContentItemRequest{Title: "Rollout", Body: "Step one"}, "retry-key")
	if err != nil {
		t.Fatalf("CreateContentItem(plans): %v", err)
	}
	if created.Ref != "ABC-P1" || created.Kind != "plan" {
		t.Errorf("CreateContentItem(plans) = %+v, want ref=ABC-P1 kind=plan", created)
	}
	if gotIdempotencyKey != "retry-key" {
		t.Errorf("Idempotency-Key header = %q, want %q", gotIdempotencyKey, "retry-key")
	}

	got, err := c.GetContentItem(t.Context(), "plans", "ABC-P1")
	if err != nil {
		t.Fatalf("GetContentItem(plans): %v", err)
	}
	if got.Ref != "ABC-P1" {
		t.Errorf("GetContentItem(plans) = %+v, want ref=ABC-P1", got)
	}

	page, err := c.ListContentItems(t.Context(), "plans", "ABC", 0, "")
	if err != nil {
		t.Fatalf("ListContentItems(plans): %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Ref != "ABC-P1" {
		t.Errorf("ListContentItems(plans) = %+v, want exactly item ABC-P1", page)
	}

	updated, err := c.UpdateContentItem(t.Context(), "plans", "ABC-P1", UpdateContentItemRequest{Title: "Rollout (final)", Body: "Step one\nStep two"}, 1)
	if err != nil {
		t.Fatalf("UpdateContentItem(plans): %v", err)
	}
	if updated.Version != 2 || updated.Body != "Step one\nStep two" {
		t.Errorf("UpdateContentItem(plans) = %+v, want version=2 body updated", updated)
	}
	if gotIfMatch != `"1"` {
		t.Errorf("UpdateContentItem(plans) If-Match = %q, want %q", gotIfMatch, `"1"`)
	}

	versions, err := c.ListContentItemVersions(t.Context(), "plans", "ABC-P1")
	if err != nil {
		t.Fatalf("ListContentItemVersions(plans): %v", err)
	}
	if len(versions.Versions) != 1 || versions.Versions[0].Title != "Rollout" {
		t.Errorf("ListContentItemVersions(plans) = %+v, want exactly one archived version titled %q", versions, "Rollout")
	}

	diff, err := c.GetContentItemDiff(t.Context(), "plans", "ABC-P1", 1, 2)
	if err != nil {
		t.Fatalf("GetContentItemDiff(plans): %v", err)
	}
	if len(diff.Title) != 1 || diff.Title[0].Text != "Rollout" {
		t.Errorf("GetContentItemDiff(plans) = %+v, want a title diff line", diff)
	}
}

// TestDocumentRoutesUseDocumentPrefix proves urlKind="documents" hits
// /documents, not /plans — the "plans" round trip above already
// exercises the shared serialization shape, so this only needs to
// check the URL prefix the "documents" urlKind actually targets.
func TestDocumentRoutesUseDocumentPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/ABC/documents":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(ContentItem{Ref: "ABC-DOC1", Project: "ABC", Kind: "document", Title: "Notes", Representation: "markdown", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/documents/ABC-DOC1":
			_ = json.NewEncoder(w).Encode(ContentItem{Ref: "ABC-DOC1", Project: "ABC", Kind: "document", Title: "Notes", Representation: "markdown", Version: 1})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}

	created, err := c.CreateContentItem(t.Context(), "documents", "ABC", CreateContentItemRequest{Title: "Notes"}, "")
	if err != nil {
		t.Fatalf("CreateContentItem(documents): %v", err)
	}
	if created.Ref != "ABC-DOC1" {
		t.Errorf("CreateContentItem(documents) = %+v, want ref=ABC-DOC1", created)
	}

	got, err := c.GetContentItem(t.Context(), "documents", "ABC-DOC1")
	if err != nil {
		t.Fatalf("GetContentItem(documents): %v", err)
	}
	if got.Kind != "document" {
		t.Errorf("GetContentItem(documents) = %+v, want kind=document", got)
	}
}
