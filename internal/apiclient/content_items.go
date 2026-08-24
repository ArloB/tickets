package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ContentItem mirrors internal/httpapi/wire.go's contentItemDetail
// field-for-field (product spec §5.9). Kind is "plan" or "document";
// Representation is always "markdown" in Phase 5 Step 3.
type ContentItem struct {
	Ref            string    `json:"ref"`
	Project        string    `json:"project"`
	Kind           string    `json:"kind"`
	Title          string    `json:"title"`
	Representation string    `json:"representation"`
	Body           string    `json:"body"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ContentItemCompact mirrors internal/httpapi/wire.go's
// contentItemCompact — list rows never carry Body.
type ContentItemCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ContentItemsPage is GET /projects/{key}/plans|documents' response
// envelope.
type ContentItemsPage struct {
	Items      []ContentItemCompact `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

// CreateContentItemRequest is POST /projects/{key}/plans|documents'
// request body.
type CreateContentItemRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// UpdateContentItemRequest is PATCH /plans|documents/{ref}'s request
// body — a full-representation update, matching UpdateDecisionRequest's
// contract.
type UpdateContentItemRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ContentItemVersion mirrors internal/httpapi/wire.go's
// contentItemVersionEntry.
type ContentItemVersion struct {
	Version        int64     `json:"version"`
	Representation string    `json:"representation"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	EditedBy       string    `json:"edited_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// ContentItemVersionsPage is GET /plans|documents/{ref}/versions'
// response envelope.
type ContentItemVersionsPage struct {
	Versions []ContentItemVersion `json:"versions"`
}

// ContentItemDiff is GET /plans|documents/{ref}/diff's response shape.
type ContentItemDiff struct {
	FromVersion int64      `json:"from_version"`
	ToVersion   int64      `json:"to_version"`
	Title       []DiffLine `json:"title"`
	Body        []DiffLine `json:"body"`
}

// Every method below takes urlKind ("plans" or "documents") as its
// first argument rather than existing as a CreatePlan/CreateDocument
// pair — a code-review pass on an earlier version of this file (twelve
// near-identical methods differing only in URL segment) flagged that
// as real duplication risk, independently, from three separate review
// angles. cmd/tickets/content_item.go and web/src/api/content-items.ts
// already parameterize the same way (ContentItemUrlKind), so this
// mirrors both rather than being the one layer that didn't generalize.

// CreateContentItem is POST /projects/{key}/{urlKind}.
func (c *Client) CreateContentItem(ctx context.Context, urlKind, projectKey string, req CreateContentItemRequest, idempotencyKey string) (ContentItem, error) {
	var item ContentItem
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectKey)+"/"+urlKind, req, &item, requestOptions{IdempotencyKey: idempotencyKey})
	return item, err
}

// GetContentItem is GET /{urlKind}/{ref}.
func (c *Client) GetContentItem(ctx context.Context, urlKind, ref string) (ContentItem, error) {
	var item ContentItem
	err := c.do(ctx, http.MethodGet, "/"+urlKind+"/"+url.PathEscape(ref), nil, &item, requestOptions{})
	return item, err
}

// ListContentItems is GET /projects/{key}/{urlKind}. Compact rows only.
func (c *Client) ListContentItems(ctx context.Context, urlKind, projectKey string, limit int, cursor string) (ContentItemsPage, error) {
	var page ContentItemsPage
	path := "/projects/" + url.PathEscape(projectKey) + "/" + urlKind + listQuery(nil, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}

// UpdateContentItem is PATCH /{urlKind}/{ref}.
func (c *Client) UpdateContentItem(ctx context.Context, urlKind, ref string, req UpdateContentItemRequest, expectedVersion int64) (ContentItem, error) {
	var item ContentItem
	err := c.do(ctx, http.MethodPatch, "/"+urlKind+"/"+url.PathEscape(ref), req, &item, requestOptions{IfMatch: &expectedVersion})
	return item, err
}

// ListContentItemVersions is GET /{urlKind}/{ref}/versions.
func (c *Client) ListContentItemVersions(ctx context.Context, urlKind, ref string) (ContentItemVersionsPage, error) {
	var page ContentItemVersionsPage
	err := c.do(ctx, http.MethodGet, "/"+urlKind+"/"+url.PathEscape(ref)+"/versions", nil, &page, requestOptions{})
	return page, err
}

// GetContentItemDiff is GET /{urlKind}/{ref}/diff?from=&to=.
func (c *Client) GetContentItemDiff(ctx context.Context, urlKind, ref string, from, to int64) (ContentItemDiff, error) {
	var diff ContentItemDiff
	path := "/" + urlKind + "/" + url.PathEscape(ref) + "/diff?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
	err := c.do(ctx, http.MethodGet, path, nil, &diff, requestOptions{})
	return diff, err
}
