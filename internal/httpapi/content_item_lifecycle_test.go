package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestContentItemArchiveOverHTTP mirrors
// TestProjectUpdateAndArchiveOverHTTP (ADR 0028): archiving a plan or
// document via POST /{kind}/{ref}/status flips status and excludes it
// from the default GET /projects/{key}/{kind} page but not
// ?include_archived=true; GET /{kind}/{ref} stays reachable regardless;
// unarchiving restores default visibility.
func TestContentItemArchiveOverHTTP(t *testing.T) {
	for _, kind := range []string{"plans", "documents"} {
		t.Run(kind, func(t *testing.T) {
			ts := newTestServer(t)
			ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

			createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/"+kind, nil,
				mustJSON(t, map[string]string{"title": "Old", "body": "stale"}))
			if createResp.StatusCode != http.StatusCreated {
				t.Fatalf("create %s status = %d, body=%s", kind, createResp.StatusCode, createBody)
			}
			var item map[string]any
			_ = json.Unmarshal(createBody, &item)
			ref, _ := item["ref"].(string)
			if item["status"] != "active" {
				t.Errorf("created %s status = %v, want active", kind, item["status"])
			}
			version := int64(item["version"].(float64))

			// --- stale If-Match on the status endpoint is a 409 ---
			staleResp, staleBody := ts.do(http.MethodPost, "/"+kind+"/"+ref+"/status",
				map[string]string{"If-Match": `"` + strconv.FormatInt(version+1, 10) + `"`},
				mustJSON(t, map[string]string{"status": "archived"}))
			if staleResp.StatusCode != http.StatusConflict {
				t.Fatalf("stale archive status = %d, body=%s, want 409", staleResp.StatusCode, staleBody)
			}

			// --- archive ---
			archResp, archBody := ts.do(http.MethodPost, "/"+kind+"/"+ref+"/status",
				map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
				mustJSON(t, map[string]string{"status": "archived"}))
			if archResp.StatusCode != http.StatusOK {
				t.Fatalf("archive %s status = %d, body=%s", kind, archResp.StatusCode, archBody)
			}
			var archived map[string]any
			_ = json.Unmarshal(archBody, &archived)
			if archived["status"] != "archived" {
				t.Errorf("archived %s status = %v, want archived", kind, archived["status"])
			}
			if archived["body"] != "stale" {
				t.Errorf("archived %s body = %v, want unchanged %q — archiving is not a content edit", kind, archived["body"], "stale")
			}
			version = int64(archived["version"].(float64))

			// --- GET /{kind}/{ref} stays reachable while archived ---
			getResp, getBody := ts.do(http.MethodGet, "/"+kind+"/"+ref, nil, nil)
			if getResp.StatusCode != http.StatusOK {
				t.Fatalf("get archived %s status = %d, body=%s, want 200", kind, getResp.StatusCode, getBody)
			}

			// --- excluded from the default project list ---
			listResp, listBody := ts.do(http.MethodGet, "/projects/ABC/"+kind, nil, nil)
			if listResp.StatusCode != http.StatusOK {
				t.Fatalf("list %s status = %d, body=%s", kind, listResp.StatusCode, listBody)
			}
			var page map[string]any
			_ = json.Unmarshal(listBody, &page)
			if items, _ := page["items"].([]any); len(items) != 0 {
				t.Errorf("default %s list = %v, want empty (archived)", kind, items)
			}

			// --- present with ?include_archived=true ---
			incResp, incBody := ts.do(http.MethodGet, "/projects/ABC/"+kind+"?include_archived=true", nil, nil)
			if incResp.StatusCode != http.StatusOK {
				t.Fatalf("list %s (include_archived) status = %d, body=%s", kind, incResp.StatusCode, incBody)
			}
			var incPage map[string]any
			_ = json.Unmarshal(incBody, &incPage)
			if items, _ := incPage["items"].([]any); len(items) != 1 {
				t.Errorf("include_archived %s list = %v, want 1", kind, items)
			}

			// --- unarchive ---
			unarchResp, unarchBody := ts.do(http.MethodPost, "/"+kind+"/"+ref+"/status",
				map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
				mustJSON(t, map[string]string{"status": "active"}))
			if unarchResp.StatusCode != http.StatusOK {
				t.Fatalf("unarchive %s status = %d, body=%s", kind, unarchResp.StatusCode, unarchBody)
			}

			backResp, backBody := ts.do(http.MethodGet, "/projects/ABC/"+kind, nil, nil)
			if backResp.StatusCode != http.StatusOK {
				t.Fatalf("list %s after unarchive status = %d, body=%s", kind, backResp.StatusCode, backBody)
			}
			var backPage map[string]any
			_ = json.Unmarshal(backBody, &backPage)
			if items, _ := backPage["items"].([]any); len(items) != 1 {
				t.Errorf("default %s list after unarchive = %v, want 1 (present again)", kind, items)
			}
		})
	}
}
