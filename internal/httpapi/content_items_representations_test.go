package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func createTestProjectForContentItems(t *testing.T, ts *testServer) {
	t.Helper()
	ts.doUnvalidated(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
}

// TestCreateDocumentFileRepresentationOverHTTP covers create (upload)
// -> get -> download -> replace (new version) -> download version 1
// from history, over the real HTTP surface.
func TestCreateDocumentFileRepresentationOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	createTestProjectForContentItems(t, ts)

	createResp, createBody := ts.doMultipart(http.MethodPost, "/projects/ABC/documents", nil,
		map[string]string{"title": "spec"}, "file", "spec.pdf", []byte("pdf v1"))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create document (file) status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var doc map[string]any
	if err := json.Unmarshal(createBody, &doc); err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	ref, _ := doc["ref"].(string)
	if doc["representation"] != "file" {
		t.Errorf("representation = %v, want file", doc["representation"])
	}
	if doc["file_name"] != "spec.pdf" {
		t.Errorf("file_name = %v, want spec.pdf", doc["file_name"])
	}

	downloadResp, downloadBody := ts.doUnvalidated(http.MethodGet, "/documents/"+ref+"/download", nil, nil)
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", downloadResp.StatusCode)
	}
	if string(downloadBody) != "pdf v1" {
		t.Errorf("downloaded content = %q, want %q", downloadBody, "pdf v1")
	}

	version := int64(doc["version"].(float64))
	replaceResp, replaceBody := ts.doMultipart(http.MethodPatch, "/documents/"+ref, ifMatch(float64(version)),
		map[string]string{"title": "spec"}, "file", "spec-v2.pdf", []byte("pdf v2"))
	if replaceResp.StatusCode != http.StatusOK {
		t.Fatalf("replace document status = %d, body=%s", replaceResp.StatusCode, replaceBody)
	}

	_, downloadBody2 := ts.doUnvalidated(http.MethodGet, "/documents/"+ref+"/download", nil, nil)
	if string(downloadBody2) != "pdf v2" {
		t.Errorf("downloaded content after replace = %q, want %q", downloadBody2, "pdf v2")
	}

	_, v1Body := ts.doUnvalidated(http.MethodGet, "/documents/"+ref+"/versions/1/download", nil, nil)
	if string(v1Body) != "pdf v1" {
		t.Errorf("version 1 download = %q, want original content", v1Body)
	}
}

// TestCreatePlanPathRepresentationNeverRead is ADR 0007's boundary as
// a regression test for content items, mirroring
// TestAttachmentPathReferenceNeverRead: a path-representation plan is
// created successfully, but the download route never opens its
// target.
func TestCreatePlanPathRepresentationNeverRead(t *testing.T) {
	ts := newTestServer(t)
	createTestProjectForContentItems(t, ts)

	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/plans", nil,
		mustJSON(t, map[string]string{"title": "external plan", "representation": "path", "path": "/etc/passwd"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create plan (path) status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var plan map[string]any
	if err := json.Unmarshal(createBody, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	if plan["representation"] != "path" {
		t.Fatalf("representation = %v, want path", plan["representation"])
	}
	if plan["path_value"] != "/etc/passwd" {
		t.Errorf("path_value = %v, want /etc/passwd", plan["path_value"])
	}
	ref, _ := plan["ref"].(string)

	downloadResp, downloadBody := ts.doUnvalidated(http.MethodGet, "/plans/"+ref+"/download", nil, nil)
	if downloadResp.StatusCode == http.StatusOK {
		t.Fatalf("download of a path-representation plan returned 200 with body %q — its target must never be served", downloadBody)
	}
}

// TestCreateDocumentURLRepresentationOverHTTP covers create + get for
// the url representation, which has no download affordance (a url
// representation is a link, not downloadable content).
func TestCreateDocumentURLRepresentationOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	createTestProjectForContentItems(t, ts)

	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/documents", nil,
		mustJSON(t, map[string]string{"title": "wiki page", "representation": "url", "url": "https://wiki.example.com/page"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create document (url) status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var doc map[string]any
	if err := json.Unmarshal(createBody, &doc); err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	if doc["url_value"] != "https://wiki.example.com/page" {
		t.Errorf("url_value = %v, want https://wiki.example.com/page", doc["url_value"])
	}
	ref, _ := doc["ref"].(string)

	getResp, getBody := ts.do(http.MethodGet, "/documents/"+ref, nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get document status = %d, body=%s", getResp.StatusCode, getBody)
	}
}

// TestUpdateContentItemCannotChangeRepresentation confirms a plan
// created as markdown stays markdown after an update — there is no
// representation field on the update request at all, so this proves
// the immutability holds through the real HTTP surface, not just in
// unit tests against the service layer directly.
func TestUpdateContentItemCannotChangeRepresentation(t *testing.T) {
	ts := newTestServer(t)
	createTestProjectForContentItems(t, ts)

	createResp, createBody := ts.do(http.MethodPost, "/projects/ABC/plans", nil,
		mustJSON(t, map[string]string{"title": "plan", "body": "original body"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create plan status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var plan map[string]any
	if err := json.Unmarshal(createBody, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	ref, _ := plan["ref"].(string)
	version := plan["version"].(float64)

	updateResp, updateBody := ts.do(http.MethodPatch, "/plans/"+ref, ifMatch(version),
		mustJSON(t, map[string]string{"title": "plan", "body": "updated body", "path": "/should/be/ignored", "url": "https://should-be-ignored.example.com"}))
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update plan status = %d, body=%s", updateResp.StatusCode, updateBody)
	}
	var updated map[string]any
	if err := json.Unmarshal(updateBody, &updated); err != nil {
		t.Fatalf("unmarshal updated plan: %v", err)
	}
	if updated["representation"] != "markdown" {
		t.Errorf("representation after update = %v, want markdown", updated["representation"])
	}
	if updated["body"] != "updated body" {
		t.Errorf("body after update = %v, want %q", updated["body"], "updated body")
	}
	if _, ok := updated["path_value"]; ok {
		t.Errorf("path_value should be absent on a markdown item, got %v", updated["path_value"])
	}
}
