package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFeatureRoundTrip(t *testing.T) {
	var gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/ABC/features":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(Feature{Ref: "ABC-F2", Project: "ABC", Title: "Payments", Status: "backlog", Priority: "high", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/features/ABC-F2":
			_ = json.NewEncoder(w).Encode(Feature{Ref: "ABC-F2", Project: "ABC", Title: "Payments", Status: "backlog", Priority: "high", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/ABC/features":
			_ = json.NewEncoder(w).Encode(FeaturesPage{Features: []FeatureCompact{{Ref: "ABC-F2", Title: "Payments", Status: "backlog", Priority: "high", Version: 1}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/features/ABC-F2":
			gotIfMatch = r.Header.Get("If-Match")
			_ = json.NewEncoder(w).Encode(Feature{Ref: "ABC-F2", Project: "ABC", Title: "Payments (renamed)", Status: "backlog", Priority: "high", Version: 2})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}

	created, err := c.CreateFeature(t.Context(), "ABC", CreateFeatureRequest{Title: "Payments", Priority: "high"})
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if created.Ref != "ABC-F2" || created.Title != "Payments" {
		t.Errorf("CreateFeature = %+v, want ref=ABC-F2 title=Payments", created)
	}

	got, err := c.GetFeature(t.Context(), "ABC-F2")
	if err != nil {
		t.Fatalf("GetFeature: %v", err)
	}
	if got.Ref != "ABC-F2" {
		t.Errorf("GetFeature = %+v, want ref=ABC-F2", got)
	}

	page, err := c.ListFeatures(t.Context(), "ABC", 0, "")
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	if len(page.Features) != 1 || page.Features[0].Ref != "ABC-F2" {
		t.Errorf("ListFeatures = %+v, want exactly feature ABC-F2", page)
	}

	updated, err := c.UpdateFeature(t.Context(), "ABC-F2", UpdateFeatureRequest{Title: "Payments (renamed)", Priority: "high"}, 1)
	if err != nil {
		t.Fatalf("UpdateFeature: %v", err)
	}
	if updated.Title != "Payments (renamed)" || updated.Version != 2 {
		t.Errorf("UpdateFeature = %+v, want title=%q version=2", updated, "Payments (renamed)")
	}
	if gotIfMatch != `"1"` {
		t.Errorf("UpdateFeature If-Match = %q, want %q", gotIfMatch, `"1"`)
	}
}
