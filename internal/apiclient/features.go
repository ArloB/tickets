package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Feature mirrors internal/httpapi/wire.go's featureDetail
// field-for-field — see dto.go's top comment for why this isn't
// internal/domain.Feature.
type Feature struct {
	Ref         string    `json:"ref"`
	Project     string    `json:"project"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FeatureCompact mirrors internal/httpapi/wire.go's featureCompact —
// list rows never carry Description, per product spec §7.2/§11.
type FeatureCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FeaturesPage is GET /projects/{key}/features' response envelope.
type FeaturesPage struct {
	Features   []FeatureCompact `json:"features"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// CreateFeatureRequest is POST /projects/{key}/features' request body.
type CreateFeatureRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority,omitempty"`
}

// UpdateFeatureRequest is PATCH /features/{ref}'s request body — a
// full-representation update, matching internal/httpapi's
// updateFeatureRequest (no separate status-only route the way tickets
// have; features have no PUT/PATCH split).
type UpdateFeatureRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

// CreateFeature is POST /projects/{key}/features.
func (c *Client) CreateFeature(ctx context.Context, projectKey string, req CreateFeatureRequest) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectKey)+"/features", req, &f, requestOptions{})
	return f, err
}

// GetFeature is GET /features/{ref}.
func (c *Client) GetFeature(ctx context.Context, ref string) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodGet, "/features/"+url.PathEscape(ref), nil, &f, requestOptions{})
	return f, err
}

// ListFeatures is GET /projects/{key}/features. Compact rows only.
func (c *Client) ListFeatures(ctx context.Context, projectKey string, limit int, cursor string) (FeaturesPage, error) {
	var page FeaturesPage
	path := "/projects/" + url.PathEscape(projectKey) + "/features" + listQuery(nil, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}

// UpdateFeature is PATCH /features/{ref} — a full-representation
// update, so (unlike UpdateTicket) there is no merge-from-unset-fields
// convenience here; the caller supplies every field.
func (c *Client) UpdateFeature(ctx context.Context, ref string, req UpdateFeatureRequest, expectedVersion int64) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodPatch, "/features/"+url.PathEscape(ref), req, &f, requestOptions{IfMatch: &expectedVersion})
	return f, err
}
