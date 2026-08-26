package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestContentItemLifecycleOverHTTP is Phase 5 Step 3's route-wiring
// exit check: create -> get -> update -> stale-version conflict ->
// associate with a ticket -> list, for both a plan and a document (they
// share the same table/handlers, discriminated only by URL prefix and
// reference kind), each response validated against api/openapi.yaml by
// ts.do.
func TestContentItemLifecycleOverHTTP(t *testing.T) {
	for _, kind := range []string{"plans", "documents"} {
		t.Run(kind, func(t *testing.T) {
			ts := newTestServer(t)
			ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
			ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))

			// --- create ---
			createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/"+kind, nil,
				mustJSON(t, map[string]string{"title": "Rollout", "body": "Step one"}))
			if createResp.StatusCode != http.StatusCreated {
				t.Fatalf("create %s status = %d, body=%s", kind, createResp.StatusCode, createBody)
			}
			var item map[string]any
			if err := json.Unmarshal(createBody, &item); err != nil {
				t.Fatalf("unmarshal %s: %v", kind, err)
			}
			ref, _ := item["ref"].(string)
			if ref == "" {
				t.Fatalf("created %s has no ref: %v", kind, item)
			}
			if item["representation"] != "markdown" {
				t.Errorf("created %s representation = %v, want markdown", kind, item["representation"])
			}
			if item["body"] != "Step one" {
				t.Errorf("created %s body = %v, want %q", kind, item["body"], "Step one")
			}
			version := int64(item["version"].(float64))

			// --- get ---
			getResp, getBody := ts.do(http.MethodGet, "/"+kind+"/"+ref, nil, nil)
			if getResp.StatusCode != http.StatusOK {
				t.Fatalf("get %s status = %d, body=%s", kind, getResp.StatusCode, getBody)
			}

			// --- update ---
			updateResp, updateBody := ts.do(http.MethodPatch, "/"+kind+"/"+ref,
				map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
				mustJSON(t, map[string]string{"title": "Rollout (final)", "body": "Step one\nStep two"}))
			if updateResp.StatusCode != http.StatusOK {
				t.Fatalf("update %s status = %d, body=%s", kind, updateResp.StatusCode, updateBody)
			}
			var updated map[string]any
			_ = json.Unmarshal(updateBody, &updated)
			if updated["title"] != "Rollout (final)" || updated["body"] != "Step one\nStep two" {
				t.Errorf("updated %s = %v, want title/body updated", kind, updated)
			}
			newVersion := int64(updated["version"].(float64))
			if newVersion != version+1 {
				t.Errorf("updated %s version = %d, want %d", kind, newVersion, version+1)
			}

			// --- stale version conflict ---
			conflictResp, conflictBody := ts.do(http.MethodPatch, "/"+kind+"/"+ref,
				map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
				mustJSON(t, map[string]string{"title": "Stale write", "body": "x"}))
			if conflictResp.StatusCode != http.StatusConflict {
				t.Fatalf("stale update %s status = %d, want 409, body=%s", kind, conflictResp.StatusCode, conflictBody)
			}

			// --- associate with a ticket ---
			assocResp, assocBody := ts.do(http.MethodPost, "/tickets/ABC-1/associations", nil, mustJSON(t, map[string]string{"target": ref}))
			if assocResp.StatusCode != http.StatusCreated {
				t.Fatalf("associate ticket with %s status = %d, body=%s", kind, assocResp.StatusCode, assocBody)
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
			listResp, listBody := ts.do(http.MethodGet, "/projects/ABC/"+kind, nil, nil)
			if listResp.StatusCode != http.StatusOK {
				t.Fatalf("list %s status = %d, body=%s", kind, listResp.StatusCode, listBody)
			}
			var page struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(listBody, &page); err != nil {
				t.Fatalf("unmarshal %s page: %v", kind, err)
			}
			if len(page.Items) != 1 || page.Items[0]["ref"] != ref {
				t.Errorf("%s list = %+v, want exactly item %s", kind, page.Items, ref)
			}
			// Compact rows must not carry the body field.
			if _, ok := page.Items[0]["body"]; ok {
				t.Errorf("%s list row = %+v, want no body field (compact rows only)", kind, page.Items[0])
			}
		})
	}
}

