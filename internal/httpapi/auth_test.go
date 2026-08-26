package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// newAuthTestServer is newTestServer without the automatic admin
// login: every test in this file needs to control credentials (or
// their absence) precisely — anonymous, a specific session, a bearer
// token, an expired session — which an auto-attached admin session
// would get in the way of. It also returns *service.Service directly,
// since several scenarios here (bootstrapping a second, non-admin
// human account; backdating a session's expiry) need to reach
// internal/store in ways no HTTP route exposes, exactly like the
// existing test suite's mustSystemActorID-style fixtures do.
func newAuthTestServer(t *testing.T, anonymousRead bool) (*testServer, *service.Service, *store.Store) {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)

	ts := httptest.NewServer(NewHandler(svc, anonymousRead))
	t.Cleanup(ts.Close)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build openapi router: %v", err)
	}

	return &testServer{t: t, url: ts.URL, router: router}, svc, st
}

func mustCreateAdmin(t *testing.T, svc *service.Service, username, password string) domain.ActorRef {
	t.Helper()
	ref, err := svc.CreateAdminAccount(context.Background(), username, password)
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}
	return ref
}

// mustCreateNonAdminHuman bootstraps a second human account directly
// through internal/store: Phase 2's HTTP surface has no "invite a
// teammate" endpoint yet (product spec §4.1 allows more than one human
// account, but self-serve creation of a non-first, non-admin one isn't
// wired up anywhere — a real gap, not exercised by this test's own
// scope, only worked around to set one up as a fixture).
func mustCreateNonAdminHuman(t *testing.T, st *store.Store, username, password string) {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	actorID, err := store.CreateActor(ctx, st.DB(), domain.ActorHuman, username, "", nil, store.Now())
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	if err := store.CreateHumanAccount(ctx, st.DB(), actorID, username, hash, false, store.Now()); err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}
}

// login drives the real login endpoint and returns the session cookie
// value and CSRF token, or fails the test if login didn't succeed.
func (ts *testServer) login(username, password string) (sessionID, csrfToken string) {
	t := ts.t
	t.Helper()

	resp, body := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
		"username": username, "password": password,
	}))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login(%s): status %d, body=%s", username, resp.StatusCode, body)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			sessionID = c.Value
		}
	}
	var lr loginResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	return sessionID, lr.CSRFToken
}

// doNoAuth is do() without testServer's usual auto-attached session
// cookie/CSRF token (there is none here — newAuthTestServer's servers
// never log in automatically), so headers passed in are the only
// credentials a request carries.
func (ts *testServer) doNoAuth(method, path string, headers map[string]string, body []byte) (*http.Response, []byte) {
	return ts.do(method, path, headers, body)
}

func TestLoginSuccess(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")

	sessionID, csrfToken := ts.login("alice", "correct-password")
	if sessionID == "" || csrfToken == "" {
		t.Fatalf("login returned empty session/csrf: %q %q", sessionID, csrfToken)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")

	resp, _ := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
		"username": "alice", "password": "wrong-password",
	}))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with wrong password: status = %d, want 401", resp.StatusCode)
	}
}

func TestAnonymousReadAllowedWhenEnabled(t *testing.T) {
	ts, _, _ := newAuthTestServer(t, true)

	resp, body := ts.doNoAuth(http.MethodGet, "/projects", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("anonymous GET /projects with anonymous read enabled: status = %d, body=%s", resp.StatusCode, body)
	}
}

func TestAnonymousReadRejectedWhenDisabled(t *testing.T) {
	ts, _, _ := newAuthTestServer(t, false)

	resp, _ := ts.doNoAuth(http.MethodGet, "/projects", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous GET /projects with anonymous read disabled: status = %d, want 401", resp.StatusCode)
	}
}

func TestAnonymousWriteRejectedEvenWhenReadEnabled(t *testing.T) {
	ts, _, _ := newAuthTestServer(t, true)

	resp, _ := ts.doNoAuth(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("anonymous POST /projects with anonymous read enabled: status = %d, want 403 (viewer can read, not write)", resp.StatusCode)
	}
}

