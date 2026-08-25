package mcpsrv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// TestHTTPBackendMapsWireShapesToDomain proves HTTPBackend's own job —
// translating apiclient's wire DTOs into domain.Project/domain.Ticket,
// including parsing the wire's "kind:name" assignee/creator strings
// back into domain.ActorRef — actually round-trips correctly.
func TestHTTPBackendMapsWireShapesToDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/ABC":
			_ = json.NewEncoder(w).Encode(apiclient.Project{
				Key: "ABC", Title: "Example", Status: "active", Version: 1,
			})
		case "/tickets/ABC-1":
			assignee := "human:alice"
			creator := "agent:codex-1"
			_ = json.NewEncoder(w).Encode(apiclient.Ticket{
				Ref: "ABC-1", Project: "ABC", Feature: "ABC-F1", Type: "bug",
				Title: "T", Status: "in_progress", Priority: "high",
				Assignee: &assignee, Creator: &creator, Version: 2,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	backend := &HTTPBackend{Client: &apiclient.Client{BaseURL: srv.URL}}

	proj, err := backend.GetProject(t.Context(), "ABC")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if proj.Key != "ABC" || proj.Status != domain.ProjectStatusActive {
		t.Errorf("GetProject = %+v, want key=ABC status=active", proj)
	}

	ticket, err := backend.GetTicket(t.Context(), "ABC-1")
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	wantAssignee := domain.ActorRef{Kind: domain.ActorHuman, Name: "alice"}
	wantCreator := domain.ActorRef{Kind: domain.ActorAgent, Name: "codex-1"}
	if ticket.Assignee == nil || *ticket.Assignee != wantAssignee {
		t.Errorf("ticket.Assignee = %v, want %v", ticket.Assignee, wantAssignee)
	}
	if ticket.Creator == nil || *ticket.Creator != wantCreator {
		t.Errorf("ticket.Creator = %v, want %v", ticket.Creator, wantCreator)
	}
	if ticket.Status != domain.WorkflowStatusInProgress || ticket.Priority != domain.PriorityHigh {
		t.Errorf("ticket status/priority = %v/%v, want in_progress/high", ticket.Status, ticket.Priority)
	}
}

// TestHTTPBackendDefaultProjectFillsOmittedKey is the plan's Step 15
// "--project/TICKETS_PROJECT" exit check: a tool call that omits a
// project key gets DefaultProject filled in before the request is
// built, and a tool call that supplies its own key is left alone.
func TestHTTPBackendDefaultProjectFillsOmittedKey(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(apiclient.Project{Key: "ABC"})
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apiclient.Ticket{Ref: "ABC-1", Type: "task", Status: "backlog", Priority: "medium"})
		}
	}))
	defer srv.Close()

	backend := &HTTPBackend{Client: &apiclient.Client{BaseURL: srv.URL}, DefaultProject: "ABC"}

	if _, err := backend.GetProject(t.Context(), ""); err != nil {
		t.Fatalf("GetProject with omitted key: %v", err)
	}
	if _, err := backend.CreateTicket(t.Context(), CreateTicketInput{Type: "task", Title: "T"}); err != nil {
		t.Fatalf("CreateTicket with omitted project key: %v", err)
	}
	// An explicit project key is left alone, not overridden by the default.
	if _, err := backend.CreateTicket(t.Context(), CreateTicketInput{ProjectKey: "XYZ", Type: "task", Title: "T"}); err != nil {
		t.Fatalf("CreateTicket with explicit project key: %v", err)
	}

	want := []string{"/projects/ABC", "/projects/ABC/tickets", "/projects/XYZ/tickets"}
	if len(gotPaths) != len(want) {
		t.Fatalf("requested paths = %v, want %v", gotPaths, want)
	}
	for i, p := range want {
		if gotPaths[i] != p {
			t.Errorf("request %d path = %q, want %q", i, gotPaths[i], p)
		}
	}
}

// TestHTTPBackendMissingProjectKeyRejected proves an omitted project
// key with no DefaultProject configured is a clear validation_failed,
// not an empty key silently reaching the server as "GET /projects/"
// or "POST /projects//tickets".
func TestHTTPBackendMissingProjectKeyRejected(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	backend := &HTTPBackend{Client: &apiclient.Client{BaseURL: srv.URL}}

	if _, err := backend.GetProject(t.Context(), ""); err == nil {
		t.Fatal("GetProject with no key and no default: want an error, got nil")
	} else if svcErr, ok := err.(*service.Error); !ok || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "project_key" {
		t.Errorf("GetProject error = %#v, want validation_failed on field project_key", err)
	}

	if _, err := backend.CreateTicket(t.Context(), CreateTicketInput{Type: "task", Title: "T"}); err == nil {
		t.Fatal("CreateTicket with no project key and no default: want an error, got nil")
	} else if svcErr, ok := err.(*service.Error); !ok || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "project_key" {
		t.Errorf("CreateTicket error = %#v, want validation_failed on field project_key", err)
	}

	if called {
		t.Error("server was called despite the missing project key — request should have been rejected client-side")
	}
}

