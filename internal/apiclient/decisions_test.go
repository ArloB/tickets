package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecisionRoundTrip(t *testing.T) {
	var gotIdempotencyKey, gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/ABC/decisions":
			gotIdempotencyKey = r.Header.Get("Idempotency-Key")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(Decision{Ref: "ABC-D1", Project: "ABC", Title: "Use SQLite", Status: "proposed", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/decisions/ABC-D1":
			_ = json.NewEncoder(w).Encode(Decision{Ref: "ABC-D1", Project: "ABC", Title: "Use SQLite", Status: "proposed", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/ABC/decisions":
			_ = json.NewEncoder(w).Encode(DecisionsPage{Decisions: []DecisionCompact{{Ref: "ABC-D1", Title: "Use SQLite", Status: "proposed", Version: 1}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/decisions/ABC-D1":
			gotIfMatch = r.Header.Get("If-Match")
			_ = json.NewEncoder(w).Encode(Decision{Ref: "ABC-D1", Project: "ABC", Title: "Use SQLite (final)", Status: "accepted", Version: 2})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}

	created, err := c.CreateDecision(t.Context(), "ABC", CreateDecisionRequest{Title: "Use SQLite"}, "retry-key")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	if created.Ref != "ABC-D1" || created.Status != "proposed" {
		t.Errorf("CreateDecision = %+v, want ref=ABC-D1 status=proposed", created)
	}
	if gotIdempotencyKey != "retry-key" {
		t.Errorf("Idempotency-Key header = %q, want %q", gotIdempotencyKey, "retry-key")
	}

	got, err := c.GetDecision(t.Context(), "ABC-D1")
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if got.Ref != "ABC-D1" {
		t.Errorf("GetDecision = %+v, want ref=ABC-D1", got)
	}

	page, err := c.ListDecisions(t.Context(), "ABC", 0, "")
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(page.Decisions) != 1 || page.Decisions[0].Ref != "ABC-D1" {
		t.Errorf("ListDecisions = %+v, want exactly decision ABC-D1", page)
	}

	updated, err := c.UpdateDecision(t.Context(), "ABC-D1", UpdateDecisionRequest{Title: "Use SQLite (final)", Status: "accepted"}, 1)
	if err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}
	if updated.Status != "accepted" || updated.Version != 2 {
		t.Errorf("UpdateDecision = %+v, want status=accepted version=2", updated)
	}
	if gotIfMatch != `"1"` {
		t.Errorf("UpdateDecision If-Match = %q, want %q", gotIfMatch, `"1"`)
	}
}