// TestAnonymousReadCoversStep10Through14Routes closes a gap this auth
// suite had: it predates Steps 10-14's roughly dozen new routeViewer
// GET routes (features, comments — including tombstoned ones,
// relationships, associations, and now the ticket list endpoint), so
// nothing pinned that anonymous read (product spec §4.2, true by
// default on a loopback install) actually covers them too, not just
// the Phase 0/1 routes this file originally tested against. Also
// confirms a write among the same new surface (adding a comment) is
// still rejected for an anonymous viewer, the same as every other
// mutating route.
func TestAnonymousReadCoversStep10Through14Routes(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, true)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, csrfToken := ts.login("alice", "correct-password")

	authed := map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken}
	ts.doNoAuth(http.MethodPost, "/projects", authed, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", authed, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	ts.doNoAuth(http.MethodPost, "/tickets/ABC-1/comments", authed, mustJSON(t, map[string]string{"body": "hello"}))

	// Anonymous GETs across the new route surface all succeed.
	for _, path := range []string{"/features/ABC-F1", "/projects/ABC/tickets", "/tickets/ABC-1/comments", "/tickets/ABC-1/relationships", "/tickets/ABC-1/associations"} {
		resp, body := ts.doNoAuth(http.MethodGet, path, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("anonymous GET %s with anonymous read enabled: status = %d, body=%s", path, resp.StatusCode, body)
		}
	}

	// An anonymous write among that same new surface is still rejected.
	resp, _ := ts.doNoAuth(http.MethodPost, "/tickets/ABC-1/comments", nil, mustJSON(t, map[string]string{"body": "should be rejected"}))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("anonymous POST /tickets/ABC-1/comments with anonymous read enabled: status = %d, want 403", resp.StatusCode)
	}
}

