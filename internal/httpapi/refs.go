package httpapi

import (
	"net/http"
	"strings"
)

// resolvedRefView is one entry of GET /refs/resolve's answer. Kind,
// title, and status are omitted for an unresolved token, so the
// smallest useful client check is the presence of "exists".
type resolvedRefView struct {
	Ref    string `json:"ref"`
	Exists bool   `json:"exists"`
	Kind   string `json:"kind,omitempty"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
}

// resolveRefs is GET /api/v1/refs/resolve?refs=ABC-1,ABC-F2 — the
// existence check behind rendering references in Markdown prose as
// hyperlinks (ADR 0025). A GET, not a POST, both because it is a pure
// read and because route_table_test.go's invariant reserves every
// non-GET route for Editor and above; this one must be reachable by
// any viewer, since it answers exactly what a GET on each named
// record would.
//
// The project-scoped short form (#123) is not accepted here: it has
// meaning only relative to the body being rendered, which this route
// never sees. A caller expands it against its own project scope
// first, the same expansion domain.ScanReferences does with
// scopeProjectKey.
func (s *Server) resolveRefs(w http.ResponseWriter, r *http.Request) {
	var refs []string
	if v := r.URL.Query().Get("refs"); v != "" {
		for _, token := range strings.Split(v, ",") {
			if t := strings.TrimSpace(token); t != "" {
				refs = append(refs, t)
			}
		}
	}

	resolved, err := s.svc.ResolveRefs(r.Context(), refs)
	if err != nil {
		writeError(w, r, err)
		return
	}

	out := make([]resolvedRefView, len(resolved))
	for i, rr := range resolved {
		out[i] = resolvedRefView{Ref: rr.Ref, Exists: rr.Exists, Kind: rr.Kind, Title: rr.Title, Status: rr.Status}
	}
	writeJSON(w, http.StatusOK, map[string]any{"refs": out})
}
