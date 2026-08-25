package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestProjectUpdateAndArchiveOverHTTP is Phase 7's wiring check for
// PATCH /projects/{key} and POST /projects/{key}/status (ADR 0021),
// mirroring TestFeatureLifecycleOverHTTP/TestUpdateFeatureStatusOverHTTP:
// update succeeds and reindexes; archiving flips status and excludes
// the project from GET /projects' default page but not
// ?include_archived=true; GET /projects/{key} stays reachable
// regardless; unarchiving restores default visibility.
func TestProjectUpdateAndArchiveOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	createResp, createBody := ts.do(http.MethodPost, "/projects", nil,
		mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var proj map[string]any
	_ = json.Unmarshal(createBody, &proj)
	version := int64(proj["version"].(float64))

	// --- update ---
	updResp, updBody := ts.do(http.MethodPatch, "/projects/ABC",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Example v2", "description": "renamed"}))
	if updResp.StatusCode != http.StatusOK {
		t.Fatalf("update project status = %d, body=%s", updResp.StatusCode, updBody)
	}
	var updated map[string]any
	_ = json.Unmarshal(updBody, &updated)
	if updated["title"] != "Example v2" {
		t.Errorf("updated project title = %v, want %q", updated["title"], "Example v2")
	}
	version = int64(updated["version"].(float64))

	// --- stale If-Match on update is a 409 carrying the current version ---
	staleResp, staleBody := ts.do(http.MethodPatch, "/projects/ABC",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version-1, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Example v3"}))
	if staleResp.StatusCode != http.StatusConflict {
		t.Fatalf("stale update status = %d, body=%s, want 409", staleResp.StatusCode, staleBody)
	}

	// --- archive ---
	archResp, archBody := ts.do(http.MethodPost, "/projects/ABC/status",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"status": "archived"}))
	if archResp.StatusCode != http.StatusOK {
		t.Fatalf("archive project status = %d, body=%s", archResp.StatusCode, archBody)
	}
	var archived map[string]any
	_ = json.Unmarshal(archBody, &archived)
	if archived["status"] != "archived" {
		t.Errorf("archived project status = %v, want %q", archived["status"], "archived")
	}
	version = int64(archived["version"].(float64))

	// --- GET /projects/{key} stays reachable while archived ---
	getResp, getBody := ts.do(http.MethodGet, "/projects/ABC", nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get archived project status = %d, body=%s, want 200", getResp.StatusCode, getBody)
	}

	// --- excluded from the default list ---
	listResp, listBody := ts.do(http.MethodGet, "/projects", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list projects status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page map[string]any
	_ = json.Unmarshal(listBody, &page)
	if projects, _ := page["projects"].([]any); len(projects) != 0 {
		t.Errorf("default list = %v, want empty (ABC is archived)", projects)
	}

	// --- present with ?include_archived=true ---
	incResp, incBody := ts.do(http.MethodGet, "/projects?include_archived=true", nil, nil)
	if incResp.StatusCode != http.StatusOK {
		t.Fatalf("list projects (include_archived) status = %d, body=%s", incResp.StatusCode, incBody)
	}
	var incPage map[string]any
	_ = json.Unmarshal(incBody, &incPage)
	if projects, _ := incPage["projects"].([]any); len(projects) != 1 {
		t.Errorf("include_archived list = %v, want 1 (ABC)", projects)
	}

	// --- unarchive ---
	unarchResp, unarchBody := ts.do(http.MethodPost, "/projects/ABC/status",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"status": "active"}))
	if unarchResp.StatusCode != http.StatusOK {
		t.Fatalf("unarchive project status = %d, body=%s", unarchResp.StatusCode, unarchBody)
	}

	backResp, backBody := ts.do(http.MethodGet, "/projects", nil, nil)
	if backResp.StatusCode != http.StatusOK {
		t.Fatalf("list projects after unarchive status = %d, body=%s", backResp.StatusCode, backBody)
	}
	var backPage map[string]any
	_ = json.Unmarshal(backBody, &backPage)
	if projects, _ := backPage["projects"].([]any); len(projects) != 1 {
		t.Errorf("default list after unarchive = %v, want 1 (ABC present again)", projects)
	}
}
