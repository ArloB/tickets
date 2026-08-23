package apiclient

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordedRequest captures just what these tests need to assert on:
// method, path, and the headers/body a mutating call sent.
type recordedRequest struct {
	Method  string
	Path    string
	IfMatch string
	Body    map[string]any
}

func newRecordingTicketServer(t *testing.T, respond func(w http.ResponseWriter, req recordedRequest)) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var reqs []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			if len(b) > 0 {
				_ = json.Unmarshal(b, &body)
			}
		}
		rec := recordedRequest{Method: r.Method, Path: r.URL.Path, IfMatch: r.Header.Get("If-Match"), Body: body}
		reqs = append(reqs, rec)
		respond(w, rec)
	}))
	return srv, &reqs
}

// TestUpdateTicketStatusOnly proves a status-only UpdateTicket call
// with an explicit ExpectedVersion sends exactly one PATCH, no PUT.
func TestUpdateTicketStatusOnly(t *testing.T) {
	srv, reqs := newRecordingTicketServer(t, func(w http.ResponseWriter, req recordedRequest) {
		_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Type: "task", Title: "T", Status: "in_progress", Priority: "medium", Version: 2})
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	status := "in_progress"
	version := int64(1)
	ticket, err := c.UpdateTicket(t.Context(), "ABC-1", UpdateTicketOptions{Status: &status, ExpectedVersion: &version})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if ticket.Status != "in_progress" || ticket.Version != 2 {
		t.Errorf("UpdateTicket = %+v, want status=in_progress version=2", ticket)
	}
	if len(*reqs) != 1 {
		t.Fatalf("requests = %+v, want exactly one (PATCH only)", *reqs)
	}
	got := (*reqs)[0]
	if got.Method != http.MethodPatch || got.Path != "/tickets/ABC-1" || got.IfMatch != `"1"` {
		t.Errorf("request = %+v, want PATCH /tickets/ABC-1 with If-Match \"1\"", got)
	}
}

// TestUpdateTicketFieldsMergesUnsetFields proves a fields-only update
// with no ExpectedVersion does one GET to learn the version and
// current field values, then a PUT carrying the merged result — the
// unset fields (Type, Title, Description) survive unchanged.
func TestUpdateTicketFieldsMergesUnsetFields(t *testing.T) {
	srv, reqs := newRecordingTicketServer(t, func(w http.ResponseWriter, req recordedRequest) {
		switch req.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(Ticket{
				Ref: "ABC-1", Type: "bug", Title: "Original title", Description: "Original description",
				Status: "backlog", Priority: "medium", Version: 3,
			})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(Ticket{
				Ref: "ABC-1", Type: "bug", Title: "Original title", Description: "Original description",
				Status: "backlog", Priority: "high", Version: 4,
			})
		}
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	priority := "high"
	ticket, err := c.UpdateTicket(t.Context(), "ABC-1", UpdateTicketOptions{Priority: &priority})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if ticket.Priority != "high" || ticket.Version != 4 {
		t.Errorf("UpdateTicket = %+v, want priority=high version=4", ticket)
	}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %+v, want exactly two (GET then PUT)", *reqs)
	}
	get, put := (*reqs)[0], (*reqs)[1]
	if get.Method != http.MethodGet {
		t.Errorf("first request method = %s, want GET", get.Method)
	}
	if put.Method != http.MethodPut || put.IfMatch != `"3"` {
		t.Errorf("PUT request = %+v, want If-Match \"3\" (the version the GET reported)", put)
	}
	if put.Body["title"] != "Original title" || put.Body["description"] != "Original description" || put.Body["type"] != "bug" {
		t.Errorf("PUT body = %+v, want Title/Description/Type preserved from the GET", put.Body)
	}
	if put.Body["priority"] != "high" {
		t.Errorf("PUT body priority = %v, want high", put.Body["priority"])
	}
}

// TestUpdateTicketPreservesCallerExpectedVersionAcrossMergeFetch is
// the one genuinely subtle correctness property: when the caller
// supplies ExpectedVersion for a fields-only update, the internal
// merge-fetch GET (needed to fill in unset fields) must not silently
// swap in whatever version that GET happens to report — the PUT must
// still carry the caller's own ExpectedVersion, so a stale value
// correctly surfaces as a conflict instead of being quietly bypassed
// (product spec §8.4).
func TestUpdateTicketPreservesCallerExpectedVersionAcrossMergeFetch(t *testing.T) {
	srv, reqs := newRecordingTicketServer(t, func(w http.ResponseWriter, req recordedRequest) {
		switch req.Method {
		case http.MethodGet:
			// The live server reports version 5 — newer than the caller's
			// ExpectedVersion of 3, simulating a concurrent change the
			// caller doesn't know about yet.
			_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Type: "task", Title: "T", Status: "backlog", Priority: "medium", Version: 5})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Type: "task", Title: "T", Status: "backlog", Priority: "high", Version: 6})
		}
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	priority := "high"
	version := int64(3)
	if _, err := c.UpdateTicket(t.Context(), "ABC-1", UpdateTicketOptions{Priority: &priority, ExpectedVersion: &version}); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %+v, want exactly two (merge-fetch GET then PUT)", *reqs)
	}
	put := (*reqs)[1]
	if put.IfMatch != `"3"` {
		t.Errorf("PUT If-Match = %s, want \"3\" (the caller's own ExpectedVersion, not the merge-fetch GET's version 5)", put.IfMatch)
	}
}

