package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// doMultipart is do's counterpart for multipart/form-data requests —
// attachments' upload routes are the only ones in the whole API that
// aren't uniform JSON (see the Phase 5 plan's OpenAPI note), so they
// go through the raw client the same way doUnvalidated does for
// non-JSON-schema cases, rather than through do's OpenAPI validation.
func (ts *testServer) doMultipart(method, path string, headers map[string]string, fields map[string]string, fileField, fileName string, fileContent []byte) (*http.Response, []byte) {
	t := ts.t
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if fileField != "" {
		fw, err := mw.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(fileContent); err != nil {
			t.Fatalf("write file content: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(method, ts.url+"/api/v1"+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if ts.sessionID != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ts.sessionID})
	}
	if ts.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", ts.csrfToken)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do multipart request %s %s: %v", method, path, err)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	_ = resp.Body.Close()
	return resp, respBody
}

func createTestTicket(t *testing.T, ts *testServer) string {
	t.Helper()
	ts.doUnvalidated(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	_, body := ts.doUnvalidated(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]string{"type": "task", "title": "T"}))
	var ticket map[string]any
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	return ticket["ref"].(string)
}

func ifMatch(version float64) map[string]string {
	return map[string]string{"If-Match": `"` + strconv.FormatInt(int64(version), 10) + `"`}
}

// TestAttachmentUploadLifecycle covers create (upload) -> list -> get
// metadata -> download bytes -> new version -> download again -> stale
// If-Match conflict -> delete -> tombstone no longer downloadable.
func TestAttachmentUploadLifecycle(t *testing.T) {
	ts := newTestServer(t)
	ref := createTestTicket(t, ts)

	createResp, createBody := ts.doMultipart(http.MethodPost, "/tickets/"+ref+"/attachments", nil,
		map[string]string{"title": "design notes"}, "file", "notes.txt", []byte("hello, attachments"))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create attachment status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var attachment map[string]any
	if err := json.Unmarshal(createBody, &attachment); err != nil {
		t.Fatalf("unmarshal attachment: %v", err)
	}
	id := int64(attachment["id"].(float64))
	idStr := strconv.FormatInt(id, 10)
	if attachment["owner_ref"] != ref {
		t.Errorf("owner_ref = %v, want %v", attachment["owner_ref"], ref)
	}
	if attachment["kind"] != "upload" {
		t.Errorf("kind = %v, want upload", attachment["kind"])
	}
	if attachment["current_version"].(float64) != 1 {
		t.Errorf("current_version = %v, want 1", attachment["current_version"])
	}

	// --- list ---
	_, listBody := ts.do(http.MethodGet, "/tickets/"+ref+"/attachments", nil, nil)
	var list struct {
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list.Attachments) != 1 {
		t.Fatalf("listed %d attachments, want 1", len(list.Attachments))
	}

	// --- get metadata ---
	getResp, getBody := ts.do(http.MethodGet, "/attachments/"+idStr, nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get attachment status = %d, body=%s", getResp.StatusCode, getBody)
	}

	// --- download ---
	downloadResp, downloadBody := ts.doUnvalidated(http.MethodGet, "/attachments/"+idStr+"/download", nil, nil)
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", downloadResp.StatusCode)
	}
	if string(downloadBody) != "hello, attachments" {
		t.Errorf("downloaded content = %q, want %q", downloadBody, "hello, attachments")
	}
	if got := downloadResp.Header.Get("Content-Disposition"); got == "" {
		t.Error("download response missing Content-Disposition header")
	}

	// --- stale If-Match on replace is rejected ---
	staleResp, staleBody := ts.doMultipart(http.MethodPut, "/attachments/"+idStr, ifMatch(999),
		nil, "file", "notes-v2.txt", []byte("revised content"))
	if staleResp.StatusCode != http.StatusConflict {
		t.Fatalf("replace with stale If-Match status = %d, body=%s, want 409", staleResp.StatusCode, staleBody)
	}

	// --- replace (new version) ---
	replaceResp, replaceBody := ts.doMultipart(http.MethodPut, "/attachments/"+idStr, ifMatch(attachment["current_version"].(float64)),
		nil, "file", "notes-v2.txt", []byte("revised content"))
	if replaceResp.StatusCode != http.StatusOK {
		t.Fatalf("replace attachment status = %d, body=%s", replaceResp.StatusCode, replaceBody)
	}
	var replaced map[string]any
	if err := json.Unmarshal(replaceBody, &replaced); err != nil {
		t.Fatalf("unmarshal replaced attachment: %v", err)
	}
	if replaced["current_version"].(float64) != 2 {
		t.Errorf("current_version after replace = %v, want 2", replaced["current_version"])
	}

	// --- download again returns the new content ---
	_, downloadBody2 := ts.doUnvalidated(http.MethodGet, "/attachments/"+idStr+"/download", nil, nil)
	if string(downloadBody2) != "revised content" {
		t.Errorf("downloaded content after replace = %q, want %q", downloadBody2, "revised content")
	}

	// --- version 1 is still downloadable from history ---
	_, v1Body := ts.doUnvalidated(http.MethodGet, "/attachments/"+idStr+"/versions/1/download", nil, nil)
	if string(v1Body) != "hello, attachments" {
		t.Errorf("version 1 download = %q, want original content", v1Body)
	}

	// --- version history lists both versions ---
	_, versionsBody := ts.do(http.MethodGet, "/attachments/"+idStr+"/versions", nil, nil)
	var versions struct {
		Versions []map[string]any `json:"versions"`
	}
	if err := json.Unmarshal(versionsBody, &versions); err != nil {
		t.Fatalf("unmarshal versions: %v", err)
	}
	if len(versions.Versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(versions.Versions))
	}

	// --- delete (soft) ---
	deleteResp, deleteBody := ts.do(http.MethodDelete, "/attachments/"+idStr, ifMatch(2), nil)
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete attachment status = %d, body=%s", deleteResp.StatusCode, deleteBody)
	}

	// --- tombstoned attachment is no longer reachable by id ---
	afterDeleteResp, _ := ts.do(http.MethodGet, "/attachments/"+idStr, nil, nil)
	if afterDeleteResp.StatusCode != http.StatusNotFound {
		t.Errorf("get deleted attachment status = %d, want 404", afterDeleteResp.StatusCode)
	}
}

