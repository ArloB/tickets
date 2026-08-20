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

		{http.MethodPost, "/api/v1/agents", routeAdmin, s.createAgent},
		{http.MethodGet, "/api/v1/agents", routeAdmin, s.listAgents},
		{http.MethodPost, "/api/v1/agents/{name}/tokens", routeAdmin, s.createAgentToken},
		{http.MethodGet, "/api/v1/agents/{name}/tokens", routeAdmin, s.listAgentTokens},
		{http.MethodDelete, "/api/v1/agents/{name}/tokens/{id}", routeAdmin, s.revokeAgentToken},
	}
}

// NewHandler builds the /api/v1 router plus /healthz and /readyz.
// Three routes are reachable with no credentials at all: /healthz,
// /readyz (pure liveness/readiness, product spec §9 — an orchestrator
// probing these shouldn't need an account), and POST
// /api/v1/auth/login (requiring credentials to obtain credentials
// would be circular). Every other route goes through authenticate,
// which resolves a Principal from a session cookie or bearer token, or
// grants anonymous Viewer access when anonymousRead allows it, or
// rejects outright otherwise (product spec §4.2). routeTable's
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
	mux.Handle("/", s.authenticate(protected))

	return mux
}
