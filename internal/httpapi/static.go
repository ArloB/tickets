package httpapi

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ArloB/tickets/web"
)

// staticHandler serves the embedded production web UI build (the Vite
// output under web/dist, embedded as web.Dist) plus a single-page-app
// fallback: a GET/HEAD request for a path that isn't a real file in
// the build, and doesn't look like an asset request, gets index.html
// instead — so a client-side route like /projects/ABC survives a hard
// refresh. A path that does look like an asset request (a file
// extension in its final segment, e.g. "assets/index-abcd123.js") and
// isn't found gets a plain 404 instead of an HTML page served with the
// wrong content type — a stale asset reference should fail obviously,
// not produce a baffling MIME error in the browser console. Anything
// that resolves to a directory (e.g. bare "assets") also 404s rather
// than falling into directory-listing behavior.
//
// index.html itself is always served by reading its bytes directly
// (serveIndex below), never via http.FileServerFS — net/http's
// built-in file handler special-cases any request whose final path
// segment is literally "index.html" and issues a redirect to the
// containing directory (avoiding two URLs for one piece of content).
// That's the right behavior for a request that spells out
// ".../index.html" by name, but the SPA fallback constructs exactly
// that request internally for every non-file route, which would
// immediately redirect back to the original URL and loop forever —
// confirmed by TestStaticHandlerSPAFallback failing with "stopped
// after 10 redirects" before this was special-cased.
//
// Mounted at "/" in NewHandler, strictly lower precedence than the
// "/api/v1/" subtree — see NewHandler's doc comment for why that split
// exists and what it protects against.
func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		// web.Dist always embeds dist/ (go:embed all:dist) — Sub can
		// only fail if that directory is missing from the build,
		// which would already have failed at compile time.
		panic("httpapi: web.Dist has no dist/ subtree: " + err.Error())
	}
	return newStaticHandler(sub)
}

// newStaticHandler builds staticHandler's logic against any fs.FS,
// not just the real embedded web.Dist — so static_test.go can drive it
// against a small in-memory fstest.MapFS instead of depending on
// web/dist/ actually containing a real `npm run build` output. Go
// tests must pass on a bare `go build`/`go test` checkout with no
// Node/npm involved at all (ADR 0010's promise, which covers `go test`
// exactly as much as `go build`) — before this split, these tests
// depended on whatever happened to be sitting in web/dist/ locally,
// silently passing or failing depending on build state unrelated to
// the code under test.
func newStaticHandler(sub fs.FS) http.Handler {
	fileServer := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		reqPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if reqPath != "" && reqPath != "index.html" {
			if info, err := fs.Stat(sub, reqPath); err == nil && !info.IsDir() {
				if strings.HasPrefix(reqPath, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			if looksLikeAssetRequest(reqPath) {
				http.NotFound(w, r)
				return
			}
		}

		serveIndex(w, r, sub)
	})
}

// serveIndex writes dist/index.html's content directly via
// http.ServeContent (which serves from bytes/Range/If-Modified-Since
// without any name-based redirect logic) rather than delegating to an
// fs.FS-backed file server by path — see staticHandler's doc comment
// for why the latter would redirect-loop for every SPA fallback.
func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "web UI build not found — run `task web:build` (or `npm run build` in web/) to produce it", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(data))
}

// looksLikeAssetRequest reports whether reqPath names a specific file
// (a dot in its final path segment, e.g. "assets/app.js" or
// "favicon.ico") rather than a client-side route such as
// "projects/ABC", which never contains a dot in this app's routing
// scheme. An empty path (the site root) is never an asset request.
func looksLikeAssetRequest(reqPath string) bool {
	if reqPath == "" {
		return false
	}
	return strings.Contains(path.Base(reqPath), ".")
}

// securityHeaders applies product spec §10's baseline response
// headers to every response this server sends, API and static alike:
// a restrictive Content-Security-Policy (same-origin only — the
// embedded web UI is self-contained per the Artifact-style constraint
// of shipping no third-party script/style/font dependency without a
// deliberate CSP update alongside it) and the usual MIME/frame
// hardening pairing with it.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
