package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestTicketLifecycleMutationsOverHTTP is Step 13's route-wiring exit
// check: full-field update, assign, move, reorder, delete, restore —
// each validated against api/openapi.yaml — plus Creator/Assignee now
// actually reaching the wire.
func TestTicketLifecycleMutationsOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]string{"type": "task", "title": "T"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket: %d, body=%s", createResp.StatusCode, createBody)
	}
	var ticket map[string]any
	_ = json.Unmarshal(createBody, &ticket)
	if ticket["creator"] != "human:test-admin" {
		t.Errorf("created ticket creator = %v, want human:test-admin", ticket["creator"])
	}
	version := int64(ticket["version"].(float64))

	// --- PUT: full-field update ---
	putResp, putBody := ts.do(http.MethodPut, "/tickets/ABC-1",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]any{"type": "bug", "title": "T2", "description": "d", "priority": "high", "severity": "high"}))
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT ticket status = %d, body=%s", putResp.StatusCode, putBody)
	}
	var updated map[string]any
	_ = json.Unmarshal(putBody, &updated)
	if updated["type"] != "bug" || updated["title"] != "T2" || updated["severity"] != "high" {
		t.Errorf("PUT-updated ticket = %+v, want type=bug title=T2 severity=high", updated)
	}
	version = int64(updated["version"].(float64))

	// --- assign ---
	assignResp, assignBody := ts.do(http.MethodPost, "/tickets/ABC-1/assign",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"assignee": "human:test-admin"}))
	if assignResp.StatusCode != http.StatusOK {
		t.Fatalf("assign status = %d, body=%s", assignResp.StatusCode, assignBody)
	}
	var assigned map[string]any
	_ = json.Unmarshal(assignBody, &assigned)
	if assigned["assignee"] != "human:test-admin" {
		t.Errorf("assigned ticket assignee = %v, want human:test-admin", assigned["assignee"])
	}
	version = int64(assigned["version"].(float64))

	// --- unassign (null clears it) ---
	unassignResp, unassignBody := ts.do(http.MethodPost, "/tickets/ABC-1/assign",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]any{"assignee": nil}))
	if unassignResp.StatusCode != http.StatusOK {
		t.Fatalf("unassign status = %d, body=%s", unassignResp.StatusCode, unassignBody)
	}
	var unassigned map[string]any
	_ = json.Unmarshal(unassignBody, &unassigned)
	if _, present := unassigned["assignee"]; present {
		t.Errorf("unassigned ticket still has an assignee key: %v", unassigned["assignee"])
	}
	version = int64(unassigned["version"].(float64))

	// --- move to a new feature ---
	featResp, featBody := ts.do(http.MethodPost, "/projects/ABC/features", nil,
		mustJSON(t, map[string]string{"title": "Payments", "priority": "medium"}))
	if featResp.StatusCode != http.StatusCreated {
		t.Fatalf("create feature: %d, body=%s", featResp.StatusCode, featBody)
	}
	var feature map[string]any
	_ = json.Unmarshal(featBody, &feature)
	featureRef, _ := feature["ref"].(string)

	moveResp, moveBody := ts.do(http.MethodPost, "/tickets/ABC-1/move",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"feature": featureRef}))
	if moveResp.StatusCode != http.StatusOK {
		t.Fatalf("move status = %d, body=%s", moveResp.StatusCode, moveBody)
	}
	var moved map[string]any
	_ = json.Unmarshal(moveBody, &moved)
	if moved["feature"] != featureRef {
		t.Errorf("moved ticket feature = %v, want %v", moved["feature"], featureRef)
	}
	version = int64(moved["version"].(float64))

	// --- reorder (to head of its own group) ---
	reorderResp, reorderBody := ts.do(http.MethodPost, "/tickets/ABC-1/reorder",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]any{"after_ref": nil}))
	if reorderResp.StatusCode != http.StatusOK {
		t.Fatalf("reorder status = %d, body=%s", reorderResp.StatusCode, reorderBody)
	}
	var reordered map[string]any
	_ = json.Unmarshal(reorderBody, &reordered)
	version = int64(reordered["version"].(float64))

	// --- delete ---
	deleteResp, deleteBody := ts.do(http.MethodDelete, "/tickets/ABC-1",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`}, nil)
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, body=%s", deleteResp.StatusCode, deleteBody)
	}
	var deleted map[string]any
	_ = json.Unmarshal(deleteBody, &deleted)
	newVersion := int64(deleted["version"].(float64))
	if newVersion != version+1 {
		t.Errorf("delete returned version = %d, want %d", newVersion, version+1)
	}

	goneResp, _ := ts.do(http.MethodGet, "/tickets/ABC-1", nil, nil)
	if goneResp.StatusCode != http.StatusNotFound {
		t.Errorf("get deleted ticket status = %d, want 404", goneResp.StatusCode)
	}

	// --- restore ---
	restoreResp, restoreBody := ts.do(http.MethodPost, "/tickets/ABC-1/restore",
		map[string]string{"If-Match": `"` + strconv.FormatInt(newVersion, 10) + `"`}, nil)
	if restoreResp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d, body=%s", restoreResp.StatusCode, restoreBody)
	}

	backResp, _ := ts.do(http.MethodGet, "/tickets/ABC-1", nil, nil)
	if backResp.StatusCode != http.StatusOK {
		t.Errorf("get restored ticket status = %d, want 200", backResp.StatusCode)
	}
}

// TestMoveTicketToDifferentProjectFeatureRejected confirms ADR 0001's
// cross-project move guard survives translation to HTTP.
func TestMoveTicketToDifferentProjectFeatureRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "A"}))
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "XYZ", "title": "B"}))
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]string{"type": "task", "title": "T"}))
	var ticket map[string]any
	_ = json.Unmarshal(createBody, &ticket)
	version := int64(ticket["version"].(float64))
	_ = createResp

	resp, body := ts.do(http.MethodPost, "/tickets/ABC-1/move",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"feature": "XYZ-F1"}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-project move status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

// TestAssignTicketToUnknownActorRejected confirms AssignTicket's
// "assignee must already exist as a seeded actor" rule (service/ticket.go's
// doc) surfaces as a client-fixable validation_failed, not a 500.
func TestAssignTicketToUnknownActorRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]string{"type": "task", "title": "T"}))
	var ticket map[string]any
	_ = json.Unmarshal(createBody, &ticket)
	version := int64(ticket["version"].(float64))
	_ = createResp

	resp, body := ts.do(http.MethodPost, "/tickets/ABC-1/assign",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"assignee": "human:nobody"}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("assign to unknown actor status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}
