package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// SearchHit mirrors internal/httpapi/search.go's searchHit — one
// ranked row of a full-text search result (product spec §5.12).
type SearchHit struct {
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	CommentID *int64 `json:"comment_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Snippet   string `json:"snippet"`
}

// SearchPage is GET /search's response envelope.
type SearchPage struct {
	Hits       []SearchHit `json:"hits"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// SearchOptions is Search's optional filters. Zero value means no
// filter on that dimension — Kinds empty means every kind.
type SearchOptions struct {
	Project string
	Kinds   []string
	Status  string
}

// Search is GET /search: a unified full-text search over tickets/
// features/decisions/plans/documents and comments.
func (c *Client) Search(ctx context.Context, query string, opts SearchOptions, limit int, cursor string) (SearchPage, error) {
	extra := url.Values{}
	extra.Set("q", query)
	if opts.Project != "" {
		extra.Set("project", opts.Project)
	}
	if len(opts.Kinds) > 0 {
		extra.Set("kind", strings.Join(opts.Kinds, ","))
	}
	if opts.Status != "" {
		extra.Set("status", opts.Status)
	}

	var page SearchPage
	path := "/search" + listQuery(extra, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}
