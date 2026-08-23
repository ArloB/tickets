package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

type ticketsPageForTest struct {
	Tickets []struct {
		Ref string `json:"ref"`
	} `json:"tickets"`
	NextCursor string `json:"next_cursor"`
}

// TestListTicketsFilterByStatusOverHTTP exercises the new ?status=
// query param end to end (routing, service validation, store SQL),
// and against the OpenAPI schema the do() helper validates every call
// with.
func TestListTicketsFilterByStatusOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	_, backlogBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"title": "stays backlog", "type": "task"}))
	var backlog struct {
		Ref string `json:"ref"`
	}
	_ = json.Unmarshal(backlogBody, &backlog)

	_, movedBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"title": "moves along", "type": "task"}))
	var moved struct {
		Ref     string `json:"ref"`
		Version int64  `json:"version"`
	}
	_ = json.Unmarshal(movedBody, &moved)
	patchResp, patchBody := ts.do(http.MethodPatch, "/tickets/"+moved.Ref,
		map[string]string{"If-Match": `"` + strconv.FormatInt(moved.Version, 10) + `"`},
		mustJSON(t, map[string]string{"status": "in_progress"}))
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("move to in_progress: status = %d, body=%s", patchResp.StatusCode, patchBody)
	}

	resp, body := ts.do(http.MethodGet, "/projects/ABC/tickets?status=in_progress", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered list: status = %d, body=%s", resp.StatusCode, body)
	}
	var page ticketsPageForTest
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(page.Tickets) != 1 || page.Tickets[0].Ref != moved.Ref {
		t.Fatalf("filtered list = %+v, want exactly %s", page.Tickets, moved.Ref)
	}
}

// TestListTicketsFilterInvalidFeatureRefOverHTTP confirms an
// unrecognized ?feature_ref= value comes back as a clean 400
// validation_failed, not a 500 or a silently-empty page. Uses
// feature_ref rather than an enum-shaped filter (priority/status/etc)
// because those are now constrained in the OpenAPI schema itself
// (api/openapi.yaml's FilterPriority et al.) — the OpenAPI request
// validator ts.do() runs on every call rejects an out-of-enum value
// before the request ever reaches the handler, which proves the
// contract but can't exercise the server's own validation path the
// way this test wants to. feature_ref is deliberately just `type:
// string` in the schema, so this request reaches ListTicketsFiltered
// and its resolveTicketFilters check for real.
func TestListTicketsFilterInvalidFeatureRefOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	resp, body := ts.do(http.MethodGet, "/projects/ABC/tickets?feature_ref=ABC-F999", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown feature_ref filter: status = %d, body=%s, want 400", resp.StatusCode, body)
	}
}

// TestListFeaturesFilterByPriorityOverHTTP mirrors the ticket-filter
// HTTP test for the smaller feature filter surface.
func TestListFeaturesFilterByPriorityOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	_, criticalBody := ts.do(http.MethodPost, "/projects/ABC/features", nil, mustJSON(t, map[string]string{"title": "Critical", "priority": "critical"}))
	var critical struct {
		Ref string `json:"ref"`
	}
	_ = json.Unmarshal(criticalBody, &critical)
	ts.do(http.MethodPost, "/projects/ABC/features", nil, mustJSON(t, map[string]string{"title": "Medium"}))

	resp, body := ts.do(http.MethodGet, "/projects/ABC/features?priority=critical", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered list: status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Features []struct {
			Ref string `json:"ref"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(page.Features) != 1 || page.Features[0].Ref != critical.Ref {
		t.Fatalf("filtered list = %+v, want exactly %s", page.Features, critical.Ref)
	}
}
