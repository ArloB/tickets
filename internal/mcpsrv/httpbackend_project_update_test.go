package mcpsrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/domain"
)

// TestHTTPBackendUpdateProjectStatusAndFieldsTogether is
// HTTPBackend.UpdateProject's counterpart to
// TestInProcessBackendUpdateProjectStatusAndFieldsTogether: a combined
// status+title UpdateProjectInput must call POST .../status first,
// then PATCH .../{key} with the If-Match the status response
// returned — not the caller's original ExpectedVersion. A fake server
// records the sequence of requests and their If-Match headers to make
// the rethreading observable without a real database.
func TestHTTPBackendUpdateProjectStatusAndFieldsTogether(t *testing.T) {
	type seen struct {
		method, path, ifMatch string
	}
	var requests []seen

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, seen{method: r.Method, path: r.URL.Path, ifMatch: r.Header.Get("If-Match")})
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/ABC/status":
			_ = json.NewEncoder(w).Encode(apiclient.Project{Key: "ABC", Title: "Example", Description: "d", Status: "archived", Version: 2})
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/ABC":
			_ = json.NewEncoder(w).Encode(apiclient.Project{Key: "ABC", Title: "Renamed", Description: "d", Status: "archived", Version: 3})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	backend := &HTTPBackend{Client: &apiclient.Client{BaseURL: srv.URL}}
	status := "archived"
	title := "Renamed"
	proj, err := backend.UpdateProject(t.Context(), UpdateProjectInput{
		Key: "ABC", Status: &status, Title: &title, ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if proj.Status != domain.ProjectStatusArchived || proj.Title != "Renamed" || proj.Version != 3 {
		t.Errorf("UpdateProject result = %+v, want status=archived title=Renamed version=3", proj)
	}

	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want exactly 2 (status then fields)", requests)
	}
	if requests[0].method != http.MethodPost || requests[0].path != "/projects/ABC/status" || requests[0].ifMatch != `"1"` {
		t.Errorf("first request = %+v, want POST /projects/ABC/status If-Match=\"1\" (the caller's ExpectedVersion)", requests[0])
	}
	if requests[1].method != http.MethodPatch || requests[1].path != "/projects/ABC" || requests[1].ifMatch != `"2"` {
		t.Errorf("second request = %+v, want PATCH /projects/ABC If-Match=\"2\" (the status response's new version, not the original 1)", requests[1])
	}
}
