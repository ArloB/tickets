package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestDecisionLifecycleOverHTTP is Phase 3 Step 6's route-wiring exit
// check: create -> get -> update -> stale-version conflict ->
// associate with a ticket -> list, each response validated against
// api/openapi.yaml by ts.do.
func TestDecisionLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "T"}))

	// --- create ---
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/decisions", nil,
		mustJSON(t, map[string]string{"title": "Use SQLite", "context": "We need a store", "decision": "Use SQLite", "rationale": "Simplicity"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create decision status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var decision map[string]any
	if err := json.Unmarshal(createBody, &decision); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}
	ref, _ := decision["ref"].(string)
	if ref != "ABC-D1" {
		t.Errorf("decision ref = %v, want ABC-D1", ref)
	}
	if decision["status"] != "proposed" {
		t.Errorf("decision status = %v, want proposed", decision["status"])
	}
	version := int64(decision["version"].(float64))

	// --- get ---
	getResp, getBody := ts.do(http.MethodGet, "/decisions/"+ref, nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get decision status = %d, body=%s", getResp.StatusCode, getBody)
	}

	// --- update ---
	updateResp, updateBody := ts.do(http.MethodPatch, "/decisions/"+ref,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Use SQLite (final)", "context": "We need a store", "decision": "Use SQLite", "rationale": "Simplicity", "status": "accepted"}))
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update decision status = %d, body=%s", updateResp.StatusCode, updateBody)
	}
	var updated map[string]any
	_ = json.Unmarshal(updateBody, &updated)
	if updated["status"] != "accepted" || updated["title"] != "Use SQLite (final)" {
		t.Errorf("updated decision = %v, want status=accepted title=%q", updated, "Use SQLite (final)")
	}
	newVersion := int64(updated["version"].(float64))
	if newVersion != version+1 {
		t.Errorf("updated version = %d, want %d", newVersion, version+1)
	}

	// --- stale version conflict ---
	conflictResp, conflictBody := ts.do(http.MethodPatch, "/decisions/"+ref,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Stale write", "status": "accepted"}))
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("stale update status = %d, want 409, body=%s", conflictResp.StatusCode, conflictBody)
	}

	// --- associate with a ticket ---
	assocResp, assocBody := ts.do(http.MethodPost, "/tickets/ABC-1/associations", nil, mustJSON(t, map[string]string{"target": ref}))
	if assocResp.StatusCode != http.StatusCreated {
		t.Fatalf("associate ticket with decision status = %d, body=%s", assocResp.StatusCode, assocBody)
	}
	assocListResp, assocListBody := ts.do(http.MethodGet, "/tickets/ABC-1/associations", nil, nil)
	if assocListResp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket associations status = %d, body=%s", assocListResp.StatusCode, assocListBody)
	}
	var assocPage struct {
		Associated []string `json:"associated"`
	}
	_ = json.Unmarshal(assocListBody, &assocPage)
	found := false
	for _, a := range assocPage.Associated {
		if a == ref {
			found = true
		}
	}
	if !found {
		t.Errorf("ticket associations = %v, want it to include %s", assocPage.Associated, ref)
	}

	// --- list ---
	listResp, listBody := ts.do(http.MethodGet, "/projects/ABC/decisions", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list decisions status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page struct {
		Decisions []map[string]any `json:"decisions"`
	}
	if err := json.Unmarshal(listBody, &page); err != nil {
		t.Fatalf("unmarshal decisions page: %v", err)
	}
	if len(page.Decisions) != 1 || page.Decisions[0]["ref"] != ref {
		t.Errorf("decisions list = %+v, want exactly decision %s", page.Decisions, ref)
	}
	// Compact rows must not carry the full-text fields.
	if _, ok := page.Decisions[0]["context"]; ok {
		t.Errorf("decisions list row = %+v, want no context field (compact rows only)", page.Decisions[0])
	}
}

// TestDecisionGetRejectsTicketReference proves parseDecisionRef's
// kind guard: a syntactically valid but wrong-kind reference is
// validation_failed, not a confusing not_found.
func TestDecisionGetRejectsTicketReference(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "T"}))

	resp, body := ts.do(http.MethodGet, "/decisions/ABC-1", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("get decision with a ticket ref status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}
