// Package httpapi's security tests, scoped to what Phase 2 actually
// built (the plan's Step 17) — Markdown/upload-related security tests
// wait for the content those features add in later phases. CSRF
// enforcement, revoked-bearer-token rejection, and session-expiry
// handling are already covered by auth_test.go's
// TestMutatingRequestWithoutCSRFTokenRejected/
// TestMutatingRequestWithWrongCSRFTokenRejected/
// TestRevokedBearerTokenRejected/TestExpiredSessionRejected, so this
// file covers what wasn't yet: throttle recovery, token-in-logs, and a
// live SQL-injection regression check.
package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// TestLoginThrottleResetsAfterWindow is TestLoginThrottledAfterMaxFailures's
// counterpart: throttling isn't permanent lockout — once every failed
// attempt in the trailing window ages out, login works again with the
// correct password. Backdates login_attempts.created_at directly
// (store.TooManyAttempts's window is 15 minutes, too long to actually
// sleep through in a test) rather than waiting out the real window.
func TestLoginThrottleResetsAfterWindow(t *testing.T) {
	ts, svc, st := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")

	for i := 0; i < 10; i++ { // matches service.loginThrottleMax
		resp, _ := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
			"username": "alice", "password": "wrong-password",
		}))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (not yet throttled)", i, resp.StatusCode)
		}
	}
	throttledResp, _ := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
		"username": "alice", "password": "correct-password",
	}))
	if throttledResp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("login after 10 failures: status = %d, want 429 (throttle didn't engage)", throttledResp.StatusCode)
	}

	// Age every recorded attempt out of the trailing window.
	if _, err := st.DB().Exec(`UPDATE login_attempts SET created_at = '2020-01-01T00:00:00.000000000Z'`); err != nil {
		t.Fatalf("backdate login_attempts: %v", err)
	}

	resetResp, resetBody := ts.doNoAuth(http.MethodPost, "/auth/login", nil, mustJSON(t, map[string]string{
		"username": "alice", "password": "correct-password",
	}))
	if resetResp.StatusCode != http.StatusOK {
		t.Fatalf("login after the throttle window ages out: status = %d, body=%s, want 200 (throttle should have reset)", resetResp.StatusCode, resetBody)
	}
}

// TestBearerTokenNeverAppearsInLogOutput proves an agent's raw bearer
// token never reaches internal/httpapi's operational log output — the
// one call site that logs anything during bearer-token handling
// (warnIfInsecureBearer, auth_middleware.go) logs only the request's
// Host, never the token. Installs a buffer-backed logger via
// SetLogger, drives a request over a deliberately non-loopback-looking
// Host header specifically to trigger that warning (proving the
// assertion isn't vacuously true because nothing logged at all), and
// confirms the token substring is absent from everything captured.
func TestBearerTokenNeverAppearsInLogOutput(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, csrfToken := ts.login("alice", "correct-password")
	authHeaders := map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken}

	ts.doNoAuth(http.MethodPost, "/agents", authHeaders, mustJSON(t, map[string]string{"name": "codex"}))
	tokenResp, tokenBody := ts.doNoAuth(http.MethodPost, "/agents/codex/tokens", authHeaders, mustJSON(t, map[string]string{"description": "log test"}))
	if tokenResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent token: status = %d, body=%s", tokenResp.StatusCode, tokenBody)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(tokenBody, &created); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}
	if created.Token == "" {
		t.Fatal("created token is empty")
	}

	var logBuf bytes.Buffer
	prevLogger := logger
	SetLogger(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { SetLogger(prevLogger) })

	req, err := http.NewRequest(http.MethodGet, ts.url+"/api/v1/projects", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+created.Token)
	// A non-loopback-looking Host, deliberately: triggers
	// warnIfInsecureBearer's log line so this test proves something,
	// rather than passing vacuously because nothing logged at all.
	req.Host = "example.internal:9999"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /projects over a non-loopback Host: status = %d, want 200", resp.StatusCode)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "bearer token presented over non-loopback plain HTTP") {
		t.Fatalf("log output = %q, want it to contain the insecure-bearer warning (test didn't exercise the intended code path)", logged)
	}
	if strings.Contains(logged, created.Token) {
		t.Errorf("log output contains the raw bearer token: %q", logged)
	}
}

// TestSQLMetacharactersAreTreatedAsInertData is the parameterized-query
// regression check: every internal/store query is a static `?`-
// placeholder string with arguments passed separately to
// database/sql, never string-built from request data — this test
// proves that holds end to end over the real HTTP API, not just by
// grep. A title containing SQL metacharacters (including a
// statement-terminating `;` and a classic tautology) must round-trip
// as inert data, and the database must still be fully functional
// afterward — a naive string-concatenated query would either error
// oddly or silently corrupt state.
func TestSQLMetacharactersAreTreatedAsInertData(t *testing.T) {
	ts := newTestServer(t)

	malicious := `'); DROP TABLE projects; -- ' OR '1'='1`
	createResp, createBody := ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{
		"key": "ABC", "title": malicious,
	}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project with SQL metacharacters in title: status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var created map[string]any
	_ = json.Unmarshal(createBody, &created)
	if created["title"] != malicious {
		t.Errorf("stored title = %v, want the literal input string unmodified", created["title"])
	}

	// The database is still intact: a second, unrelated project can
	// still be created and the first one is still readable — neither
	// would hold if `projects` had actually been dropped.
	secondResp, secondBody := ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "XYZ", "title": "Unrelated"}))
	if secondResp.StatusCode != http.StatusCreated {
		t.Fatalf("create a second project after the injection attempt: status = %d, body=%s (table may have been dropped)", secondResp.StatusCode, secondBody)
	}

	getResp, getBody := ts.do(http.MethodGet, "/projects/ABC", nil, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get the project back: status = %d, body=%s", getResp.StatusCode, getBody)
	}
	var fetched map[string]any
	_ = json.Unmarshal(getBody, &fetched)
	if fetched["title"] != malicious {
		t.Errorf("fetched title = %v, want %q unchanged", fetched["title"], malicious)
	}
}