// TestAttachmentPathReferenceNeverRead is ADR 0007's boundary as a
// regression test: a path attachment is created and listed
// successfully, but no route ever opens its target — the download
// route returns a validation error, not the file's bytes, even though
// the path names a real, readable file on this machine.
func TestAttachmentPathReferenceNeverRead(t *testing.T) {
	ts := newTestServer(t)
	ref := createTestTicket(t, ts)

	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve me"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	createResp, createBody := ts.do(http.MethodPost, "/tickets/"+ref+"/attachments", nil,
		mustJSON(t, map[string]string{"title": "design doc", "path": secret}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create path attachment status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var attachment map[string]any
	if err := json.Unmarshal(createBody, &attachment); err != nil {
		t.Fatalf("unmarshal attachment: %v", err)
	}
	if attachment["kind"] != "path" {
		t.Fatalf("kind = %v, want path", attachment["kind"])
	}
	if attachment["path_value"] != secret {
		t.Errorf("path_value = %v, want %v", attachment["path_value"], secret)
	}
	id := int64(attachment["id"].(float64))

	downloadResp, downloadBody := ts.doUnvalidated(http.MethodGet, "/attachments/"+strconv.FormatInt(id, 10)+"/download", nil, nil)
	if downloadResp.StatusCode == http.StatusOK {
		t.Fatalf("download of a path attachment returned 200 with body %q — a path attachment's target must never be served", downloadBody)
	}
	if bytes.Contains(downloadBody, []byte("do not serve me")) {
		t.Fatalf("response leaked the path attachment's file contents: %s", downloadBody)
	}
}

// TestAttachmentUploadTooLarge shrinks maxUploadSize for the duration
// of the test so this doesn't need to actually generate a 25 MiB
// request body.
func TestAttachmentUploadTooLarge(t *testing.T) {
	ts := newTestServer(t)
	ref := createTestTicket(t, ts)

	original := maxUploadSize
	maxUploadSize = 8
	t.Cleanup(func() { maxUploadSize = original })

	resp, body := ts.doMultipart(http.MethodPost, "/tickets/"+ref+"/attachments", nil,
		map[string]string{"title": "too big"}, "file", "big.bin", []byte("this is way more than 8 bytes"))
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload status = %d, body=%s, want 413", resp.StatusCode, body)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal error envelope: %v", err)
	}
	if envelope.Error.Code != "upload_too_large" {
		t.Errorf("error code = %q, want upload_too_large", envelope.Error.Code)
	}
}

