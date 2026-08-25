package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// loginAs logs an already-created human account in via the real login
// endpoint, returning its own session cookie and CSRF token —
// separate from ts's own (admin) session, the way two different
// browser sessions never share cookies.
func loginAs(t *testing.T, ts *testServer, username, password string) (sessionID, csrfToken string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
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
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login as %s: status %d, body=%s", username, resp.StatusCode, respBody)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			sessionID = c.Value
		}
	}
	var loginResp loginResponse
	if err := json.Unmarshal(respBody, &loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return sessionID, loginResp.CSRFToken
}

// TestCreateAccountRequiresAdmin is the assertion §2's plan called
// for explicitly: TestEveryMutatingRouteRequiresAtLeastEditor only
// proves "at least Editor," which a route mistakenly gated routeEditor
// instead of routeAdmin would still pass — this is the test that
// actually protects the admin-only account-management routes.
func TestCreateAccountRequiresAdmin(t *testing.T) {
	ts := newTestServer(t)

	// A non-admin human account, created by the admin session
	// newTestServer already logged in as.
	createResp, createBody := ts.do(http.MethodPost, "/accounts", nil,
		mustJSON(t, map[string]any{"username": "bob", "password": "bobs-password-here"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create bob's account status = %d, body=%s", createResp.StatusCode, createBody)
	}

	bobSession, bobCSRF := loginAs(t, ts, "bob", "bobs-password-here")

	// bob (Editor, not admin) is rejected from both account-management
	// routes.
	req, err := http.NewRequest(http.MethodPost, ts.url+"/api/v1/accounts", bytes.NewReader(
		mustJSON(t, map[string]any{"username": "carol", "password": "carols-password-here"})))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	req.Header.Set("X-CSRF-Token", bobCSRF)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bob POST /accounts status = %d, want 403 (non-admin Editor must be rejected)", resp.StatusCode)
	}

	listReq, err := http.NewRequest(http.MethodGet, ts.url+"/api/v1/accounts", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	listReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = listResp.Body.Close()
	if listResp.StatusCode != http.StatusForbidden {
		t.Errorf("bob GET /accounts status = %d, want 403 (non-admin Editor must be rejected)", listResp.StatusCode)
	}
}

// TestCreateAccountIsAdminFlagIsHonored confirms is_admin isn't just a
// round-tripped field — an account created with it set can actually
// reach a routeAdmin route itself, the same one
// TestCreateAccountRequiresAdmin proves a non-admin Editor cannot.
func TestCreateAccountIsAdminFlagIsHonored(t *testing.T) {
	ts := newTestServer(t)

	createResp, createBody := ts.do(http.MethodPost, "/accounts", nil,
		mustJSON(t, map[string]any{"username": "ops", "password": "ops-password-here", "is_admin": true}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create ops's account status = %d, body=%s", createResp.StatusCode, createBody)
	}

	opsSession, opsCSRF := loginAs(t, ts, "ops", "ops-password-here")

	listReq, err := http.NewRequest(http.MethodGet, ts.url+"/api/v1/accounts", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	listReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: opsSession})
	listReq.Header.Set("X-CSRF-Token", opsCSRF)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Errorf("ops (created with is_admin) GET /accounts status = %d, want 200", listResp.StatusCode)
	}
}

// TestChangePasswordSelfOrAdmin exercises the three-way permission
// split changePassword's own doc comment describes: self-service with
// the correct old password succeeds; another non-admin human is
// rejected outright; an admin can reset without the old password.
func TestChangePasswordSelfOrAdmin(t *testing.T) {
	ts := newTestServer(t)

	for _, u := range []struct{ name, password string }{
		{"bob", "bobs-original-password"},
		{"carol", "carols-original-password"},
	} {
		resp, body := ts.do(http.MethodPost, "/accounts", nil,
			mustJSON(t, map[string]any{"username": u.name, "password": u.password}))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s status = %d, body=%s", u.name, resp.StatusCode, body)
		}
	}

	bobSession, bobCSRF := loginAs(t, ts, "bob", "bobs-original-password")
	carolSession, carolCSRF := loginAs(t, ts, "carol", "carols-original-password")

	// carol cannot change bob's password.
	forbidden, err := http.NewRequest(http.MethodPost, ts.url+"/api/v1/accounts/bob/password", bytes.NewReader(
		mustJSON(t, map[string]any{"old_password": "irrelevant", "new_password": "hijacked"})))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	forbidden.Header.Set("Content-Type", "application/json")
	forbidden.AddCookie(&http.Cookie{Name: sessionCookieName, Value: carolSession})
	forbidden.Header.Set("X-CSRF-Token", carolCSRF)
	forbiddenResp, err := http.DefaultClient.Do(forbidden)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = forbiddenResp.Body.Close()
	if forbiddenResp.StatusCode != http.StatusForbidden {
		t.Errorf("carol changing bob's password: status = %d, want 403", forbiddenResp.StatusCode)
	}

	// bob can change his own, with the correct old password.
	self, err := http.NewRequest(http.MethodPost, ts.url+"/api/v1/accounts/bob/password", bytes.NewReader(
		mustJSON(t, map[string]any{"old_password": "bobs-original-password", "new_password": "bobs-new-password"})))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	self.Header.Set("Content-Type", "application/json")
	self.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	self.Header.Set("X-CSRF-Token", bobCSRF)
	selfResp, err := http.DefaultClient.Do(self)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = selfResp.Body.Close()
	if selfResp.StatusCode != http.StatusNoContent {
		t.Errorf("bob changing his own password: status = %d, want 204", selfResp.StatusCode)
	}

	// The admin session (ts's own) can reset bob's password without
	// knowing it.
	adminResetResp, adminResetBody := ts.do(http.MethodPost, "/accounts/bob/password", nil,
		mustJSON(t, map[string]any{"new_password": "admin-reset-password"}))
	if adminResetResp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin reset bob's password: status = %d, body=%s", adminResetResp.StatusCode, adminResetBody)
	}

	// bob's old session (from before the reset) no longer works — the
	// reset invalidated it.
	staleReq, err := http.NewRequest(http.MethodGet, ts.url+"/api/v1/auth/me", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	staleReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	staleResp, err := http.DefaultClient.Do(staleReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bob's session after password reset: status = %d, want 401 (invalidated)", staleResp.StatusCode)
	}
}