// TestPlanAndDocumentNumberIndependently proves the shared-table
// reference numbering doesn't collide: a plan and a document created in
// the same project both land on seq=1, distinguished by their kind
// code (P vs DOC).
func TestPlanAndDocumentNumberIndependently(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	_, planBody := ts.do(http.MethodPost, "/projects/ABC/plans", nil, mustJSON(t, map[string]string{"title": "P"}))
	var plan map[string]any
	_ = json.Unmarshal(planBody, &plan)
	if plan["ref"] != "ABC-P1" {
		t.Errorf("plan ref = %v, want ABC-P1", plan["ref"])
	}

	_, docBody := ts.do(http.MethodPost, "/projects/ABC/documents", nil, mustJSON(t, map[string]string{"title": "D"}))
	var doc map[string]any
	_ = json.Unmarshal(docBody, &doc)
	if doc["ref"] != "ABC-DOC1" {
		t.Errorf("document ref = %v, want ABC-DOC1", doc["ref"])
	}
}

// TestContentItemGetRejectsTicketReference proves parseContentItemRef's
// kind guard: a syntactically valid but wrong-kind reference is
// validation_failed, not a confusing not_found.
func TestContentItemGetRejectsTicketReference(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))

	resp, body := ts.do(http.MethodGet, "/plans/ABC-1", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("get plan with a ticket ref status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

// TestContentItemRouteRejectsCrossKindReference is a regression test
// for a code-review finding: parseContentItemRef used to accept *any*
// plan-or-document reference regardless of which specific route it was
// reached through, so GET/PATCH /documents/{ref} would happily read or
// write a plan by its ABC-P... ref (and vice versa) since both resolve
// through the same content_items table. Every content-item handler now
// binds its expected kind via closure from the route registration
// (server.go), the same way createContentItem/listContentItems already
// did — this proves the fix across get/update/versions/diff.
func TestContentItemRouteRejectsCrossKindReference(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	_, planBody := ts.do(http.MethodPost, "/projects/ABC/plans", nil, mustJSON(t, map[string]string{"title": "P"}))
	var plan map[string]any
	_ = json.Unmarshal(planBody, &plan)
	planRef, _ := plan["ref"].(string)

	if resp, body := ts.do(http.MethodGet, "/documents/"+planRef, nil, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("get plan %s via /documents/... status = %d, want 400, body=%s", planRef, resp.StatusCode, body)
	}
	if resp, body := ts.do(http.MethodPatch, "/documents/"+planRef, map[string]string{"If-Match": `"1"`}, mustJSON(t, map[string]string{"title": "x", "body": "y"})); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("update plan %s via /documents/... status = %d, want 400, body=%s", planRef, resp.StatusCode, body)
	}
	if resp, body := ts.do(http.MethodGet, "/documents/"+planRef+"/versions", nil, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("list plan %s versions via /documents/... status = %d, want 400, body=%s", planRef, resp.StatusCode, body)
	}
	if resp, body := ts.do(http.MethodGet, "/documents/"+planRef+"/diff?from=1&to=1", nil, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("diff plan %s via /documents/... status = %d, want 400, body=%s", planRef, resp.StatusCode, body)
	}

	// And the reverse: a document via /plans/...
	_, docBody := ts.do(http.MethodPost, "/projects/ABC/documents", nil, mustJSON(t, map[string]string{"title": "D"}))
	var doc map[string]any
	_ = json.Unmarshal(docBody, &doc)
	docRef, _ := doc["ref"].(string)

	if resp, body := ts.do(http.MethodGet, "/plans/"+docRef, nil, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("get document %s via /plans/... status = %d, want 400, body=%s", docRef, resp.StatusCode, body)
	}
}

// TestContentItemVersionsAndDiffOverHTTP mirrors
// TestDecisionVersionsAndDiffOverHTTP for a document.
func TestContentItemVersionsAndDiffOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/documents", nil,
		mustJSON(t, map[string]string{"title": "Notes", "body": "line one\nline two"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create document status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var created map[string]any
	_ = json.Unmarshal(createBody, &created)
	ref, _ := created["ref"].(string)
	version := int64(created["version"].(float64))

	updateResp, updateBody := ts.do(http.MethodPatch, "/documents/"+ref,
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"title": "Notes", "body": "line one\nline three"}))
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update document status = %d, body=%s", updateResp.StatusCode, updateBody)
	}
	var updated map[string]any
	_ = json.Unmarshal(updateBody, &updated)
	newVersion := int64(updated["version"].(float64))

	// --- versions ---
	versionsResp, versionsBody := ts.do(http.MethodGet, "/documents/"+ref+"/versions", nil, nil)
	if versionsResp.StatusCode != http.StatusOK {
		t.Fatalf("list document versions status = %d, body=%s", versionsResp.StatusCode, versionsBody)
	}
	var versionsPage struct {
		Versions []map[string]any `json:"versions"`
	}
	_ = json.Unmarshal(versionsBody, &versionsPage)
	if len(versionsPage.Versions) != 1 {
		t.Fatalf("document versions = %+v, want exactly 1 archived version", versionsPage.Versions)
	}
	if versionsPage.Versions[0]["body"] != "line one\nline two" {
		t.Errorf("archived version body = %v, want the pre-update value", versionsPage.Versions[0]["body"])
	}

	// --- diff ---
	diffResp, diffBody := ts.do(http.MethodGet,
		"/documents/"+ref+"/diff?from="+strconv.FormatInt(version, 10)+"&to="+strconv.FormatInt(newVersion, 10), nil, nil)
	if diffResp.StatusCode != http.StatusOK {
		t.Fatalf("get document diff status = %d, body=%s", diffResp.StatusCode, diffBody)
	}
	var diff struct {
		Body []map[string]string `json:"body"`
	}
	if err := json.Unmarshal(diffBody, &diff); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}
	wantOps := []string{"equal", "remove", "add"}
	if len(diff.Body) != len(wantOps) {
		t.Fatalf("diff.body = %+v, want %d lines", diff.Body, len(wantOps))
	}
	for i, op := range wantOps {
		if diff.Body[i]["op"] != op {
			t.Errorf("diff.body[%d].op = %q, want %q", i, diff.Body[i]["op"], op)
		}
	}
}