// TestAnonymousReadCoversPhase4And5Routes is
// TestAnonymousReadCoversStep10Through14Routes's counterpart for
// everything routeTable gained afterward: decisions, plans/documents
// (including their version history and, per ADR 0004's Consequences —
// "Phase 5 widened anonymous read to attachment bytes" — their
// downloadable content), external links, backlinks, activity, search,
// and an attachment's own bytes on a ticket. Also locks in the
// deliberate exception on the same surface: notifications and a
// subscription's own status are routeEditor even for GET (no anonymous
// identity to hold either), so anonymous access to those two must stay
// rejected even with anonymous read enabled.
func TestAnonymousReadCoversPhase4And5Routes(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, true)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, csrfToken := ts.login("alice", "correct-password")
	authed := map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken}

	mustCreated := func(resp *http.Response, body []byte, label string) map[string]any {
		t.Helper()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s: status = %d, body=%s", label, resp.StatusCode, body)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("%s: unmarshal response: %v", label, err)
		}
		return m
	}

	projResp, projBody := ts.doNoAuth(http.MethodPost, "/projects", authed, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	mustCreated(projResp, projBody, "create project")

	ticketResp, ticketBody := ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", authed, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	ticket := mustCreated(ticketResp, ticketBody, "create ticket")
	ticketRef, _ := ticket["ref"].(string)

	decisionResp, decisionBody := ts.doNoAuth(http.MethodPost, "/projects/ABC/decisions", authed, mustJSON(t, map[string]string{
		"title": "A decision", "decision": "do the thing",
	}))
	decision := mustCreated(decisionResp, decisionBody, "create decision")
	decisionRef, _ := decision["ref"].(string)

	// A file representation, not markdown — DownloadContentItem (ADR
	// 0004's flagged risk, extended to plans/documents) only serves
	// bytes for the file representation.
	planResp, planBody := ts.doMultipart(http.MethodPost, "/projects/ABC/plans", authed,
		map[string]string{"title": "A plan"}, "file", "plan.txt", []byte("plan file bytes"))
	plan := mustCreated(planResp, planBody, "create plan")
	planRef, _ := plan["ref"].(string)

	linkResp, linkBody := ts.doNoAuth(http.MethodPost, "/tickets/"+ticketRef+"/links", authed, mustJSON(t, map[string]string{
		"title": "External doc", "url": "https://example.com/doc",
	}))
	link := mustCreated(linkResp, linkBody, "create link")
	linkID, _ := link["id"].(float64)

	attachResp, attachBody := ts.doMultipart(http.MethodPost, "/tickets/"+ticketRef+"/attachments", authed,
		map[string]string{"title": "A file"}, "file", "notes.txt", []byte("attachment bytes"))
	attachment := mustCreated(attachResp, attachBody, "create attachment")
	attachmentID, _ := attachment["id"].(float64)

	// A comment on the project itself (Phase 6 Step 2's non-ticket
	// comment surface) so /projects/{key}/comments has something to
	// read back.
	ts.doNoAuth(http.MethodPost, "/projects/ABC/comments", authed, mustJSON(t, map[string]string{"body": "a project comment"}))

	anonymousGET := func(path string) {
		t.Helper()
		resp, body := ts.doNoAuth(http.MethodGet, path, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("anonymous GET %s with anonymous read enabled: status = %d, body=%s", path, resp.StatusCode, body)
		}
	}

	for _, path := range []string{
		"/decisions/" + decisionRef,
		"/decisions/" + decisionRef + "/versions",
		"/decisions/" + decisionRef + "/diff?from=1&to=1",
		"/projects/ABC/plans",
		"/plans/" + planRef,
		"/plans/" + planRef + "/versions",
		"/plans/" + planRef + "/download",
		"/tickets/" + ticketRef + "/links",
		"/tickets/" + ticketRef + "/backlinks",
		"/tickets/" + ticketRef + "/attachments",
		fmt.Sprintf("/attachments/%.0f", attachmentID),
		fmt.Sprintf("/attachments/%.0f/download", attachmentID), // ADR 0004: the specific risk Phase 5 introduced
		fmt.Sprintf("/attachments/%.0f/versions", attachmentID),
		"/projects/ABC/comments",
		"/projects/ABC/activity",
		"/search?q=ticket",
	} {
		anonymousGET(path)
	}

	// The link created above round-trips through the anonymous list
	// read — a weak check that the list route isn't just returning an
	// empty/error body that happens to be 200.
	resp, body := ts.doNoAuth(http.MethodGet, "/tickets/"+ticketRef+"/links", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "External doc") {
		t.Errorf("anonymous GET links = %d %s, want 200 containing the created link (id %.0f)", resp.StatusCode, body, linkID)
	}

	// Deliberate exception: notifications and a subscription's own
	// status require an actual identity, so they stay routeEditor even
	// for GET — anonymous access to either must still be rejected with
	// anonymous read enabled.
	for _, path := range []string{"/notifications", "/tickets/" + ticketRef + "/subscribe"} {
		resp, _ := ts.doNoAuth(http.MethodGet, path, nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("anonymous GET %s with anonymous read enabled: status = %d, want 403 (requires an actual identity)", path, resp.StatusCode)
		}
	}
}

func TestMutatingRequestWithoutCSRFTokenRejected(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, _ := ts.login("alice", "correct-password")

	resp, _ := ts.doNoAuth(http.MethodPost, "/projects",
		map[string]string{"Cookie": sessionCookieName + "=" + sessionID},
		mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("session-authenticated POST without X-CSRF-Token: status = %d, want 403", resp.StatusCode)
	}
}

func TestMutatingRequestWithWrongCSRFTokenRejected(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, _ := ts.login("alice", "correct-password")

	resp, _ := ts.doNoAuth(http.MethodPost, "/projects",
		map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": "not-the-right-token"},
		mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("session-authenticated POST with wrong X-CSRF-Token: status = %d, want 403", resp.StatusCode)
	}
}

