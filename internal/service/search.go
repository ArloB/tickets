package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// searchKinds is the valid value set for SearchRequest.Kinds — every
// kind a search hit can have, ticket/feature/decision/plan/document
// (product spec §5.2's principal kinds) plus "comment" (§5.10), which
// has no entities.kind of its own but is a real, filterable search hit
// kind (ADR 0018).
var searchKinds = map[string]bool{
	"ticket": true, "feature": true, "decision": true,
	"plan": true, "document": true, "comment": true,
	"attachment": true, "link": true,
}

// SearchRequest is Search's input. ProjectKey "" searches every
// project; Kinds empty means every kind; Status "" means unfiltered.
// Status is deliberately not validated against a single enum here —
// unlike a kind-specific list filter, one search spans several kinds
// with different status vocabularies (workflow status for tickets/
// features, decision status for decisions, no status at all for
// plans/documents/comments), so an unrecognized value simply matches
// no rows rather than being rejected.
type SearchRequest struct {
	Query      string
	ProjectKey string
	Kinds      []string
	Status     string
	Limit      int
	Cursor     string
}

// SearchHit is one ranked search result — mirrors store.SearchHit,
// re-exported at the service boundary the same way every other list
// result type here wraps its store counterpart.
type SearchHit struct {
	Kind      string
	Ref       string
	CommentID *int64
	Title     string
	Snippet   string
}

// SearchResult is Search's output.
type SearchResult struct {
	Hits       []SearchHit
	NextCursor string
}

// Search runs a full-text search over tickets/features/decisions/
// plans/documents and comments (product spec §5.12), ranked by bm25.
// The query is sanitized (domain.SanitizeFTSQuery) before it ever
// reaches FTS5's MATCH — see that function's doc for why: raw user
// input containing FTS5 query-language syntax (a colon, an unbalanced
// quote, a bare boolean operator) would otherwise be a query-time
// syntax error, not a zero-result search.
func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	sanitized := domain.SanitizeFTSQuery(req.Query)
	if sanitized == "" {
		return SearchResult{}, newValidationError("q", "q is required")
	}

	var filters store.SearchFilters
	if req.ProjectKey != "" {
		proj, err := store.GetProjectByKey(ctx, s.store.DB(), req.ProjectKey)
		if errors.Is(err, store.ErrNotFound) {
			return SearchResult{}, newNotFoundError("project %q not found", req.ProjectKey)
		}
		if err != nil {
			return SearchResult{}, fmt.Errorf("service: look up project: %w", err)
		}
		filters.ProjectID = proj.ID
	}
	for _, k := range req.Kinds {
		if !searchKinds[k] {
			return SearchResult{}, newValidationError("kind", "invalid kind %q", k)
		}
	}
	filters.Kinds = req.Kinds
	filters.Status = req.Status

	limit := req.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	offset, derr := store.DecodeSearchOffsetCursor(req.Cursor)
	if derr != nil {
		return SearchResult{}, newValidationError("cursor", "invalid cursor")
	}

	page, err := store.Search(ctx, s.store.DB(), sanitized, filters, limit, offset)
	if err != nil {
		return SearchResult{}, fmt.Errorf("service: search: %w", err)
	}

	out := make([]SearchHit, len(page.Hits))
	for i, h := range page.Hits {
		out[i] = SearchHit{Kind: h.Kind, Ref: h.Ref, CommentID: h.CommentID, Title: h.Title, Snippet: h.Snippet}
	}
	return SearchResult{Hits: out, NextCursor: page.NextCursor}, nil
}

// RebuildSearchIndex clears and reindexes the entire search index from
// scratch (store.RebuildSearchIndex) — the administrative recovery
// path for anything the incremental UpsertSearchDocument call sites
// (ticket.go, feature.go, decision.go, content_item.go, comment.go,
// attachment.go, link.go) miss or get wrong.
func (s *Service) RebuildSearchIndex(ctx context.Context) (int, error) {
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("service: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	count, err := store.RebuildSearchIndex(ctx, tx)
	if err != nil {
		return 0, fmt.Errorf("service: rebuild search index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("service: commit: %w", err)
	}
	committed = true
	return count, nil
}
