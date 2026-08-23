package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRelationshipRoundTrip(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tickets/ABC-1/relationships":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "added"})
		case r.Method == http.MethodGet && r.URL.Path == "/tickets/ABC-1/relationships":
			_ = json.NewEncoder(w).Encode(RelationshipsPage{Relationships: []RelationshipView{{Type: "blocks", Other: "ABC-2"}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/tickets/ABC-1/relationships/blocks/ABC-2":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if err := c.AddRelationship(t.Context(), "ABC-1", "blocks", "ABC-2"); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}
	if gotBody["target"] != "ABC-2" || gotBody["type"] != "blocks" {
		t.Errorf("AddRelationship body = %+v, want target=ABC-2 type=blocks", gotBody)
	}

	page, err := c.ListRelationships(t.Context(), "ABC-1")
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(page.Relationships) != 1 || page.Relationships[0].Type != "blocks" || page.Relationships[0].Other != "ABC-2" {
		t.Errorf("ListRelationships = %+v, want one blocks edge to ABC-2", page)
	}

	if err := c.RemoveRelationship(t.Context(), "ABC-1", "blocks", "ABC-2"); err != nil {
		t.Fatalf("RemoveRelationship: %v", err)
	}
}