// TestContentItemDiffRejectsUnknownVersionOverHTTP mirrors
// TestDecisionDiffRejectsUnknownVersionOverHTTP.
func TestContentItemDiffRejectsUnknownVersionOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/plans", nil, mustJSON(t, map[string]string{"title": "T"}))

	resp, body := ts.do(http.MethodGet, "/plans/ABC-P1/diff?from=1&to=99", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("diff against version 99 status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

// TestContentItemMentionBacklinkOverHTTP proves a ticket mentioning a
// plan via #ABC-P1 shows up in the plan's backlinks.
func TestContentItemMentionBacklinkOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	_, planBody := ts.do(http.MethodPost, "/projects/ABC/plans", nil, mustJSON(t, map[string]string{"title": "Plan"}))
	var plan map[string]any
	_ = json.Unmarshal(planBody, &plan)
	planRef, _ := plan["ref"].(string)

	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "T", "description": "See #" + planRef, "general": true}))

	resp, body := ts.do(http.MethodGet, "/plans/"+planRef+"/backlinks", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get plan backlinks status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Backlinks []map[string]any `json:"backlinks"`
	}
	_ = json.Unmarshal(body, &page)
	found := false
	for _, b := range page.Backlinks {
		if b["ref"] == "ABC-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("plan backlinks = %+v, want it to include ABC-1", page.Backlinks)
	}
}
