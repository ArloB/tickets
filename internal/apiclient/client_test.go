package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestClientRoundTrip proves Client decodes a real success response
// into the DTOs this package declares, against a fake server standing
// in for internal/httpapi (a full httptest.NewServer(httpapi.NewHandler)
// round trip is covered by internal/mcpsrv's TestStdioBridgeReachesSameService;
// this test isolates apiclient's own decoding logic).
func TestClientRoundTrip(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/ABC":
			_ = json.NewEncoder(w).Encode(Project{Key: "ABC", Title: "Example", Status: "active", Version: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/tickets/ABC-1":
			_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Project: "ABC", Feature: "ABC-F1", Type: "task", Title: "T", Status: "backlog", Priority: "medium", Version: 1})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/ABC/tickets":
			var req CreateTicketRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-2", Project: "ABC", Feature: "ABC-F1", Type: req.Type, Title: req.Title, Status: "backlog", Priority: req.Priority, Version: 1})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "test-token"}

	proj, err := c.GetProject(t.Context(), "ABC")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if proj.Key != "ABC" || proj.Title != "Example" {
		t.Errorf("GetProject = %+v, want key=ABC title=Example", proj)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-token")
	}

	ticket, err := c.GetTicket(t.Context(), "ABC-1")
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if ticket.Ref != "ABC-1" || ticket.Title != "T" {
		t.Errorf("GetTicket = %+v, want ref=ABC-1 title=T", ticket)
	}

	created, err := c.CreateTicket(t.Context(), "ABC", CreateTicketRequest{Type: "bug", Title: "New bug", Priority: "high"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if created.Ref != "ABC-2" || created.Type != "bug" || created.Priority != "high" {
		t.Errorf("CreateTicket = %+v, want ref=ABC-2 type=bug priority=high", created)
	}
}

// TestCreateProjectRoundTrip proves CreateProject POSTs to /projects
// (not /projects/, and not nested under an existing project the way
// CreateTicket is) and forwards its idempotency key — the request
// shape isn't exercised by the CLI-level TestProjectCreateJSON, which
// hits a real server that would happily accept a wrong path or a
// dropped header without failing.
func TestCreateProjectRoundTrip(t *testing.T) {
	var gotMethod, gotPath, gotIdempotencyKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotIdempotencyKey = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Project{Key: "XYZ", Title: "Second Project", Status: "active", Version: 1})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	created, err := c.CreateProject(t.Context(), CreateProjectRequest{Key: "XYZ", Title: "Second Project"}, "retry-key")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if created.Key != "XYZ" || created.Title != "Second Project" {
		t.Errorf("CreateProject = %+v, want key=XYZ title=%q", created, "Second Project")
	}
	if gotMethod != http.MethodPost || gotPath != "/projects" {
		t.Errorf("request = %s %s, want POST /projects", gotMethod, gotPath)
	}
	if gotIdempotencyKey != "retry-key" {
		t.Errorf("Idempotency-Key header = %q, want %q", gotIdempotencyKey, "retry-key")
	}
}

// TestClientDecodesErrorEnvelope proves a non-2xx response decodes
// into a client-side *Error a caller can inspect by domain.ErrorCode —
// not just a generic "request failed" error.
func TestClientDecodesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "not_found", "message": "project \"NOPE\" not found"},
		})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	_, err := c.GetProject(t.Context(), "NOPE")
	if err == nil {
		t.Fatal("GetProject: want an error, got nil")
	}
	var cerr *Error
	if e, ok := err.(*Error); ok {
		cerr = e
	} else {
		t.Fatalf("GetProject error type = %T, want *Error", err)
	}
	if cerr.Code != domain.ErrNotFound {
		t.Errorf("error.Code = %q, want %q", cerr.Code, domain.ErrNotFound)
	}
	if cerr.Message == "" {
		t.Error("error.Message is empty")
	}
}

// TestClientVersionConflictCarriesCurrentVersion proves the
// CurrentVersion field survives the envelope round trip — the one
// piece of *Error a caller actually branches on beyond Code/Message
// (a retry-with-fresh-version loop).
func TestClientVersionConflictCarriesCurrentVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "version_conflict", "message": "version mismatch", "current_version": 4},
		})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	_, err := c.GetTicket(t.Context(), "ABC-1")
	var cerr *Error
	if e, ok := err.(*Error); ok {
		cerr = e
	} else {
		t.Fatalf("GetTicket error type = %T, want *Error", err)
	}
	if cerr.CurrentVersion == nil || *cerr.CurrentVersion != 4 {
		t.Errorf("error.CurrentVersion = %v, want 4", cerr.CurrentVersion)
	}
}

// TestClientDefaultHTTPClient confirms Client works with no HTTPClient
// set at all (http.DefaultClient fallback), matching how
// cmd/tickets/mcp.go constructs it.
func TestClientDefaultHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Project{Key: "ABC"})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if _, err := c.GetProject(t.Context(), "ABC"); err != nil {
		t.Fatalf("GetProject with default HTTP client: %v", err)
	}
}
