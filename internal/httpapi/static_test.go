package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// fakeDistFS is a small in-memory stand-in for a real `npm run build`
// output, so the static-handler-behavior tests below never depend on
// whether web/dist/ actually contains one locally — see
// newStaticHandler's doc comment (static.go) for why that dependency
// existed and had to go.
func fakeDistFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<html><body>the app shell</body></html>")},
		"assets/app-abc.js":  {Data: []byte("console.log('app')")},
		"assets/app-abc.css": {Data: []byte("body{color:red}")},
	}
}

func TestLooksLikeAssetRequest(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"", false},
		{"index.html", true},
		{"assets/app-abc123.js", true},
		{"favicon.ico", true},
		{"projects/ABC", false},
		{"projects/ABC-123", false},
		{"tickets/ABC-1", false},
	}
	for _, c := range cases {
		if got := looksLikeAssetRequest(c.path); got != c.want {
			t.Errorf("looksLikeAssetRequest(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// newStaticTestServer builds a bare httptest server around
// NewHandler, with no admin account and no auto-attached credentials
// — most of these tests are precisely about what's reachable *without*
// any.
func newStaticTestServer(t *testing.T, anonymousRead bool) string {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)
	ts := httptest.NewServer(NewHandler(svc, anonymousRead))
	t.Cleanup(ts.Close)
	return ts.URL
}

func getNoAuth(t *testing.T, url, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestStaticHandlerServesIndexUnauthenticated confirms the SPA shell
// itself is reachable with zero credentials — a logged-out browser has
// to be able to load the sign-in page before it can ever present any
// credential. (The "unauthenticated" half — that "/" sits outside
// authenticate entirely — is proven separately, at the full-server
// level, by TestUnauthenticatedRoutesReachableWithoutCredentials and
// TestUnmatchedAPIRouteStaysUnderAuthenticateNotSPA below; this test
// only needs to prove the handler itself serves real content.)
func TestStaticHandlerServesIndexUnauthenticated(t *testing.T) {
	h := newStaticHandler(fakeDistFS())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("GET / Cache-Control = %q, want no-cache", cc)
	}
	if rec.Body.String() != "<html><body>the app shell</body></html>" {
		t.Errorf("GET / body = %q, want the fake index.html content", rec.Body.String())
	}
}

// TestStaticHandlerSPAFallback confirms a client-side route with no
// matching file (e.g. /projects/ABC, which only exists as a React
// Router route once the real build ships) still serves index.html —
// the fallback a hard refresh on a deep link depends on — while a
// request that looks like a missing asset does not.
func TestStaticHandlerSPAFallback(t *testing.T) {
	h := newStaticHandler(fakeDistFS())
	serve := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	indexRec := serve("/")
	routeRec := serve("/projects/ABC-123")
	if routeRec.Code != http.StatusOK {
		t.Fatalf("GET /projects/ABC-123: status = %d, want 200 (SPA fallback)", routeRec.Code)
	}
	if routeRec.Body.String() != indexRec.Body.String() {
		t.Error("GET /projects/ABC-123 did not return the same content as GET / (index.html fallback)")
	}

	if rec := serve("/assets/app-deadbeef.js"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /assets/app-deadbeef.js (missing asset): status = %d, want 404, not the SPA shell", rec.Code)
	}
	if rec := serve("/favicon.ico"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /favicon.ico (missing asset): status = %d, want 404, not the SPA shell", rec.Code)
	}

	// A real asset that does exist is served as itself, with a
	// long-lived cache header — not swallowed by the SPA fallback.
	assetRec := serve("/assets/app-abc.js")
	if assetRec.Code != http.StatusOK || assetRec.Body.String() != "console.log('app')" {
		t.Errorf("GET /assets/app-abc.js = status %d, body %q — want the real asset content", assetRec.Code, assetRec.Body.String())
	}
	if cc := assetRec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("GET /assets/app-abc.js Cache-Control = %q, want a long-lived immutable cache header", cc)
	}
}

// TestStaticHandlerRejectsNonGetMethods confirms POST / (or any
// non-GET/HEAD method) is not silently served the SPA shell.
func TestStaticHandlerRejectsNonGetMethods(t *testing.T) {
	h := newStaticHandler(fakeDistFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /: status = %d, want 404", rec.Code)
	}
}

// TestStaticHandlerMissingBuildReturns500WithClearMessage confirms an
// empty dist/ (no npm build has run) fails obviously — a clear 500
// with instructions, not a confusing 404 or a panic — matching
// serveIndex's doc comment.
func TestStaticHandlerMissingBuildReturns500WithClearMessage(t *testing.T) {
	h := newStaticHandler(fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET / with no build present: status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "web:build") {
		t.Errorf("GET / with no build present: body = %q, want a message pointing at how to produce one", rec.Body.String())
	}
}

// TestUnmatchedAPIRouteStaysUnderAuthenticateNotSPA is the concrete
// regression the /api/v1/ vs "/" subtree split exists to prevent: an
// unmatched path under /api/v1/ must keep going through authenticate
// (and 401 with no credentials when anonymous read is off), never fall
// through to the static/SPA handler and come back as "200 index.html".
func TestUnmatchedAPIRouteStaysUnderAuthenticateNotSPA(t *testing.T) {
	url := newStaticTestServer(t, false)

	resp := getNoAuth(t, url, "/api/v1/this-route-does-not-exist")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/this-route-does-not-exist with no credentials: status = %d, want 401 (must not fall through to the SPA shell)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("GET /api/v1/this-route-does-not-exist Content-Type = %q, want application/json (got body %s)", ct, body)
	}
}

// TestUnauthenticatedRoutesReachableWithoutCredentials asserts every
// route in unauthenticatedRoutes actually responds without needing
// credentials when anonymousRead is off — none should ever come back
// with the exact "authentication required" 401 resolvePrincipal
// returns for a route still behind authenticate. A route can still
// legitimately return its own 401 for other reasons (login with a
// bogus username/password is genuinely "invalid username or
// password", not "you needed credentials to even ask this question"),
// so the check is on the error message, not just the status code.
func TestUnauthenticatedRoutesReachableWithoutCredentials(t *testing.T) {
	url := newStaticTestServer(t, false)

	for _, route := range unauthenticatedRoutes {
		var body io.Reader
		if route.method == http.MethodPost {
			body = strings.NewReader(`{"username":"nobody","password":"wrong"}`)
		}
		req, err := http.NewRequest(route.method, url+route.pattern, body)
		if err != nil {
			t.Fatalf("new request %s %s: %v", route.method, route.pattern, err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", route.method, route.pattern, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized && strings.Contains(string(respBody), "authentication required") {
			t.Errorf("%s %s with no credentials: got the authenticate middleware's \"authentication required\" 401 — this route is supposed to be reachable without credentials, body=%s", route.method, route.pattern, respBody)
		}
	}
}
