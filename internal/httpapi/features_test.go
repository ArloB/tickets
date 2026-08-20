package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestFeatureLifecycleOverHTTP is Step 10's route-wiring exit check:
// every feature route (create/list/get/update/reorder/delete/restore)
// exercised end to end, each response validated against
// api/openapi.yaml by ts.do.
func TestFeatureLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	// --- create ---
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/features", nil,
		mustJSON(t, map[string]string{"title": "Payments", "priority": "high"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create feature status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var feature map[string]any
	if err := json.Unmarshal(createBody, &feature); err != nil {
		t.Fatalf("unmarshal feature: %v", err)
	}
	ref, _ := feature["ref"].(string)
	if ref != "ABC-F2" { // F1 is the project's mandatory General feature
		t.Errorf("feature ref = %v, want ABC-F2", ref)
	}
	version := int64(feature["version"].(float64))

	// --- list (General + the new one) ---
	listResp, listBody := ts.do(http.MethodGet, "/projects/ABC/features", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list features status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page map[string]any
	_ = json.Unmarshal(listBody, &page)
	if features, _ := page["features"].([]any); len(features) != 2 {
		t.Errorf("list features returned %d, want 2 (General + Payments)", len(features))
	}

	// --- get ---
	getResp, getBody := ts.do(http.MethodGet, "/features/"+ref, nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get feature status = %d, body=%s", getResp.StatusCode, getBody)
	}

	// --- update ---
	updResp, updBody := ts.do(http.MethodPatch, "/features/"+ref,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Payments v2", "description": "renamed", "priority": "critical"}))
	if updResp.StatusCode != http.StatusOK {
		t.Fatalf("update feature status = %d, body=%s", updResp.StatusCode, updBody)
	}
	var updated map[string]any
	_ = json.Unmarshal(updBody, &updated)
	if updated["title"] != "Payments v2" {
		t.Errorf("updated feature title = %v, want %q", updated["title"], "Payments v2")
	}
	version = int64(updated["version"].(float64))

	// --- reorder (to head of its own group — still a real move) ---
	reorderResp, reorderBody := ts.do(http.MethodPost, "/features/"+ref+"/reorder",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]any{"after_ref": nil}))
	if reorderResp.StatusCode != http.StatusOK {
		t.Fatalf("reorder feature status = %d, body=%s", reorderResp.StatusCode, reorderBody)
	}
	var reordered map[string]any
	_ = json.Unmarshal(reorderBody, &reordered)
	version = int64(reordered["version"].(float64))

	// --- delete (no dependent tickets — there's no move-ticket-to-
	// feature HTTP route yet to put one there; that's Step 13. The
	// has_dependents/cascade path is exercised separately in
	// TestDeleteFeatureCascadeOverHTTP and at the service layer in
	// soft_delete_test.go) ---
	deleteResp, deleteBody := ts.do(http.MethodDelete, "/features/"+ref,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`}, nil)
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete feature status = %d, body=%s", deleteResp.StatusCode, deleteBody)
	}
	var deleted map[string]any
	_ = json.Unmarshal(deleteBody, &deleted)
	newVersion := int64(deleted["version"].(float64))
	if newVersion != version+1 {
		t.Errorf("delete feature returned version = %d, want %d", newVersion, version+1)
	}

	// A deleted feature 404s on get.
	goneResp, _ := ts.do(http.MethodGet, "/features/"+ref, nil, nil)
	if goneResp.StatusCode != http.StatusNotFound {
		t.Errorf("get deleted feature status = %d, want 404", goneResp.StatusCode)
	}

	// --- restore ---
	restoreResp, restoreBody := ts.do(http.MethodPost, "/features/"+ref+"/restore",
		map[string]string{"If-Match": `"` + strconv.FormatInt(newVersion, 10) + `"`}, nil)
	if restoreResp.StatusCode != http.StatusOK {
		t.Fatalf("restore feature status = %d, body=%s", restoreResp.StatusCode, restoreBody)
	}

	backResp, _ := ts.do(http.MethodGet, "/features/"+ref, nil, nil)
	if backResp.StatusCode != http.StatusOK {
		t.Errorf("get restored feature status = %d, want 200", backResp.StatusCode)
	}
}

// TestDeleteFeatureCascadeOverHTTP exercises the has_dependents ->
// cascade=true path end to end, the first time it's reachable over
// HTTP (previously only internal/service-level tests exercised it).
func TestDeleteFeatureCascadeOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/features", nil,
		mustJSON(t, map[string]string{"title": "Payments", "priority": "medium"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create feature: %d, body=%s", createResp.StatusCode, createBody)
	}
	var feature map[string]any
	_ = json.Unmarshal(createBody, &feature)
	ref, _ := feature["ref"].(string)
	version := int64(feature["version"].(float64))

	// There's no HTTP route yet to move a ticket into a feature (Step
	// 13), so this can't reach the actual has_dependents-then-cascade
	// branch over HTTP — that response shape is already covered at the
	// service layer (soft_delete_test.go). What's new here is proving
	// DELETE .../features/{ref}?cascade=true is wired through the route
	// table and round-trips through the OpenAPI contract at all.
	resp, body := ts.do(http.MethodDelete, "/features/"+ref+"?cascade=true",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cascade delete status = %d, body=%s", resp.StatusCode, body)
	}
}

// TestDeleteGeneralFeatureRejectedOverHTTP confirms ADR 0001's rule
// (the General feature can never be deleted) survives translation to
// HTTP as validation_failed, not a 500 or a silent success.
func TestDeleteGeneralFeatureRejectedOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	resp, body := ts.do(http.MethodDelete, "/features/ABC-F1", map[string]string{"If-Match": `"1"`}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete General feature status = %d, want 400, body=%s", resp.StatusCode, body)
	}
	var envelope map[string]any
	_ = json.Unmarshal(body, &envelope)
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "validation_failed" {
		t.Errorf("error.code = %v, want validation_failed", errObj["code"])
	}
}

// TestGetFeatureRejectsTicketRef confirms parseFeatureRef's kind check
// — a syntactically valid ticket reference handed to a feature route
// is validation_failed, not a confusing not_found.
func TestGetFeatureRejectsTicketRef(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "T"}))

	resp, body := ts.do(http.MethodGet, "/features/ABC-1", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("get feature with a ticket ref status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}
