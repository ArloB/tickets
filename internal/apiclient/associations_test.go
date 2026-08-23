package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAssociationRoundTripTicketSource proves AddAssociation/
// ListAssociations/RemoveAssociation dispatch to /tickets/... when ref
// is a ticket reference.
func TestAssociationRoundTripTicketSource(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "added"})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(AssociationsPage{Associated: []string{"ABC-F2"}})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if err := c.AddAssociation(t.Context(), "ABC-1", "ABC-F2"); err != nil {
		t.Fatalf("AddAssociation: %v", err)
	}
	page, err := c.ListAssociations(t.Context(), "ABC-1")
	if err != nil {
		t.Fatalf("ListAssociations: %v", err)
	}
	if len(page.Associated) != 1 || page.Associated[0] != "ABC-F2" {
		t.Errorf("ListAssociations = %+v, want [ABC-F2]", page)
	}
	if err := c.RemoveAssociation(t.Context(), "ABC-1", "ABC-F2"); err != nil {
		t.Fatalf("RemoveAssociation: %v", err)
	}

	want := []string{
		"POST /tickets/ABC-1/associations",
		"GET /tickets/ABC-1/associations",
		"DELETE /tickets/ABC-1/associations/ABC-F2",
	}
	if len(gotPaths) != len(want) {
		t.Fatalf("requests = %v, want %v", gotPaths, want)
	}
	for i, p := range want {
		if gotPaths[i] != p {
			t.Errorf("request %d = %q, want %q", i, gotPaths[i], p)
		}
	}
}

// TestAssociationRoundTripFeatureSource proves the same three methods
// dispatch to /features/... when ref is a feature reference instead.
func TestAssociationRoundTripFeatureSource(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "added"})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if err := c.AddAssociation(t.Context(), "ABC-F1", "ABC-1"); err != nil {
		t.Fatalf("AddAssociation with a feature source: %v", err)
	}
	if gotPath != "/features/ABC-F1/associations" {
		t.Errorf("request path = %q, want /features/ABC-F1/associations", gotPath)
	}
}

// TestAssociationRoundTripDecisionSource proves the same three methods
// dispatch to /decisions/... when ref is a decision reference —
// decisions have had association routes since Phase 3's decisions
// slice landed, and apiclient must know about them too.
func TestAssociationRoundTripDecisionSource(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "added"})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if err := c.AddAssociation(t.Context(), "ABC-D1", "ABC-1"); err != nil {
		t.Fatalf("AddAssociation with a decision source: %v", err)
	}
	if gotPath != "/decisions/ABC-D1/associations" {
		t.Errorf("request path = %q, want /decisions/ABC-D1/associations", gotPath)
	}
}

// TestAssociationRejectsUnsupportedSourceKind proves an unparseable
// reference is rejected client-side, before any request is built.
func TestAssociationRejectsUnsupportedSourceKind(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if err := c.AddAssociation(t.Context(), "not-a-valid-reference", "ABC-1"); err == nil {
		t.Error("AddAssociation with an unparseable reference: want error, got nil")
	}
	if called {
		t.Error("server was called despite an invalid source reference — request should have been rejected client-side")
	}
}
