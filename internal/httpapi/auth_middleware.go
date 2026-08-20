package httpapi

import (
	"net"
	"net/http"
	"strings"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// sessionCookieName is the browser-facing session cookie (ADR 0004).
const sessionCookieName = "tickets_session"

// authenticate resolves an auth.Principal for every request reaching
// it and attaches it to the request context (auth.WithPrincipal)
// before calling next. It does not itself decide whether the resolved
// Principal is *sufficient* for the route being called — requireEditor
// and requireAdmin do that — except for the one case with no per-route
// answer: when no credentials are presented at all and anonymous read
// is disabled, there is no permission level to grant at all (product
// spec §4.2: "Anonymous requests have this [viewer] level when
// anonymous reading is enabled" — the converse is no level), so that
// case is rejected here, uniformly, before any handler runs.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.resolvePrincipal(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func (s *Server) resolvePrincipal(r *http.Request) (auth.Principal, error) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		info, err := s.svc.ResolveSession(r.Context(), cookie.Value)
		if err != nil {
			return auth.Principal{}, err
		}
		return auth.Principal{
			Actor: info.Actor, Permission: auth.PermissionEditor, IsAdmin: info.IsAdmin,
			AuthMethod: "session", CSRFToken: info.CSRFToken,
		}, nil
	}

	if token, ok := bearerToken(r); ok {
		warnIfInsecureBearer(r)
		actor, err := s.svc.VerifyBearerToken(r.Context(), token)
		if err != nil {
			return auth.Principal{}, err
		}
		return auth.Principal{Actor: actor, Permission: auth.PermissionEditor, AuthMethod: "bearer"}, nil
	}

	if s.anonymousRead {
		return auth.Principal{Permission: auth.PermissionViewer, AuthMethod: "anonymous"}, nil
	}
	return auth.Principal{}, &service.Error{Code: domain.ErrUnauthorized, Message: "authentication required"}
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}

// warnIfInsecureBearer implements product spec §10's "refuse or
// strongly warn about bearer tokens over unencrypted non-loopback
// HTTP." A loopback bind is trusted regardless of TLS (that's the
// personal-install default the whole product optimizes for); anything
// else presenting a bearer token without TLS gets a warning, not a
// refusal — consistent with the non-loopback-bind warning
// internal/config already prints rather than hard-failing.
func warnIfInsecureBearer(r *http.Request) {
	if r.TLS != nil {
		return
	}
	if config.IsLoopback(hostWithoutPort(r.Host)) {
		return
	}
	logger.Warn("bearer token presented over non-loopback plain HTTP", "host", r.Host)
}

func hostWithoutPort(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// clientIP extracts the TCP peer address from r.RemoteAddr for login
// throttling (internal/auth.TooManyAttempts). It deliberately does not
// consult X-Forwarded-For: that header is trivially spoofable unless a
// reverse proxy is configured to strip/overwrite it, and the product's
// primary target is a direct loopback install (product spec §2) where
// RemoteAddr is already correct. A trusted-proxy X-Forwarded-For
// policy is real future work for team/shared deployments, not decided
// here.
func clientIP(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// requireEditor wraps a mutating handler, rejecting anything below
// Editor permission (product spec §4.2) before next runs, and — for a
// session-authenticated caller — requiring a matching X-CSRF-Token
// header (ADR 0004: "session security (CSRF...)"). Bearer-token
// callers are exempt: CSRF specifically targets a browser silently
// attaching a user's cookies to a cross-site request, and a bearer
// token in an Authorization header is never attached automatically the
// way a cookie is, so there's nothing for CSRF to protect there.
//
// This is a deliberate, narrow exception to ADR 0005's "service is the
// sole authorization boundary": the check depends only on the
// request's resolved Principal, never on which entity or project a
// mutation targets, so there is no service-layer concept for it to
// live inside. If per-project ACLs are ever added (product spec §18),
// that check would have to move into internal/service, since it would
// then depend on the mutation's target — this one doesn't.
func (s *Server) requireEditor(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p.Permission != auth.PermissionEditor {
			writeError(w, r, &service.Error{Code: domain.ErrForbidden, Message: "editor permission required"})
			return
		}
		if p.AuthMethod == "session" {
			if p.CSRFToken == "" || r.Header.Get("X-CSRF-Token") != p.CSRFToken {
				writeError(w, r, &service.Error{Code: domain.ErrForbidden, Message: "missing or invalid X-CSRF-Token"})
				return
			}
		}
		next(w, r)
	}
}

// requireAdmin wraps an admin-only handler (agent/token management,
// product spec §13). It composes requireEditor rather than duplicating
// its CSRF check: an admin session is also an editor session by
// construction (§4.2 — the admin flag is orthogonal to, not instead
// of, editor content permission).
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireEditor(func(w http.ResponseWriter, r *http.Request) {
		if !auth.FromContext(r.Context()).IsAdmin {
			writeError(w, r, &service.Error{Code: domain.ErrForbidden, Message: "admin permission required"})
			return
		}
		next(w, r)
	})
}
