package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestDecisionVersionsAndDiffOverHTTP is Phase 5 Step 2's route-wiring
// exit check: create -> update -> GET .../versions shows the archived
// pre-update state -> GET .../diff?from=&to= shows the field-level
// change, all validated against api/openapi.yaml by ts.do.
func TestDecisionVersionsAndDiffOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/decisions", nil,
		mustJSON(t, map[string]string{"title": "Use SQLite", "context": "line one\nline two", "consequences": "cheap"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create decision status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var created map[string]any
	_ = json.Unmarshal(createBody, &created)
	ref, _ := created["ref"].(string)
	version := int64(created["version"].(float64))
	if created["consequences"] != "cheap" {
		t.Errorf("created decision consequences = %v, want %q", created["consequences"], "cheap")
	}

	updateResp, updateBody := ts.do(http.MethodPatch, "/decisions/"+ref,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Use SQLite", "context": "line one\nline three", "consequences": "still cheap", "status": "accepted"}))
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update decision status = %d, body=%s", updateResp.StatusCode, updateBody)
	}
	var updated map[string]any
	_ = json.Unmarshal(updateBody, &updated)
	newVersion := int64(updated["version"].(float64))

	// --- versions ---
	versionsResp, versionsBody := ts.do(http.MethodGet, "/decisions/"+ref+"/versions", nil, nil)
	if versionsResp.StatusCode != http.StatusOK {
		t.Fatalf("list decision versions status = %d, body=%s", versionsResp.StatusCode, versionsBody)
	}
	var versionsPage struct {
		Versions []map[string]any `json:"versions"`
	}
	_ = json.Unmarshal(versionsBody, &versionsPage)
	if len(versionsPage.Versions) != 1 {
		t.Fatalf("decision versions = %+v, want exactly 1 archived version", versionsPage.Versions)
	}
	if versionsPage.Versions[0]["context"] != "line one\nline two" {
		t.Errorf("archived version context = %v, want the pre-update value", versionsPage.Versions[0]["context"])
	}
	if versionsPage.Versions[0]["status"] != "proposed" {
		t.Errorf("archived version status = %v, want proposed", versionsPage.Versions[0]["status"])
	}

	// --- diff ---
	diffResp, diffBody := ts.do(http.MethodGet,
		"/decisions/"+ref+"/diff?from="+strconv.FormatInt(version, 10)+"&to="+strconv.FormatInt(newVersion, 10), nil, nil)
	if diffResp.StatusCode != http.StatusOK {
		t.Fatalf("get decision diff status = %d, body=%s", diffResp.StatusCode, diffBody)
	}
	var diff struct {
		Context    []map[string]string `json:"context"`
		StatusFrom string              `json:"status_from"`
		StatusTo   string              `json:"status_to"`
	}
	if err := json.Unmarshal(diffBody, &diff); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}
	if diff.StatusFrom != "proposed" || diff.StatusTo != "accepted" {
		t.Errorf("diff status = %s -> %s, want proposed -> accepted", diff.StatusFrom, diff.StatusTo)
	}
	wantOps := []string{"equal", "remove", "add"}
	if len(diff.Context) != len(wantOps) {
		t.Fatalf("diff.context = %+v, want %d lines", diff.Context, len(wantOps))
	}
	for i, op := range wantOps {
		if diff.Context[i]["op"] != op {
			t.Errorf("diff.context[%d].op = %q, want %q", i, diff.Context[i]["op"], op)
		}
	}
}

// TestDecisionDiffRejectsUnknownVersionOverHTTP proves a syntactically
// valid but nonexistent version number (something OpenAPI's static
// integer/minimum schema can't catch, unlike a malformed string) is 400
// validation_failed, not a 500 — the httpapi-level counterpart to
// internal/service's TestGetDecisionDiffRejectsUnknownVersion.
func TestDecisionDiffRejectsUnknownVersionOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/decisions", nil, mustJSON(t, map[string]string{"title": "T"}))

	resp, body := ts.do(http.MethodGet, "/decisions/ABC-D1/diff?from=1&to=99", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("diff against version 99 status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

// TestDecisionSupersessionOverHTTP proves superseded_by round-trips
// through PATCH and GET.
func TestDecisionSupersessionOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	_, oldBody := ts.do(http.MethodPost, "/projects/ABC/decisions", nil, mustJSON(t, map[string]string{"title": "Old"}))
	var old map[string]any
	_ = json.Unmarshal(oldBody, &old)
	oldRef, _ := old["ref"].(string)
	oldVersion := int64(old["version"].(float64))

	_, newBody := ts.do(http.MethodPost, "/projects/ABC/decisions", nil, mustJSON(t, map[string]string{"title": "New"}))
	var newDecision map[string]any
	_ = json.Unmarshal(newBody, &newDecision)
	newRef, _ := newDecision["ref"].(string)

	updateResp, updateBody := ts.do(http.MethodPatch, "/decisions/"+oldRef,
		map[string]string{"If-Match": `"` + strconv.FormatInt(oldVersion, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Old", "status": "superseded", "superseded_by": newRef}))
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("supersede update status = %d, body=%s", updateResp.StatusCode, updateBody)
	}
	var updated map[string]any
	_ = json.Unmarshal(updateBody, &updated)
	if updated["superseded_by"] != newRef {
		t.Errorf("updated.superseded_by = %v, want %q", updated["superseded_by"], newRef)
	}

	getResp, getBody := ts.do(http.MethodGet, "/decisions/"+oldRef, nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get decision status = %d, body=%s", getResp.StatusCode, getBody)
	}
	var fetched map[string]any
	_ = json.Unmarshal(getBody, &fetched)
	if fetched["superseded_by"] != newRef {
		t.Errorf("fetched.superseded_by = %v, want %q", fetched["superseded_by"], newRef)
	}
}
