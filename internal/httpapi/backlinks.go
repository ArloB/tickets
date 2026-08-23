package httpapi

import (
	"fmt"
	"net/http"

	"github.com/ArloB/tickets/internal/domain"
)

// backlinkView is one backlink edge on the wire. CommentID is present
// only when the mention came from a comment rather than the source
// entity's own Markdown body — see service.Backlink's doc comment.
type backlinkView struct {
	Ref       string `json:"ref"`
	CommentID *int64 `json:"comment_id,omitempty"`
}

// listBacklinks is GET .../backlinks on a ticket, feature, or
// decision — read-only, reusing parseAssociationRef the same way
// listAssociations/listLinks do, since backlinks are supported on
// exactly the same three entity kinds.
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
		refStr, ferr := domain.Format(b.SourceRef)
		if ferr != nil {
			writeError(w, r, fmt.Errorf("httpapi: format backlink source: %w", ferr))
			return
		}
		view := backlinkView{Ref: refStr}
		if b.SourceCommentID != 0 {
			id := b.SourceCommentID
			view.CommentID = &id
		}
		out[i] = view
	}
	writeJSON(w, http.StatusOK, map[string]any{"backlinks": out})
}