func TestMutatingRequestWithSessionAndCSRFSucceeds(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, csrfToken := ts.login("alice", "correct-password")

	resp, body := ts.doNoAuth(http.MethodPost, "/projects",
		map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken},
		mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("session+CSRF POST /projects: status = %d, body=%s", resp.StatusCode, body)
	}
	var proj map[string]any
	_ = json.Unmarshal(body, &proj)
	if proj["key"] != "ABC" {
		t.Errorf("created project key = %v, want ABC", proj["key"])
	}
}

func TestBearerTokenAuthenticatesWriteWithoutCSRF(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	admin := mustCreateAdmin(t, svc, "alice", "correct-password")
	agent, err := svc.CreateAgent(context.Background(), service.CreateAgentRequest{Name: "codex"}, admin, "test")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	raw, _, err := svc.CreateAgentToken(context.Background(), agent.Ref, "", nil, admin, "test")
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	resp, body := ts.doNoAuth(http.MethodPost, "/projects",
		map[string]string{"Authorization": "Bearer " + raw},
		mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("bearer-token POST /projects (no CSRF needed): status = %d, body=%s", resp.StatusCode, body)
	}
}

func TestRevokedBearerTokenRejected(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	admin := mustCreateAdmin(t, svc, "alice", "correct-password")
	agent, err := svc.CreateAgent(context.Background(), service.CreateAgentRequest{Name: "codex"}, admin, "test")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	raw, tokenID, err := svc.CreateAgentToken(context.Background(), agent.Ref, "", nil, admin, "test")
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}
	if err := svc.RevokeAgentToken(context.Background(), tokenID, admin, "test"); err != nil {
		t.Fatalf("RevokeAgentToken: %v", err)
	}

	resp, _ := ts.doNoAuth(http.MethodGet, "/projects", map[string]string{"Authorization": "Bearer " + raw}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET with a revoked bearer token: status = %d, want 401", resp.StatusCode)
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	ts, svc, st := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, _ := ts.login("alice", "correct-password")

	// Manipulate expires_at directly rather than sleeping — proves the
	// expiry check works without depending on real clock time.
	if _, err := st.DB().Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, "2020-01-01T00:00:00.000000000Z", sessionID); err != nil {
		t.Fatalf("backdate session: %v", err)
	}

	resp, _ := ts.doNoAuth(http.MethodGet, "/projects", map[string]string{"Cookie": sessionCookieName + "=" + sessionID}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET with an expired session: status = %d, want 401", resp.StatusCode)
	}
}

func TestLoginThrottledAfterMaxFailures(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")

	for i := 0; i < 10; i++ { // matches service.loginThrottleMax
		resp, _ := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
			"username": "alice", "password": "wrong-password",
		}))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (not yet throttled)", i, resp.StatusCode)
		}
	}

	resp, _ := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
		"username": "alice", "password": "correct-password",
	}))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("login after 10 failures (even with the correct password): status = %d, want 429", resp.StatusCode)
	}
}

func TestMeAnonymousAndAuthenticated(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, true)
	mustCreateAdmin(t, svc, "alice", "correct-password")

	resp, body := ts.doNoAuth(http.MethodGet, "/auth/me", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous GET /auth/me: status = %d, body=%s", resp.StatusCode, body)
	}
	var anon meResponseForTest
	_ = json.Unmarshal(body, &anon)
	if anon.Permission != "viewer" || anon.Actor != "" || anon.CSRFToken != "" {
		t.Errorf("anonymous /auth/me = %+v, want permission=viewer actor=\"\" csrf_token=\"\"", anon)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("/auth/me Cache-Control = %q, want no-store", cc)
	}

	sessionID, csrfToken := ts.login("alice", "correct-password")
	resp, body = ts.doNoAuth(http.MethodGet, "/auth/me", map[string]string{"Cookie": sessionCookieName + "=" + sessionID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated GET /auth/me: status = %d, body=%s", resp.StatusCode, body)
	}
	var authed meResponseForTest
	_ = json.Unmarshal(body, &authed)
	if authed.Permission != "editor" || authed.Actor != "human:alice" || !authed.IsAdmin {
		t.Errorf("authenticated /auth/me = %+v, want permission=editor actor=human:alice is_admin=true", authed)
	}
	if authed.CSRFToken != csrfToken {
		t.Errorf("authenticated /auth/me csrf_token = %q, want %q (the token login returned, so a page reload can recover it)", authed.CSRFToken, csrfToken)
	}
}

