package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestListTicketsPaginationOverHTTP is Step 14's route-wiring exit
// check for the default (priority-queue) view: create enough tickets
// to force a second page, confirm the cursor round-trips through the
// wire and the two pages together cover every created ticket exactly
// once.
func TestListTicketsPaginationOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	want := map[string]bool{}
	for i := 0; i < 3; i++ {
		resp, body := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
			mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create ticket: %d, body=%s", resp.StatusCode, body)
		}
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		want[created["ref"].(string)] = true
	}

	got := map[string]bool{}
	resp, body := ts.do(http.MethodGet, "/projects/ABC/tickets?limit=2", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tickets page 1: %d, body=%s", resp.StatusCode, body)
	}
	var page1 map[string]any
	_ = json.Unmarshal(body, &page1)
	items1, _ := page1["tickets"].([]any)
	if len(items1) != 2 {
		t.Fatalf("page 1 len = %d, want 2", len(items1))
	}
	for _, it := range items1 {
		got[it.(map[string]any)["ref"].(string)] = true
	}
	cursor, _ := page1["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("page 1 next_cursor is empty, want a cursor for the remaining ticket")
	}

	resp, body = ts.do(http.MethodGet, "/projects/ABC/tickets?limit=2&cursor="+cursor, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tickets page 2: %d, body=%s", resp.StatusCode, body)
	}
	var page2 map[string]any
	_ = json.Unmarshal(body, &page2)
	items2, _ := page2["tickets"].([]any)
	if len(items2) != 1 {
		t.Fatalf("page 2 len = %d, want 1", len(items2))
	}
	for _, it := range items2 {
		got[it.(map[string]any)["ref"].(string)] = true
	}

	if len(got) != len(want) {
		t.Fatalf("pages together returned %d distinct refs, want %d", len(got), len(want))
	}
	for ref := range want {
		if !got[ref] {
			t.Errorf("ref %q missing from either page", ref)
		}
	}
}

// TestListTicketsIssueRegisterViewOverHTTP confirms ?view=issue_register
// filters to bug/security tickets only (product spec §5.5) — a task
// ticket is created alongside a bug to prove the task is excluded, not
// just that the bug is present.
func TestListTicketsIssueRegisterViewOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "task", "title": "A task", "general": true}))
	bugResp, bugBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "bug", "title": "A bug", "severity": "high", "general": true}))
	if bugResp.StatusCode != http.StatusCreated {
		t.Fatalf("create bug: %d, body=%s", bugResp.StatusCode, bugBody)
	}
	var bug map[string]any
	_ = json.Unmarshal(bugBody, &bug)

	resp, body := ts.do(http.MethodGet, "/projects/ABC/tickets?view=issue_register", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list issue register: %d, body=%s", resp.StatusCode, body)
	}
	var page map[string]any
	_ = json.Unmarshal(body, &page)
	items, _ := page["tickets"].([]any)
	if len(items) != 1 {
		t.Fatalf("issue register len = %d, want 1 (only the bug)", len(items))
	}
	if items[0].(map[string]any)["ref"] != bug["ref"] {
		t.Errorf("issue register item = %v, want the bug %v", items[0], bug["ref"])
	}
}

// TestListTicketsInvalidViewOverHTTP confirms an unrecognized ?view=
// value is validation_failed, not a silent fallback to the default.
// view's OpenAPI parameter is itself enum-restricted to the two known
// values, so a real client following the spec would never send this —
// this test deliberately sends a request the schema forbids to prove
// the server's own validation (ticket_list.go's default case) still
// runs and returns a clean error rather than a 500, so it goes through
// doUnvalidated rather than do.
func TestListTicketsInvalidViewOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	resp, body := ts.doUnvalidated(http.MethodGet, "/projects/ABC/tickets?view=bogus", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("list tickets bad view: %d, want 400, body=%s", resp.StatusCode, body)
	}
	var envelope map[string]any
	_ = json.Unmarshal(body, &envelope)
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "validation_failed" {
		t.Errorf("error.code = %v, want validation_failed", errObj["code"])
	}
}

// TestListTicketsFieldsNarrowsResponseOverHTTP exercises ?fields= on
// the list endpoint. Its success body is a subset object per list
// item, not the full TicketCompact shape the operation's 200 schema
// documents, so it goes through doUnvalidated rather than do (see that
// helper's doc comment and the fields param's description in
// api/openapi.yaml).
func TestListTicketsFieldsNarrowsResponseOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))

	resp, body := ts.doUnvalidated(http.MethodGet, "/projects/ABC/tickets?fields=title,status", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tickets fields=: %d, body=%s", resp.StatusCode, body)
	}
	var page map[string]any
	_ = json.Unmarshal(body, &page)
	items, _ := page["tickets"].([]any)
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	item := items[0].(map[string]any)
	if len(item) != 2 {
		t.Errorf("projected item has %d keys, want exactly 2 (title, status): %v", len(item), item)
	}
	if _, ok := item["title"]; !ok {
		t.Errorf("projected item missing title: %v", item)
	}
	if _, ok := item["status"]; !ok {
		t.Errorf("projected item missing status: %v", item)
	}
	if _, ok := item["ref"]; ok {
		t.Errorf("projected item has unrequested field ref: %v", item)
	}
}