// TestUpdateTicketStatusThenFieldsReusesPATCHResponse proves that when
// both Status and a field are set, the field-merge step reuses the
// PATCH response instead of issuing a second, redundant GET — and
// that the following PUT's If-Match reflects the version PATCH just
// bumped to, not the pre-PATCH version.
func TestUpdateTicketStatusThenFieldsReusesPATCHResponse(t *testing.T) {
	srv, reqs := newRecordingTicketServer(t, func(w http.ResponseWriter, req recordedRequest) {
		switch req.Method {
		case http.MethodPatch:
			_ = json.NewEncoder(w).Encode(Ticket{
				Ref: "ABC-1", Type: "task", Title: "Original title", Description: "Original description",
				Status: "in_progress", Priority: "medium", Version: 2,
			})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(Ticket{
				Ref: "ABC-1", Type: "task", Title: "New title", Description: "Original description",
				Status: "in_progress", Priority: "medium", Version: 3,
			})
		}
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	status, title := "in_progress", "New title"
	version := int64(1)
	ticket, err := c.UpdateTicket(t.Context(), "ABC-1", UpdateTicketOptions{Status: &status, Title: &title, ExpectedVersion: &version})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if ticket.Title != "New title" || ticket.Version != 3 {
		t.Errorf("UpdateTicket = %+v, want title=\"New title\" version=3", ticket)
	}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %+v, want exactly two (PATCH then PUT, no extra GET)", *reqs)
	}
	patch, put := (*reqs)[0], (*reqs)[1]
	if patch.Method != http.MethodPatch || patch.IfMatch != `"1"` {
		t.Errorf("PATCH request = %+v, want If-Match \"1\"", patch)
	}
	if put.Method != http.MethodPut || put.IfMatch != `"2"` {
		t.Errorf("PUT request = %+v, want If-Match \"2\" (the version PATCH just bumped to)", put)
	}
	if put.Body["description"] != "Original description" {
		t.Errorf("PUT body description = %v, want it preserved from the PATCH response, not re-fetched", put.Body["description"])
	}
}

func TestUpdateTicketNoFieldsReturnsCurrentState(t *testing.T) {
	srv, reqs := newRecordingTicketServer(t, func(w http.ResponseWriter, req recordedRequest) {
		_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Type: "task", Title: "T", Status: "backlog", Priority: "medium", Version: 1})
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	ticket, err := c.UpdateTicket(t.Context(), "ABC-1", UpdateTicketOptions{})
	if err != nil {
		t.Fatalf("UpdateTicket with no fields set: %v", err)
	}
	if ticket.Ref != "ABC-1" {
		t.Errorf("UpdateTicket = %+v, want the current ticket returned unchanged", ticket)
	}
	if len(*reqs) != 1 || (*reqs)[0].Method != http.MethodGet {
		t.Errorf("requests = %+v, want exactly one GET and no mutating call", *reqs)
	}
}

// TestAssignMoveDeleteRestoreTicket is a straightforward request-shape
// round trip for the four remaining ticket write methods.
func TestAssignMoveDeleteRestoreTicket(t *testing.T) {
	srv, reqs := newRecordingTicketServer(t, func(w http.ResponseWriter, req recordedRequest) {
		switch req.Path {
		case "/tickets/ABC-1/assign":
			_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Version: 2})
		case "/tickets/ABC-1/move":
			_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Feature: "ABC-F2", Version: 3})
		case "/tickets/ABC-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 4})
		case "/tickets/ABC-1/restore":
			_ = json.NewEncoder(w).Encode(Ticket{Ref: "ABC-1", Version: 5})
		}
	})
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	assignee := "agent:codex"
	if _, err := c.AssignTicket(t.Context(), "ABC-1", &assignee, 1); err != nil {
		t.Fatalf("AssignTicket: %v", err)
	}
	if _, err := c.MoveTicket(t.Context(), "ABC-1", "ABC-F2", 2); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	newVersion, err := c.DeleteTicket(t.Context(), "ABC-1", 3)
	if err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if newVersion != 4 {
		t.Errorf("DeleteTicket returned version %d, want 4", newVersion)
	}
	if _, err := c.RestoreTicket(t.Context(), "ABC-1", 4); err != nil {
		t.Fatalf("RestoreTicket: %v", err)
	}

	wantMethods := []string{http.MethodPost, http.MethodPost, http.MethodDelete, http.MethodPost}
	wantIfMatch := []string{`"1"`, `"2"`, `"3"`, `"4"`}
	if len(*reqs) != 4 {
		t.Fatalf("requests = %+v, want exactly four", *reqs)
	}
	for i, r := range *reqs {
		if r.Method != wantMethods[i] {
			t.Errorf("request %d method = %s, want %s", i, r.Method, wantMethods[i])
		}
		if r.IfMatch != wantIfMatch[i] {
			t.Errorf("request %d If-Match = %s, want %s", i, r.IfMatch, wantIfMatch[i])
		}
	}
	if (*reqs)[0].Body["assignee"] != "agent:codex" {
		t.Errorf("assign body = %+v, want assignee=agent:codex", (*reqs)[0].Body)
	}
	if (*reqs)[1].Body["feature"] != "ABC-F2" {
		t.Errorf("move body = %+v, want feature=ABC-F2", (*reqs)[1].Body)
	}
}