// TestHTTPBackendListMethods proves ListProjects/ListTickets convert
// apiclient's compact wire DTOs into this package's own compact
// output types, and that ListTickets applies the same DefaultProject
// fallback CreateTicket already has (ADR 0016) when the caller omits
// a project key.
func TestHTTPBackendListMethods(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		switch r.URL.Path {
		case "/projects":
			_ = json.NewEncoder(w).Encode(apiclient.ProjectsPage{
				Projects:   []apiclient.ProjectCompact{{Key: "ABC", Title: "Example", Status: "active", Version: 1}},
				NextCursor: "cursor-1",
			})
		case "/projects/ABC/tickets":
			sev := "high"
			_ = json.NewEncoder(w).Encode(apiclient.TicketsPage{
				Tickets: []apiclient.TicketCompact{{Ref: "ABC-1", Title: "T", Type: "bug", Status: "backlog", Priority: "high", Severity: &sev}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	backend := &HTTPBackend{Client: &apiclient.Client{BaseURL: srv.URL}, DefaultProject: "ABC"}

	projects, err := backend.ListProjects(t.Context(), 10, "")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects.Projects) != 1 || projects.Projects[0].Key != "ABC" || projects.NextCursor != "cursor-1" {
		t.Errorf("ListProjects = %+v, want one project ABC with next_cursor=cursor-1", projects)
	}

	tickets, err := backend.ListTickets(t.Context(), "", "issue_register", TicketListFilters{}, 0, "")
	if err != nil {
		t.Fatalf("ListTickets with omitted project key: %v", err)
	}
	if gotPath != "/projects/ABC/tickets?view=issue_register" {
		t.Errorf("requested path = %q, want DefaultProject filled in with view forwarded", gotPath)
	}
	if len(tickets.Tickets) != 1 || tickets.Tickets[0].Ref != "ABC-1" || tickets.Tickets[0].Severity == nil || *tickets.Tickets[0].Severity != "high" {
		t.Errorf("ListTickets = %+v, want one bug ticket with severity=high", tickets)
	}

	// Phase 7: every TicketListFilters field forwards as its own query
	// parameter, matching docs/contracts/list-filters.md's names.
	if _, err := backend.ListTickets(t.Context(), "ABC", "priority_queue", TicketListFilters{
		Status: "in_progress", Type: "bug", Severity: "high", Priority: "critical",
		FeatureRef: "ABC-F1", Assignee: "agent:codex", Creator: "human:alice", UpdatedSince: "2024-01-01T00:00:00Z",
	}, 0, ""); err != nil {
		t.Fatalf("ListTickets with filters: %v", err)
	}
	wantParams := []string{
		"view=priority_queue", "status=in_progress", "type=bug", "severity=high", "priority=critical",
		"feature_ref=ABC-F1", "assignee=agent%3Acodex", "creator=human%3Aalice", "updated_since=2024-01-01T00%3A00%3A00Z",
	}
	for _, want := range wantParams {
		if !strings.Contains(gotPath, want) {
			t.Errorf("requested path = %q, want it to contain %q", gotPath, want)
		}
	}
}

// TestHTTPBackendConvertsClientErrorToServiceError proves an
// apiclient.Error crossing HTTPBackend's boundary comes out as a
// *service.Error — the shape tools.go's toolError type-switches on,
// the same as InProcessBackend's errors, so a client-side failure
// doesn't collapse into a generic "internal_error" over MCP.
func TestHTTPBackendConvertsClientErrorToServiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "not_found", "message": "project \"NOPE\" not found"},
		})
	}))
	defer srv.Close()

	backend := &HTTPBackend{Client: &apiclient.Client{BaseURL: srv.URL}}
	_, err := backend.GetProject(t.Context(), "NOPE")
	if err == nil {
		t.Fatal("GetProject: want an error, got nil")
	}
	svcErr, ok := err.(*service.Error)
	if !ok {
		t.Fatalf("error type = %T, want *service.Error", err)
	}
	if svcErr.Code != domain.ErrNotFound {
		t.Errorf("error.Code = %q, want %q", svcErr.Code, domain.ErrNotFound)
	}
}
