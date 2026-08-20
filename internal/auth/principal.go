package auth

import (
	"context"

	"github.com/ArloB/tickets/internal/domain"
)

// Permission is a request's resolved content-permission level (product
// spec §4.2's exactly two levels; there is no per-project dimension).
type Permission int

const (
	// PermissionViewer can read non-deleted content and must never
	// reach a mutating internal/service call.
	PermissionViewer Permission = iota
	// PermissionEditor can create and change project content - every
	// authenticated human session and every valid, unrevoked agent
	// token (§4.2: no viewer-level accounts exist).
	PermissionEditor
)

// Principal is what authentication resolves once per request or MCP
// tool call and attaches to the context via WithPrincipal. A bare
// domain.ActorRef can't express "no valid credentials," "expired," or
// "anonymous, no actor at all" without inventing a fake actor row;
// Principal carries that alongside the resolved actor.
type Principal struct {
	// Actor is the zero value for an anonymous request. Anonymous
	// requests can never reach a mutating call (product spec §4.2), so
	// there is nothing to attribute a write to and nothing manufactures
	// a placeholder actor for one.
	Actor domain.ActorRef
	// Permission is this request's resolved level.
	Permission Permission
	// IsAdmin is true only for a human session whose account carries
	// the operational admin flag; never true for an agent.
	IsAdmin bool
	// AuthMethod records how Actor was resolved - "session", "bearer",
	// or "anonymous" - for logging and diagnostics, and to decide
	// whether the CSRF check applies (session only - see
	// internal/httpapi's requireEditor).
	AuthMethod string
	// CSRFToken is the session's own token, set only when AuthMethod ==
	// "session". A mutating request authenticated this way must present
	// a matching X-CSRF-Token header; bearer-token requests are exempt
	// (a browser never attaches an Authorization header automatically
	// the way it does a cookie, so there's nothing for CSRF to protect
	// there).
	CSRFToken string
}

type principalKey struct{}

// WithPrincipal attaches p to ctx. internal/httpapi's authentication
// middleware and internal/mcpsrv's tool-call wrapper are the two
// callers, each resolving a Principal exactly once per request/call.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the Principal WithPrincipal attached, or the
// zero Principal (anonymous, PermissionViewer) if none was - a context
// nothing has authenticated behaves exactly like an anonymous request
// rather than panicking or granting access by omission.
func FromContext(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}
