package httpapi

import (
	"net/http"

	"github.com/ArloB/tickets/internal/buildinfo"
)

// healthz reports process liveness only — it never touches the
// database (product spec §9: liveness and readiness are distinct).
// version/commit aren't sensitive configuration (§9's "without
// exposing sensitive configuration" concerns things like data
// directory paths or credentials, not the build identity), and
// knowing which server version answered is useful for an operator
// probing an installation from outside — the backup manifest (Phase 6
// Step 4) records the same buildinfo.Version for the same reason.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": buildinfo.Version})
}

// readyz reports whether the database is actually reachable. Returning
// 200 unconditionally here would defeat the point of a separate
// readiness endpoint (§9).
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
