package httpapi

import "net/http"

// healthz reports process liveness only — it never touches the
// database (product spec §9: liveness and readiness are distinct).
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
