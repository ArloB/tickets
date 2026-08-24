package httpapi

import (
	"net/http"

	"github.com/ArloB/tickets/internal/service"
)

// Server holds the handler dependencies. Server itself is unexported
// so nothing outside this package constructs handlers directly (ADR
// 0005: httpapi is a thin translation layer, never a second place
// business logic lives); NewHandler is what cmd/tickets wires up.
type Server struct {
	svc *service.Service
	// anonymousRead is product spec §4.2's server-wide toggle, resolved
	// once by internal/config.Load and passed in at construction —
	// httpapi itself never re-derives it from the bind address.
	anonymousRead bool
}

// routePermission is the minimum auth.Permission a route table entry
// requires, expressed as data rather than as a hand-picked wrapper
// call per route — see routeTable's doc for why.
type routePermission int

const (
	// routeViewer routes get no permission wrapper: authenticate alone
	// (session, bearer token, or anonymous-if-enabled) is sufficient.
	routeViewer routePermission = iota
	routeEditor
	routeAdmin
)

// routeEntry is one registration: an HTTP method, a ServeMux pattern
// (path only — NewHandler prefixes /api/v1), the permission level it
// requires, and the handler itself, still unwrapped.
type routeEntry struct {
	method     string
	pattern    string
	permission routePermission
	handler    http.HandlerFunc
}