// TestMeCSRFTokenAbsentForBearer confirms /auth/me never echoes a CSRF
// token for a bearer-authenticated caller — CSRF only protects against
// a browser silently attaching cookies, and a bearer token in an
// Authorization header was never at risk of that, so there is nothing
// for the token to protect there (mirrors requireEditor's own
// bearer exemption, auth_middleware.go).
func TestMeCSRFTokenAbsentForBearer(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, csrfToken := ts.login("alice", "correct-password")
	authHeaders := map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken}

	createResp, createBody := ts.doNoAuth(http.MethodPost, "/agents", authHeaders,
		mustJSON(t, map[string]string{"name": "codex"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: status = %d, body=%s", createResp.StatusCode, createBody)
	}
	tokenResp, tokenBody := ts.doNoAuth(http.MethodPost, "/agents/codex/tokens", authHeaders, mustJSON(t, map[string]string{"description": "ci"}))
	if tokenResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent token: status = %d, body=%s", tokenResp.StatusCode, tokenBody)
	}
	var created struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(tokenBody, &created)

	resp, body := ts.doNoAuth(http.MethodGet, "/auth/me", map[string]string{"Authorization": "Bearer " + created.Token}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer GET /auth/me: status = %d, body=%s", resp.StatusCode, body)
	}
	var bearer meResponseForTest
	_ = json.Unmarshal(body, &bearer)
	if bearer.CSRFToken != "" {
		t.Errorf("bearer /auth/me csrf_token = %q, want empty", bearer.CSRFToken)
	}
}

type meResponseForTest struct {
	Actor      string `json:"actor"`
	Permission string `json:"permission"`
	IsAdmin    bool   `json:"is_admin"`
	CSRFToken  string `json:"csrf_token"`
}

// TestSetupCreatesFirstAdminThenRefusesSecond exercises POST
// /api/v1/setup end to end: unauthenticated first-run creation
// succeeds, a subsequent call (simulating a second browser tab, or a
// retry) fails with already_exists rather than creating a second
// admin, and the newly created account can immediately log in.
func TestSetupCreatesFirstAdminThenRefusesSecond(t *testing.T) {
	ts, _, _ := newAuthTestServer(t, false)

	resp, body := ts.doNoAuth(http.MethodPost, "/setup", nil, mustJSON(t, map[string]string{
		"username": "alice", "password": "correct-password",
	}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first setup: status = %d, body=%s", resp.StatusCode, body)
	}
	var created struct {
		Actor string `json:"actor"`
	}
	_ = json.Unmarshal(body, &created)
	if created.Actor != "human:alice" {
		t.Errorf("setup response actor = %q, want human:alice", created.Actor)
	}

	sessionID, csrfToken := ts.login("alice", "correct-password")
	if sessionID == "" || csrfToken == "" {
		t.Fatalf("login with the account setup just created: got empty session/csrf")
	}

	second, secondBody := ts.doNoAuth(http.MethodPost, "/setup", nil, mustJSON(t, map[string]string{
		"username": "mallory", "password": "another-password",
	}))
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("second setup: status = %d, body=%s, want 409 already_exists", second.StatusCode, secondBody)
	}

	// The rejected second call must not have created "mallory" as a
	// side effect before failing — confirmed by a real login attempt
	// through the HTTP endpoint, not by inspecting the store directly,
	// since an unauthenticated caller has no other way to observe this.
	malloryLogin, _ := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
		"username": "mallory", "password": "another-password",
	}))
	if malloryLogin.StatusCode != http.StatusUnauthorized {
		t.Errorf("login as mallory after rejected second setup: status = %d, want 401 (mallory should not exist)", malloryLogin.StatusCode)
	}
}