// TestListTicketsUnknownFieldRejectedOverHTTP confirms an unknown
// ?fields= name is validation_failed rather than silently ignored —
// this response is the normal 400 error envelope, so it goes through
// the regular ts.do contract validation.
func TestListTicketsUnknownFieldRejectedOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	resp, body := ts.do(http.MethodGet, "/projects/ABC/tickets?fields=nope", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("list tickets unknown field: %d, want 400, body=%s", resp.StatusCode, body)
	}
	var envelope map[string]any
	_ = json.Unmarshal(body, &envelope)
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "validation_failed" {
		t.Errorf("error.code = %v, want validation_failed", errObj["code"])
	}
	if errObj["field"] != "fields" {
		t.Errorf("error.field = %v, want fields", errObj["field"])
	}
}

// TestGetTicketIncludeExpandsResponseOverHTTP confirms ?include=
// comments,relationships populates ticketDetail's optional Comments/
// Relationships arrays. Unlike fields=, include= only adds optional
// properties the Ticket schema already declares, so the expanded
// response still validates and this goes through the regular ts.do.
func TestGetTicketIncludeExpandsResponseOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket: %d, body=%s", createResp.StatusCode, createBody)
	}
	var ticket map[string]any
	_ = json.Unmarshal(createBody, &ticket)
	ref, _ := ticket["ref"].(string)

	commentResp, commentBody := ts.do(http.MethodPost, "/tickets/"+ref+"/comments", nil,
		mustJSON(t, map[string]string{"body": "a comment"}))
	if commentResp.StatusCode != http.StatusCreated {
		t.Fatalf("create comment: %d, body=%s", commentResp.StatusCode, commentBody)
	}

	resp, body := ts.do(http.MethodGet, "/tickets/"+ref+"?include=comments,relationships", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get ticket include=: %d, body=%s", resp.StatusCode, body)
	}
	var detail map[string]any
	_ = json.Unmarshal(body, &detail)
	comments, ok := detail["comments"].([]any)
	if !ok || len(comments) != 1 {
		t.Fatalf("detail.comments = %v, want a 1-element array", detail["comments"])
	}
	relationships, ok := detail["relationships"].([]any)
	if !ok || len(relationships) != 0 {
		t.Fatalf("detail.relationships = %v, want an empty array", detail["relationships"])
	}

	// Without ?include=, neither key appears at all (the pointer stays
	// nil, and omitempty drops it) — confirms include= is opt-in, not
	// always-on now that the fields exist on the schema.
	plainResp, plainBody := ts.do(http.MethodGet, "/tickets/"+ref, nil, nil)
	if plainResp.StatusCode != http.StatusOK {
		t.Fatalf("get ticket: %d, body=%s", plainResp.StatusCode, plainBody)
	}
	var plain map[string]any
	_ = json.Unmarshal(plainBody, &plain)
	if _, ok := plain["comments"]; ok {
		t.Errorf("plain get exposes comments without ?include=: %v", plain)
	}
	if _, ok := plain["relationships"]; ok {
		t.Errorf("plain get exposes relationships without ?include=: %v", plain)
	}
}

// TestGetTicketFieldsNarrowsResponseOverHTTP exercises ?fields= on the
// single-ticket endpoint — same subset-object shape as the list
// endpoint's fields=, so it also goes through doUnvalidated.
func TestGetTicketFieldsNarrowsResponseOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket: %d, body=%s", createResp.StatusCode, createBody)
	}
	var ticket map[string]any
	_ = json.Unmarshal(createBody, &ticket)
	ref, _ := ticket["ref"].(string)

	resp, body := ts.doUnvalidated(http.MethodGet, "/tickets/"+ref+"?fields=ref,title", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get ticket fields=: %d, body=%s", resp.StatusCode, body)
	}
	var projected map[string]any
	_ = json.Unmarshal(body, &projected)
	if len(projected) != 2 {
		t.Fatalf("projected has %d keys, want exactly 2 (ref, title): %v", len(projected), projected)
	}
	if projected["ref"] != ref {
		t.Errorf("projected ref = %v, want %v", projected["ref"], ref)
	}
	if projected["title"] != "T" {
		t.Errorf("projected title = %v, want T", projected["title"])
	}
}

// TestGetTicketUnknownFieldRejectedOverHTTP mirrors the list
// endpoint's equivalent test for the single-ticket route.
func TestGetTicketUnknownFieldRejectedOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	_, createBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	var ticket map[string]any
	_ = json.Unmarshal(createBody, &ticket)
	ref, _ := ticket["ref"].(string)

	resp, body := ts.do(http.MethodGet, "/tickets/"+ref+"?fields=nope", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("get ticket unknown field: %d, want 400, body=%s", resp.StatusCode, body)
	}
	var envelope map[string]any
	_ = json.Unmarshal(body, &envelope)
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "validation_failed" {
		t.Errorf("error.code = %v, want validation_failed", errObj["code"])
	}
}
