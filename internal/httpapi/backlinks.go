package httpapi

import (
	"net/http"
)

// backlinkView is one backlink edge on the wire. CommentID is present
// only when the mention came from a comment rather than the source
// entity's own Markdown body — see service.Backlink's doc comment.
// Ref can now name a project (a bare key, e.g. "ABC") when the mention
// came from a comment on a project (Phase 6 Step 2) — the route's own
// {ref} target is still only a ticket/feature/decision/plan/document
// (parseAssociationRef), but a backlink's *source* isn't restricted to
// that set.
type backlinkView struct {
	Ref       string `json:"ref"`
	CommentID *int64 `json:"comment_id,omitempty"`
}

// listBacklinks is GET .../backlinks on a ticket, feature, decision,
// plan, or document — read-only, reusing parseAssociationRef the same
// way listAssociations/listLinks do for the route's own {ref} target.
func (s *Server) listBacklinks(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	backlinks, err := s.svc.GetBacklinks(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]backlinkView, len(backlinks))
	for i, b := range backlinks {
		view := backlinkView{Ref: b.SourceRef}
		if b.SourceCommentID != 0 {
			id := b.SourceCommentID
			view.CommentID = &id
		}
		out[i] = view
	}
	writeJSON(w, http.StatusOK, map[string]any{"backlinks": out})
}
