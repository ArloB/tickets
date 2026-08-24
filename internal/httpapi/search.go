package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// searchHit is one row of GET /search's result page — mirrors
// Backlink's ref+comment_id shape (docs/contracts) for a comment hit,
// so a client renders both the same way.
type searchHit struct {
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	CommentID *int64 `json:"comment_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Snippet   string `json:"snippet"`
}

type searchPage struct {
	Hits       []searchHit `json:"hits"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

func toSearchHit(h service.SearchHit) searchHit {
	return searchHit{Kind: h.Kind, Ref: h.Ref, CommentID: h.CommentID, Title: h.Title, Snippet: h.Snippet}
}

// search handles GET /api/v1/search (product spec §5.12/§6.3): a
// unified full-text search over tickets/features/decisions/plans/
// documents, comments, attachment names, and external link titles/
// URLs. q is required; project/kind/status/limit/cursor all narrow or
// paginate an otherwise-global search.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "limit", Message: "limit must be a non-negative integer"})
			return
		}
		limit = n
	}

	var kinds []string
	if v := q.Get("kind"); v != "" {
		kinds = strings.Split(v, ",")
	}

	result, err := s.svc.Search(r.Context(), service.SearchRequest{
		Query: q.Get("q"), ProjectKey: q.Get("project"), Kinds: kinds, Status: q.Get("status"),
		Limit: limit, Cursor: q.Get("cursor"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]searchHit, len(result.Hits))
	for i, h := range result.Hits {
		out[i] = toSearchHit(h)
	}
	writeJSON(w, http.StatusOK, searchPage{Hits: out, NextCursor: result.NextCursor})
}