// routeTable is every /api/v1 route this server serves, together with
// the permission NewHandler wraps it in. This exists as data — not as
// a sequence of hand-written protected.HandleFunc(s.requireEditor(...))
// calls — specifically so a route's permission can never silently
// come loose from its wrapping: NewHandler applies the wrapper purely
// as a function of this table's permission field, so a route
// registered here with routeEditor or routeAdmin is *always* wrapped,
// and TestEveryMutatingRouteRequiresAtLeastEditor
// (route_table_test.go) can check the property that actually matters
// — every non-GET route requires at least Editor — by reading this
// table directly, with no HTTP round trip needed. Add every new route
// here, not as a direct protected.HandleFunc call.
func (s *Server) routeTable() []routeEntry {
	return []routeEntry{
		{http.MethodGet, "/api/v1/auth/me", routeViewer, s.me},
		{http.MethodPost, "/api/v1/auth/logout", routeEditor, s.logout},

		{http.MethodPost, "/api/v1/projects", routeEditor, s.createProject},
		{http.MethodGet, "/api/v1/projects", routeViewer, s.listProjects},
		{http.MethodGet, "/api/v1/projects/{key}", routeViewer, s.getProject},
		{http.MethodPost, "/api/v1/projects/{key}/tickets", routeEditor, s.createTicket},
		{http.MethodGet, "/api/v1/projects/{key}/tickets", routeViewer, s.listTickets},
		{http.MethodGet, "/api/v1/tickets/{ref}", routeViewer, s.getTicket},
		{http.MethodPatch, "/api/v1/tickets/{ref}", routeEditor, s.updateTicketStatus},
		{http.MethodPut, "/api/v1/tickets/{ref}", routeEditor, s.updateTicketFields},
		{http.MethodDelete, "/api/v1/tickets/{ref}", routeEditor, s.deleteTicket},
		{http.MethodPost, "/api/v1/tickets/{ref}/assign", routeEditor, s.assignTicket},
		{http.MethodPost, "/api/v1/tickets/{ref}/move", routeEditor, s.moveTicket},
		{http.MethodPost, "/api/v1/tickets/{ref}/reorder", routeEditor, s.reorderTicket},
		{http.MethodPost, "/api/v1/tickets/{ref}/restore", routeEditor, s.restoreTicket},

		{http.MethodPost, "/api/v1/projects/{key}/features", routeEditor, s.createFeature},
		{http.MethodGet, "/api/v1/projects/{key}/features", routeViewer, s.listFeatures},
		{http.MethodGet, "/api/v1/features/{ref}", routeViewer, s.getFeature},
		{http.MethodPatch, "/api/v1/features/{ref}", routeEditor, s.updateFeature},
		{http.MethodPost, "/api/v1/features/{ref}/status", routeEditor, s.updateFeatureStatus},
		{http.MethodPost, "/api/v1/features/{ref}/reorder", routeEditor, s.reorderFeature},
		{http.MethodDelete, "/api/v1/features/{ref}", routeEditor, s.deleteFeature},
		{http.MethodPost, "/api/v1/features/{ref}/restore", routeEditor, s.restoreFeature},

		{http.MethodPost, "/api/v1/tickets/{ref}/comments", routeEditor, s.createComment},
		{http.MethodGet, "/api/v1/tickets/{ref}/comments", routeViewer, s.listComments},
		{http.MethodGet, "/api/v1/comments/{id}", routeViewer, s.getComment},
		{http.MethodGet, "/api/v1/comments/{id}/history", routeViewer, s.getCommentHistory},
		{http.MethodPatch, "/api/v1/comments/{id}", routeEditor, s.editComment},
		{http.MethodDelete, "/api/v1/comments/{id}", routeEditor, s.deleteComment},

		{http.MethodPost, "/api/v1/tickets/{ref}/relationships", routeEditor, s.addRelationship},
		{http.MethodGet, "/api/v1/tickets/{ref}/relationships", routeViewer, s.listRelationships},
		{http.MethodDelete, "/api/v1/tickets/{ref}/relationships/{type}/{target}", routeEditor, s.removeRelationship},

		{http.MethodPost, "/api/v1/tickets/{ref}/associations", routeEditor, s.addAssociation},
		{http.MethodGet, "/api/v1/tickets/{ref}/associations", routeViewer, s.listAssociations},
		{http.MethodDelete, "/api/v1/tickets/{ref}/associations/{target}", routeEditor, s.removeAssociation},
		{http.MethodPost, "/api/v1/features/{ref}/associations", routeEditor, s.addAssociation},
		{http.MethodGet, "/api/v1/features/{ref}/associations", routeViewer, s.listAssociations},
		{http.MethodDelete, "/api/v1/features/{ref}/associations/{target}", routeEditor, s.removeAssociation},
		{http.MethodPost, "/api/v1/decisions/{ref}/associations", routeEditor, s.addAssociation},
		{http.MethodGet, "/api/v1/decisions/{ref}/associations", routeViewer, s.listAssociations},
		{http.MethodDelete, "/api/v1/decisions/{ref}/associations/{target}", routeEditor, s.removeAssociation},

		{http.MethodPost, "/api/v1/tickets/{ref}/links", routeEditor, s.addLink},
		{http.MethodGet, "/api/v1/tickets/{ref}/links", routeViewer, s.listLinks},
		{http.MethodDelete, "/api/v1/tickets/{ref}/links/{id}", routeEditor, s.removeLink},
		{http.MethodPost, "/api/v1/features/{ref}/links", routeEditor, s.addLink},
		{http.MethodGet, "/api/v1/features/{ref}/links", routeViewer, s.listLinks},
		{http.MethodDelete, "/api/v1/features/{ref}/links/{id}", routeEditor, s.removeLink},
		{http.MethodPost, "/api/v1/decisions/{ref}/links", routeEditor, s.addLink},
		{http.MethodGet, "/api/v1/decisions/{ref}/links", routeViewer, s.listLinks},
		{http.MethodDelete, "/api/v1/decisions/{ref}/links/{id}", routeEditor, s.removeLink},

		{http.MethodGet, "/api/v1/tickets/{ref}/backlinks", routeViewer, s.listBacklinks},
		{http.MethodGet, "/api/v1/features/{ref}/backlinks", routeViewer, s.listBacklinks},
		{http.MethodGet, "/api/v1/decisions/{ref}/backlinks", routeViewer, s.listBacklinks},

		{http.MethodPost, "/api/v1/projects/{key}/decisions", routeEditor, s.createDecision},
		{http.MethodGet, "/api/v1/projects/{key}/decisions", routeViewer, s.listDecisions},
		{http.MethodGet, "/api/v1/decisions/{ref}", routeViewer, s.getDecision},
		{http.MethodPatch, "/api/v1/decisions/{ref}", routeEditor, s.updateDecision},
		{http.MethodGet, "/api/v1/decisions/{ref}/versions", routeViewer, s.listDecisionVersions},
		{http.MethodGet, "/api/v1/decisions/{ref}/diff", routeViewer, s.getDecisionDiff},

		{http.MethodGet, "/api/v1/projects/{key}/activity", routeViewer, s.listActivity},

		{http.MethodPost, "/api/v1/agents", routeAdmin, s.createAgent},
		{http.MethodGet, "/api/v1/agents", routeAdmin, s.listAgents},
		{http.MethodPost, "/api/v1/agents/{name}/tokens", routeAdmin, s.createAgentToken},
		{http.MethodGet, "/api/v1/agents/{name}/tokens", routeAdmin, s.listAgentTokens},
		{http.MethodDelete, "/api/v1/agents/{name}/tokens/{id}", routeAdmin, s.revokeAgentToken},
	}
}

