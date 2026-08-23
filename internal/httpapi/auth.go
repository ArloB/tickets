package httpapi

import (
	"io"
	"net/http"
	"strings"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Actor     string `json:"actor"`
	CSRFToken string `json:"csrf_token"`
}

// login authenticates a username/password pair (ADR 0004) and, on
// success, sets the session cookie and returns the CSRF token the
// client must echo back on every subsequent mutating request
// (requireEditor). Never itself requires authentication or CSRF —
// server.go mounts it outside the authenticate middleware, since
// requiring credentials to obtain credentials would be circular.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req loginRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	actor, ok, err := s.svc.Authenticate(r.Context(), req.Username, req.Password, clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !ok {
		writeError(w, r, &service.Error{Code: domain.ErrUnauthorized, Message: "invalid username or password"})
		return
	}

	sessionID, csrfToken, expiresAt, err := s.svc.CreateSession(r.Context(), actor)
	if err != nil {
		writeError(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, loginResponse{Actor: actor.String(), CSRFToken: csrfToken})
}

// logout deletes the caller's session, if any, and clears the cookie.
// Goes through the normal authenticate + requireEditor gate like any
// other mutating request (CSRF applies here too — an attacker forcing
// a cross-site logout is a minor but real CSRF target), so it is
// registered in server.go the same way createProject etc. are.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		if err := s.svc.DeleteSession(r.Context(), cookie.Value); err != nil {
			writeError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type meResponse struct {
	Actor      string `json:"actor,omitempty"`
	Permission string `json:"permission"`
	IsAdmin    bool   `json:"is_admin"`
	CSRFToken  string `json:"csrf_token,omitempty"`
}

// me reports the caller's own resolved principal — anonymous callers
// get a valid, non-error response too (when reachable at all — see
// authenticate) so a client can check its own auth state without
// treating "I'm anonymous" as a failure.
//
// CSRFToken is echoed back only for a session-authenticated caller
// (never for bearer/anonymous, matching requireEditor's own exemption
// for bearer callers), so a browser client that reloads mid-session
// can recover the token it needs for requireEditor's X-CSRF-Token
// check without forcing a fresh login. The response is marked
// no-store: it carries a live security token and must never be cached
// by a shared or intermediate cache.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p := auth.FromContext(r.Context())
	resp := meResponse{IsAdmin: p.IsAdmin}
	if p.Permission == auth.PermissionEditor {
		resp.Actor = p.Actor.String()
		resp.Permission = "editor"
	} else {
		resp.Permission = "viewer"
	}
	if p.AuthMethod == "session" {
		resp.CSRFToken = p.CSRFToken
	}
	writeJSON(w, http.StatusOK, resp)
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupResponse struct {
	Actor string `json:"actor"`
}

// setup creates the very first admin account over HTTP (product spec
// §6.5's "first-run setup" web view, §2.2's success criteria — a new
// user should get through first-run entirely in the browser, not need
// `tickets setup` on a terminal). Deliberately unauthenticated — like
// login, requiring credentials to obtain the first credentials would
// be circular — and deliberately thin: every safety property (refusing
// a second admin, hashing, the actual insert) lives in
// service.CreateAdminAccount, whose comment explains why its
// existence check is race-safe against exactly this concurrent-HTTP-
// request scenario.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req setupRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	actor, err := s.svc.CreateAdminAccount(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, setupResponse{Actor: actor.String()})
}

// requestIsSecure decides the session cookie's Secure flag. Direct TLS
// termination in this process (r.TLS != nil) is one signal; the other
// is X-Forwarded-Proto from a reverse proxy, since product spec §10's
// recommended TLS path is "normally through a documented reverse-proxy
// configuration" — Go's own listener typically only ever sees plain
// HTTP in that setup. Trusting X-Forwarded-Proto assumes the deployer's
// proxy overwrites rather than passes through any client-supplied
// value for it, which is standard reverse-proxy hygiene; on a bare
// loopback install (no proxy in front at all) neither signal fires and
// Secure is correctly false, or the cookie would never be sent back
// over the plain-HTTP loopback connection the default install uses.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
