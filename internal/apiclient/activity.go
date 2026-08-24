package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// ActivityEvent mirrors internal/httpapi/activity.go's activityEvent —
// one row of a project's activity feed (product spec §5.10).
type ActivityEvent struct {
	ID             int64     `json:"id"`
	Entity         string    `json:"entity,omitempty"`
	EntityKind     string    `json:"entity_kind"`
	Actor          string    `json:"actor"`
	EventType      string    `json:"event_type"`
	CommentID      *int64    `json:"comment_id,omitempty"`
	CommentExcerpt *string   `json:"comment_excerpt,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ActivityPage is GET /projects/{key}/activity's response envelope.
type ActivityPage struct {
	Events     []ActivityEvent `json:"events"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// ActivityListOptions is ListActivity's optional filters, AND-composed
// like every other list filter in this codebase
// (docs/contracts/list-filters.md). Zero value means no filter.
type ActivityListOptions struct {
	Actor      string
	EntityKind string
	EventType  string
}

// ListActivity is GET /projects/{key}/activity.
func (c *Client) ListActivity(ctx context.Context, projectKey string, opts ActivityListOptions, limit int, cursor string) (ActivityPage, error) {
	extra := url.Values{}
	if opts.Actor != "" {
		extra.Set("actor", opts.Actor)
	}
	if opts.EntityKind != "" {
		extra.Set("entity_kind", opts.EntityKind)
	}
	if opts.EventType != "" {
		extra.Set("event_type", opts.EventType)
	}

	var page ActivityPage
	path := "/projects/" + url.PathEscape(projectKey) + "/activity" + listQuery(extra, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}