// unauthenticatedRoutes is the exact set of routes reachable with no
// credentials at all: pure liveness/readiness probes (product spec
// §9 — an orchestrator probing these shouldn't need an account), and
// the two routes whose entire purpose is *obtaining* credentials —
// login and first-run admin setup — where requiring credentials to
// get credentials would be circular. static/SPA asset serving is
// intentionally unauthenticated too (the sign-in page itself is a
// static asset; see staticHandler). Kept as a named list, not just
// scattered mux.HandleFunc calls, so the route-set regression test in
// route_table_test.go can assert against it directly instead of
// re-deriving it from NewHandler's body.
var unauthenticatedRoutes = []struct{ method, pattern string }{
	{http.MethodGet, "/healthz"},
	{http.MethodGet, "/readyz"},
	{http.MethodPost, "/api/v1/auth/login"},
	{http.MethodPost, "/api/v1/setup"},
}

// NewHandler builds the full server mux: unauthenticatedRoutes, the
// "/api/v1/" subtree (every route in routeTable, wrapped in
// authenticate and, per its permission field, requireEditor or
// requireAdmin), and the embedded web UI mounted at "/".
//
// "/api/v1/" is registered as its own subtree pattern rather than
// authenticate(protected) being mounted at the bare "/" (as it was
// before the web UI existed) — that split matters for two reasons.
// First, a logged-out browser requesting "/" for the sign-in page must
// not 401 before it ever sees HTML: only requests actually under
// /api/v1/ need a resolved Principal at all. Second, an unmatched path
// like "/api/v1/typo" must keep 404ing from the protected mux, not
// silently fall through to the SPA handler and come back as
// "200 index.html" — scoping the subtree precisely is what keeps that
// separation from eroding as routes are added on either side.
//
// authenticate resolves a Principal from a session cookie or bearer
// token, or grants anonymous Viewer access when anonymousRead allows
// it, or rejects outright otherwise (product spec §4.2). routeTable's
// permission field drives which of requireEditor/requireAdmin (if any)
// wraps each handler before it's registered.
func NewHandler(svc *service.Service, anonymousRead bool) http.Handler {
	s := &Server{svc: svc, anonymousRead: anonymousRead}

	protected := http.NewServeMux()
	for _, e := range s.routeTable() {
		h := e.handler
		switch e.permission {
		case routeAdmin:
			h = s.requireAdmin(h)
		case routeEditor:
			h = s.requireEditor(h)
		}
		protected.HandleFunc(e.method+" "+e.pattern, h)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.Handle("/api/v1/", s.authenticate(protected))
	mux.Handle("/", s.staticHandler())

	return securityHeaders(mux)
}
