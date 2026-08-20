package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ArloB/tickets/internal/auth"
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
	svc := service.New(st)

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
	ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", authed, mustJSON(t, map[string]string{"type": "task", "title": "T"}))
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
	if anon.Permission != "viewer" || anon.Actor != "" {
		t.Errorf("anonymous /auth/me = %+v, want permission=viewer actor=\"\"", anon)
	}

	sessionID, _ := ts.login("alice", "correct-password")
	resp, body = ts.doNoAuth(http.MethodGet, "/auth/me", map[string]string{"Cookie": sessionCookieName + "=" + sessionID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated GET /auth/me: status = %d, body=%s", resp.StatusCode, body)
	}
	var authed meResponseForTest
	_ = json.Unmarshal(body, &authed)
	if authed.Permission != "editor" || authed.Actor != "human:alice" || !authed.IsAdmin {
		t.Errorf("authenticated /auth/me = %+v, want permission=editor actor=human:alice is_admin=true", authed)
	}
}

type meResponseForTest struct {
	Actor      string `json:"actor"`
	Permission string `json:"permission"`
	IsAdmin    bool   `json:"is_admin"`
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
