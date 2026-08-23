package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// externalLinkView is one link on the wire. Compact by construction —
// there is no separate detail shape, unlike tickets/features/decisions
// (docs/contracts/representations.md): a link has only three fields
// total, so compact and detail would be identical.
type externalLinkView struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type addLinkRequest struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// addLink is POST .../links on a ticket, feature, or decision — one
// handler reused across all three route registrations, the same
// pattern addAssociation uses (relationships.go), since
// parseAssociationRef doesn't restrict which of the three kinds ref
// names.
func (s *Server) addLink(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req addLinkRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	link, err := s.svc.AddExternalLink(r.Context(), service.AddExternalLinkRequest{
		Ref: ref, Title: req.Title, URL: req.URL,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, externalLinkView{ID: link.ID, Title: link.Title, URL: link.URL})
}

// removeLink is DELETE .../links/{id} — id is a path segment, not a
// body, the same reasoning removeRelationship's doc comment gives
// (relationships.go): a link id is fully identified by the URL alone.
func (s *Server) removeLink(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	id, perr := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if perr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "id", Message: "id must be an integer"})
		return
	}

	if err := s.svc.RemoveExternalLink(r.Context(), service.RemoveExternalLinkRequest{
		Ref: ref, LinkID: id,
	}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	links, err := s.svc.GetExternalLinks(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]externalLinkView, len(links))
	for i, l := range links {
		out[i] = externalLinkView{ID: l.ID, Title: l.Title, URL: l.URL}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}
