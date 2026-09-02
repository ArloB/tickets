package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// testServer wraps a real HTTP server (internal/service backed by a
// fresh internal/store) plus an OpenAPI router, so every call can be
// validated against api/openapi.yaml directly - Phase 0 verification
// gate 5's literal requirement, not just hand-checked assertions.
//
// It authenticates as a freshly created admin account and attaches the
// resulting session cookie/CSRF token to every do() call by default:
// most tests built on this helper predate Phase 2's auth work and
// exercise ordinary project/ticket CRUD as an editor, not auth itself
// (auth_test.go covers auth directly, with its own server instances
// for scenarios — genuinely anonymous requests, missing CSRF, expired
// sessions — that need to control credentials precisely rather than
// have them transparently attached).
type testServer struct {
	t         *testing.T
	url       string
	router    routers.Router
	sessionID string
	csrfToken string
	store     *store.Store
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)
	SetDataDir(dataDir)

	ts := httptest.NewServer(NewHandler(svc, false))
	t.Cleanup(ts.Close)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml fails its own validation: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build openapi router: %v", err)
	}

	server := &testServer{t: t, url: ts.URL, router: router, store: st}
	server.loginAsTestAdmin(svc)
	return server
}

// loginAsTestAdmin creates the one admin account this test server
// needs (directly through internal/service, not HTTP — first-run
// setup itself is cmd/tickets/setup.go's concern, not this helper's)
// and authenticates via the real login endpoint, so do()'s callers get
// a genuine session cookie and CSRF token exactly as a browser would.
func (ts *testServer) loginAsTestAdmin(svc *service.Service) {
	t := ts.t
	t.Helper()

	if _, err := svc.CreateAdminAccount(context.Background(), "test-admin", "test-admin-password"); err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}

	body, err := json.Marshal(map[string]string{"username": "test-admin", "password": "test-admin-password"})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.url+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("login as test-admin: status %d, body=%s", resp.StatusCode, respBody)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			ts.sessionID = c.Value
		}
	}
	if ts.sessionID == "" {
		t.Fatalf("login response set no %s cookie", sessionCookieName)
	}
	var loginResp loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	ts.csrfToken = loginResp.CSRFToken
}