// TestSetupConcurrentRequestsCreateOnlyOneAdmin exercises the race
// service.CreateAdminAccount's doc comment calls out: two concurrent
// POST /api/v1/setup calls (unlike a single local `tickets setup`
// invocation) really can race, and the transaction-scoped recheck must
// still let only one succeed.
func TestSetupConcurrentRequestsCreateOnlyOneAdmin(t *testing.T) {
	ts, _, _ := newAuthTestServer(t, false)

	const n = 8
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := ts.doNoAuth(http.MethodPost, "/setup", nil, mustJSON(t, map[string]string{
				"username": fmt.Sprintf("racer-%d", i), "password": "correct-password",
			}))
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	created, conflicted := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
		default:
			t.Errorf("concurrent setup call returned status %d, want 201 or 409", s)
		}
	}
	if created != 1 {
		t.Errorf("concurrent setup calls: %d succeeded, want exactly 1 (got %d 409s)", created, conflicted)
	}
}

func TestAdminEndpointsFullLifecycle(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, csrfToken := ts.login("alice", "correct-password")
	authHeaders := map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken}

	createResp, createBody := ts.doNoAuth(http.MethodPost, "/agents", authHeaders,
		mustJSON(t, map[string]string{"name": "codex", "description": "CI agent"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: status = %d, body=%s", createResp.StatusCode, createBody)
	}

	listResp, listBody := ts.doNoAuth(http.MethodGet, "/agents", authHeaders, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list agents: status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var agentsPage struct {
		Agents []struct{ Name string } `json:"agents"`
	}
	_ = json.Unmarshal(listBody, &agentsPage)
	if len(agentsPage.Agents) != 1 || agentsPage.Agents[0].Name != "codex" {
		t.Fatalf("list agents = %s, want one agent named codex", listBody)
	}

	tokenResp, tokenBody := ts.doNoAuth(http.MethodPost, "/agents/codex/tokens", authHeaders, mustJSON(t, map[string]string{"description": "ci"}))
	if tokenResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent token: status = %d, body=%s", tokenResp.StatusCode, tokenBody)
	}
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(tokenBody, &created)
	if created.Token == "" || created.ID == 0 {
		t.Fatalf("create agent token response = %s, want non-empty id/token", tokenBody)
	}

	// The raw token round-trips against a subsequent authenticated call.
	pingResp, _ := ts.doNoAuth(http.MethodGet, "/projects", map[string]string{"Authorization": "Bearer " + created.Token}, nil)
	if pingResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /projects with the freshly issued token: status = %d", pingResp.StatusCode)
	}

	revokeResp, revokeBody := ts.doNoAuth(http.MethodDelete, "/agents/codex/tokens/"+strconv.FormatInt(created.ID, 10), authHeaders, nil)
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke token: status = %d, body=%s", revokeResp.StatusCode, revokeBody)
	}

	afterRevoke, _ := ts.doNoAuth(http.MethodGet, "/projects", map[string]string{"Authorization": "Bearer " + created.Token}, nil)
	if afterRevoke.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /projects with the revoked token: status = %d, want 401", afterRevoke.StatusCode)
	}
}

func TestAdminEndpointsRejectNonAdmin(t *testing.T) {
	ts, svc, st := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	mustCreateNonAdminHuman(t, st, "bob", "bobs-password")
	sessionID, csrfToken := ts.login("bob", "bobs-password")

	resp, _ := ts.doNoAuth(http.MethodPost, "/agents",
		map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken},
		mustJSON(t, map[string]string{"name": "codex"}))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin POST /agents: status = %d, want 403", resp.StatusCode)
	}
}