// TestAttachmentOnComment confirms an attachment can target a comment
// instead of a principal entity, and is listed/scoped accordingly.
func TestAttachmentOnComment(t *testing.T) {
	ts := newTestServer(t)
	ref := createTestTicket(t, ts)

	_, commentBody := ts.do(http.MethodPost, "/tickets/"+ref+"/comments", nil, mustJSON(t, map[string]string{"body": "see attached"}))
	var comment map[string]any
	if err := json.Unmarshal(commentBody, &comment); err != nil {
		t.Fatalf("unmarshal comment: %v", err)
	}
	commentID := strconv.FormatInt(int64(comment["id"].(float64)), 10)

	createResp, createBody := ts.doMultipart(http.MethodPost, "/comments/"+commentID+"/attachments", nil,
		map[string]string{"title": "screenshot"}, "file", "shot.png", []byte("fake png bytes"))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create comment attachment status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var attachment map[string]any
	if err := json.Unmarshal(createBody, &attachment); err != nil {
		t.Fatalf("unmarshal attachment: %v", err)
	}
	if attachment["comment_id"].(float64) != comment["id"].(float64) {
		t.Errorf("comment_id = %v, want %v", attachment["comment_id"], comment["id"])
	}
	if _, ok := attachment["owner_ref"]; ok {
		t.Errorf("owner_ref should be absent for a comment-scoped attachment, got %v", attachment["owner_ref"])
	}

	_, listBody := ts.do(http.MethodGet, "/comments/"+commentID+"/attachments", nil, nil)
	var list struct {
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list.Attachments) != 1 {
		t.Fatalf("listed %d attachments on comment, want 1", len(list.Attachments))
	}

	// The ticket's own attachment list must not include the comment's.
	_, ticketListBody := ts.do(http.MethodGet, "/tickets/"+ref+"/attachments", nil, nil)
	var ticketList struct {
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal(ticketListBody, &ticketList); err != nil {
		t.Fatalf("unmarshal ticket list: %v", err)
	}
	if len(ticketList.Attachments) != 0 {
		t.Errorf("ticket attachment list = %d entries, want 0 (the attachment belongs to the comment, not the ticket)", len(ticketList.Attachments))
	}
}

// TestAttachmentRequiresExactlyOneOwner isn't reachable through the
// HTTP surface today (every route already supplies exactly one of
// ref/comment id), but CreateAttachment's validation is exercised
// indirectly by every other test in this file succeeding — this test
// instead confirms a path attachment missing its path value is
// rejected, the other half of buildAttachmentFields' validation.
func TestAttachmentPathRequiresPathValue(t *testing.T) {
	ts := newTestServer(t)
	ref := createTestTicket(t, ts)

	// doUnvalidated, not do: api/openapi.yaml's AddPathAttachmentRequest
	// requires "path" at the schema level too, so this specifically
	// exercises internal/service's own validation, not request-schema
	// rejection.
	resp, body := ts.doUnvalidated(http.MethodPost, "/tickets/"+ref+"/attachments", nil,
		mustJSON(t, map[string]string{"title": "no path"}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create path attachment without path status = %d, body=%s, want 400", resp.StatusCode, body)
	}
}

// TestAttachmentFilenameCannotInjectResponseHeaders is Phase 6 Step
// 7's backfill of a Phase 2-deferred test: writeAttachmentDownload
// builds Content-Disposition via fmt.Sprintf("...filename=%q", ...),
// and %q's Go-syntax quoting escapes every control character and
// embedded quote into a literal backslash sequence — so even a
// filename containing raw CR/LF or a `"` can never break out of the
// header value or inject a second header line. This writes the
// malicious filename directly into the row (bypassing multipart
// upload, whose own parser may or may not let such bytes survive
// through Filename in the first place — the invariant that actually
// matters is what the download response does with whatever ends up in
// file_name, not how it got there) so the assertion holds regardless
// of upload-side parsing behavior.
func TestAttachmentFilenameCannotInjectResponseHeaders(t *testing.T) {
	ts := newTestServer(t)
	ref := createTestTicket(t, ts)

	createResp, createBody := ts.doMultipart(http.MethodPost, "/tickets/"+ref+"/attachments", nil,
		map[string]string{"title": "design notes"}, "file", "notes.txt", []byte("hello"))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create attachment status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var attachment map[string]any
	if err := json.Unmarshal(createBody, &attachment); err != nil {
		t.Fatalf("unmarshal attachment: %v", err)
	}
	id := int64(attachment["id"].(float64))

	malicious := "evil\r\nX-Injected: yes\r\nSet-Cookie: hacked=true\".txt"
	if _, err := ts.store.DB().Exec(`UPDATE attachments SET file_name = ? WHERE id = ?`, malicious, id); err != nil {
		t.Fatalf("seed malicious file_name: %v", err)
	}

	downloadResp, downloadBody := ts.doUnvalidated(http.MethodGet, "/attachments/"+strconv.FormatInt(id, 10)+"/download", nil, nil)
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, body=%s", downloadResp.StatusCode, downloadBody)
	}
	if string(downloadBody) != "hello" {
		t.Errorf("downloaded content = %q, want %q (header injection must not corrupt the body either)", downloadBody, "hello")
	}
	if n := len(downloadResp.Header.Values("X-Injected")); n != 0 {
		t.Errorf("X-Injected header present (%d times) — filename injected a real response header", n)
	}
	if n := len(downloadResp.Header.Values("Set-Cookie")); n != 0 {
		t.Errorf("Set-Cookie header present (%d times) — filename injected a real response header", n)
	}
	if n := len(downloadResp.Header.Values("Content-Disposition")); n != 1 {
		t.Fatalf("Content-Disposition header present %d times, want exactly 1", n)
	}
	cd := downloadResp.Header.Get("Content-Disposition")
	if strings.ContainsAny(cd, "\r\n") {
		t.Errorf("Content-Disposition header contains a raw CR or LF byte: %q", cd)
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		t.Fatalf("Content-Disposition %q does not parse as a valid media type: %v", cd, err)
	}
	// %q's Go-syntax escaping and HTTP quoted-string escaping are
	// different formats — a raw CR byte becomes the two literal
	// characters `\` and `r` under %q, which a quoted-string parser
	// then unescapes as its own quoted-pair (backslash-then-literal-
	// char) down to just `r`. The exact bytes don't round-trip, and
	// that's the point: what matters is that no real control character
	// survives into the parsed value, only inert text.
	if strings.ContainsAny(params["filename"], "\r\n") {
		t.Errorf("parsed filename %q contains a raw CR or LF byte — control characters survived instead of being neutralized", params["filename"])
	}
	if !strings.Contains(params["filename"], "X-Injected") {
		t.Errorf("parsed filename %q lost the non-control-character payload entirely — expected it neutralized, not silently dropped", params["filename"])
	}
}

// TestUploadedHTMLNeverRendersInline is the discriminating check for a
// hypothesis raised during Phase 6 Step 7's review: media_type is
// client-supplied (createUploadAttachment reads it straight from
// r.FormValue("media_type") with no allow-list) and gets echoed
// verbatim into the download response's Content-Type — could an
// uploader make the server serve attacker HTML that a browser renders
// inline (stored XSS on the app's own origin)? Empirically no, and
// this pins down why: (1) an empty filename can never reach
// CreateAttachment at all — mime/multipart's own Form.File only
// classifies a part as a file when its filename param is non-empty,
// so Content-Disposition (below) is never skipped for a real upload;
// (2) writeAttachmentDownload's Content-Disposition is hardcoded to
// the "attachment" disposition type, never "inline", which is what
// actually stops a browser from rendering the response as a page
// regardless of Content-Type; (3) securityHeaders (server.go) wraps
// the whole mux, not just the SPA, so X-Content-Type-Options: nosniff
// and a strict CSP apply here too, as defense in depth.
func TestUploadedHTMLNeverRendersInline(t *testing.T) {
	ts := newTestServer(t)
	ref := createTestTicket(t, ts)

	createResp, createBody := ts.doMultipart(http.MethodPost, "/tickets/"+ref+"/attachments", nil,
		map[string]string{"title": "not actually renderable", "media_type": "text/html"},
		"file", "page.html", []byte("<script>alert(document.domain)</script>"))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create attachment status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var attachment map[string]any
	if err := json.Unmarshal(createBody, &attachment); err != nil {
		t.Fatalf("unmarshal attachment: %v", err)
	}
	id := int64(attachment["id"].(float64))

	downloadResp, downloadBody := ts.doUnvalidated(http.MethodGet, "/attachments/"+strconv.FormatInt(id, 10)+"/download", nil, nil)
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, body=%s", downloadResp.StatusCode, downloadBody)
	}
	// Content-Type does reflect the uploader's declared media_type —
	// that part of the hypothesis was right, and is expected: this
	// route serves arbitrary user content and can't know its true type.
	if ct := downloadResp.Header.Get("Content-Type"); ct != "text/html" {
		t.Errorf("Content-Type = %q, want the declared media_type text/html to be echoed", ct)
	}
	// What actually prevents inline rendering: the disposition type.
	cd := downloadResp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want it to start with %q so browsers download rather than render this response", cd, "attachment;")
	}
	if got := downloadResp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff on the attachment download route", got)
	}
	if csp := downloadResp.Header.Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy header missing on the attachment download route")
	}
}