// do sends a request under /api/v1 and validates both the request and
// the response against the loaded OpenAPI spec, failing the test on
// any schema mismatch, in addition to returning the raw response for
// further assertions.
func (ts *testServer) do(method, path string, headers map[string]string, body []byte) (*http.Response, []byte) {
	t := ts.t
	t.Helper()

	req, err := http.NewRequest(method, ts.url+"/api/v1"+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	// Attached first so a test can still override either via headers
	// (e.g. to exercise a missing/wrong CSRF token deliberately) — the
	// loop below runs after and wins on any key collision.
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
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	_ = resp.Body.Close()

	ts.validate(method, path, headers, body, resp, respBody)
	return resp, respBody
}

func (ts *testServer) validate(method, path string, headers map[string]string, body []byte, resp *http.Response, respBody []byte) {
	t := ts.t
	t.Helper()

	vReq := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(body))
	if len(body) > 0 {
		vReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		vReq.Header.Set(k, v)
	}

	route, pathParams, err := ts.router.FindRoute(vReq)
	if err != nil {
		t.Fatalf("openapi: no route matches %s %s: %v", method, path, err)
	}

	reqInput := &openapi3filter.RequestValidationInput{
		Request:     vReq,
		PathParams:  pathParams,
		QueryParams: vReq.URL.Query(),
		Route:       route,
		// This validates request/response *shapes* against the schema,
		// not whether the caller is actually authorized — that's
		// requireEditor/requireAdmin's job, exercised directly by
		// auth_test.go. Without a no-op AuthenticationFunc,
		// openapi3filter refuses to validate anything against an
		// operation that declares a security requirement at all.
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	if err := openapi3filter.ValidateRequest(context.Background(), reqInput); err != nil {
		t.Errorf("openapi: request %s %s fails schema validation: %v", method, path, err)
	}

	respInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: reqInput,
		Status:                 resp.StatusCode,
		Header:                 resp.Header,
	}
	respInput.SetBodyBytes(respBody)
	if err := openapi3filter.ValidateResponse(context.Background(), respInput); err != nil {
		t.Errorf("openapi: response %s %s (status %d) fails schema validation: %v\nbody: %s", method, path, resp.StatusCode, err, respBody)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// rawGet bypasses OpenAPI validation for /healthz and /readyz, which
// are intentionally outside the versioned /api/v1 surface (and so
// outside api/openapi.yaml's servers: url: /api/v1 scope).
func (ts *testServer) rawGet(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.url + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// doUnvalidated is do's counterpart for ?fields=-narrowed responses: a
// fields= request's success body is a subset object, not the full
// Ticket/Feature/... shape the operation's 200 schema documents —
// OpenAPI 3.0 has no way to express a response shape conditioned on a
// query parameter's value (documented on the fields param itself in
// api/openapi.yaml), so unlike do, this deliberately skips contract
// validation rather than fail it. Still hits the real /api/v1 route,
// so the handler and route wiring stay fully exercised.
func (ts *testServer) doUnvalidated(method, path string, headers map[string]string, body []byte) (*http.Response, []byte) {
	t := ts.t
	t.Helper()

	req, err := http.NewRequest(method, ts.url+"/api/v1"+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
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
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	_ = resp.Body.Close()
	return resp, respBody
}

func TestHealthAndReady(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.rawGet(t, "/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", resp.StatusCode)
	}
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	_ = resp.Body.Close()
	if health.Version == "" {
		t.Error("/healthz version = \"\", want buildinfo.Version (Phase 6 Step 3)")
	}

	resp = ts.rawGet(t, "/readyz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz status = %d, want 200 (database should be reachable)", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestFullVerticalSlice(t *testing.T) {
	ts := newTestServer(t)

	// --- create ---
	createResp, createBody := ts.do(http.MethodPost, "/projects", nil,
		mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var proj map[string]any
	if err := json.Unmarshal(createBody, &proj); err != nil {
		t.Fatalf("unmarshal project: %v", err)
	}
	if proj["key"] != "ABC" {
		t.Errorf("project key = %v, want ABC", proj["key"])
	}

	// --- get ---
	getResp, getBody := ts.do(http.MethodGet, "/projects/ABC", nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get project status = %d, body=%s", getResp.StatusCode, getBody)
	}

	// --- list ---
	listResp, listBody := ts.do(http.MethodGet, "/projects", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list projects status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page map[string]any
	if err := json.Unmarshal(listBody, &page); err != nil {
		t.Fatalf("unmarshal page: %v", err)
	}
	if projects, _ := page["projects"].([]any); len(projects) != 1 {
		t.Errorf("list projects returned %d projects, want 1", len(projects))
	}

	// --- create ticket (explicit general: true lands in the General feature) ---
	ticketResp, ticketBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "bug", "title": "Fix the parser", "general": true}))
	if ticketResp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket status = %d, body=%s", ticketResp.StatusCode, ticketBody)
	}
	var ticket map[string]any
	if err := json.Unmarshal(ticketBody, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	if ticket["ref"] != "ABC-1" {
		t.Errorf("ticket ref = %v, want ABC-1", ticket["ref"])
	}
	if ticket["feature"] != "ABC-F1" {
		t.Errorf("ticket feature = %v, want ABC-F1 (General)", ticket["feature"])
	}
	version := int64(ticket["version"].(float64))

	// --- get ticket ---
	getTicketResp, getTicketBody := ts.do(http.MethodGet, "/tickets/ABC-1", nil, nil)
	if getTicketResp.StatusCode != http.StatusOK {
		t.Fatalf("get ticket status = %d, body=%s", getTicketResp.StatusCode, getTicketBody)
	}

	// --- update status with correct If-Match ---
	updResp, updBody := ts.do(http.MethodPatch, "/tickets/ABC-1",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`},
		mustJSON(t, map[string]string{"status": "in_progress"}))
	if updResp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updResp.StatusCode, updBody)
	}

	// --- stale If-Match must 409 with current_version ---
	staleResp, staleBody := ts.do(http.MethodPatch, "/tickets/ABC-1",
		map[string]string{"If-Match": `"` + strconv.FormatInt(version, 10) + `"`}, // the now-superseded version
		mustJSON(t, map[string]string{"status": "done"}))
	if staleResp.StatusCode != http.StatusConflict {
		t.Fatalf("stale update status = %d, want 409, body=%s", staleResp.StatusCode, staleBody)
	}
	var envelope map[string]any
	if err := json.Unmarshal(staleBody, &envelope); err != nil {
		t.Fatalf("unmarshal error envelope: %v", err)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "version_conflict" {
		t.Errorf("error.code = %v, want version_conflict", errObj["code"])
	}
	if _, ok := errObj["current_version"]; !ok {
		t.Errorf("version_conflict response missing current_version: %s", staleBody)
	}
}

// TestIfMatchRejectsUnquotedInteger exercises the concurrency.md
// requirement that an unquoted bare integer is rejected, not silently
// accepted as if it were the quoted form.
func TestIfMatchRejectsUnquotedInteger(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))

	resp, body := ts.do(http.MethodPatch, "/tickets/ABC-1",
		map[string]string{"If-Match": "1"}, // bare, unquoted - must be rejected
		mustJSON(t, map[string]string{"status": "in_progress"}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unquoted If-Match status = %d, want 400, body=%s", resp.StatusCode, body)
	}
}

func TestIdempotentCreateReplayOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))

	body := mustJSON(t, map[string]any{"type": "bug", "title": "Fix the parser", "general": true})
	headers := map[string]string{"Idempotency-Key": "retry-key-1"}

	first, firstBody := ts.do(http.MethodPost, "/projects/ABC/tickets", headers, body)
	second, secondBody := ts.do(http.MethodPost, "/projects/ABC/tickets", headers, body)
	if first.StatusCode != http.StatusCreated || second.StatusCode != http.StatusCreated {
		t.Fatalf("replay statuses = %d, %d, want both 201", first.StatusCode, second.StatusCode)
	}

	var t1, t2 map[string]any
	_ = json.Unmarshal(firstBody, &t1)
	_ = json.Unmarshal(secondBody, &t2)
	if t1["ref"] != t2["ref"] {
		t.Errorf("idempotent replay created two tickets: %v vs %v", t1["ref"], t2["ref"])
	}

	// A genuinely new request without the replayed key must still get
	// a new ticket, not be swallowed by the idempotency cache.
	third, thirdBody := ts.do(http.MethodPost, "/projects/ABC/tickets", nil,
		mustJSON(t, map[string]any{"type": "task", "title": "Different", "general": true}))
	if third.StatusCode != http.StatusCreated {
		t.Fatalf("third create status = %d", third.StatusCode)
	}
	var t3 map[string]any
	_ = json.Unmarshal(thirdBody, &t3)
	if t3["ref"] == t1["ref"] {
		t.Errorf("non-replayed create got the same ref as the idempotent one: %v", t3["ref"])
	}
}

func TestGetProjectNotFoundEnvelope(t *testing.T) {
	ts := newTestServer(t)
	resp, body := ts.do(http.MethodGet, "/projects/NOPE", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", resp.StatusCode, body)
	}
	var envelope map[string]any
	_ = json.Unmarshal(body, &envelope)
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "not_found" {
		t.Errorf("error.code = %v, want not_found", errObj["code"])
	}
	if errObj["correlation_id"] == "" || errObj["correlation_id"] == nil {
		t.Errorf("error envelope missing correlation_id: %s", body)
	}
}
