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
	Ref         string     `json:"ref"`
	Project     string     `json:"project"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	Creator     *string    `json:"creator,omitempty"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
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

type FeatureListFilters struct {
	Status       string
	Priority     string
	Creator      string
	UpdatedSince string
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
func (c *Client) CreateFeature(ctx context.Context, projectKey string, req CreateFeatureRequest, idempotencyKeys ...string) (Feature, error) {
	idempotencyKey := ""
	if len(idempotencyKeys) > 0 {
		idempotencyKey = idempotencyKeys[0]
	}
	var f Feature
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectKey)+"/features", req, &f, requestOptions{IdempotencyKey: idempotencyKey})
	return f, err
}

// GetFeature is GET /features/{ref}.
func (c *Client) GetFeature(ctx context.Context, ref string) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodGet, "/features/"+url.PathEscape(ref), nil, &f, requestOptions{})
	return f, err
}

func (c *Client) GetFeatureIncludingDeleted(ctx context.Context, ref string) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodGet, "/features/"+url.PathEscape(ref)+"?include_deleted=true", nil, &f, requestOptions{})
	return f, err
}

// ListFeatures is GET /projects/{key}/features. Compact rows only.

func (c *Client) ListFeatures(ctx context.Context, projectKey string, limit int, cursor string) (FeaturesPage, error) {
	return c.ListFeaturesFiltered(ctx, projectKey, FeatureListFilters{}, limit, cursor)
}

func (c *Client) ListFeaturesFiltered(ctx context.Context, projectKey string, filters FeatureListFilters, limit int, cursor string) (FeaturesPage, error) {
	var page FeaturesPage
	q := url.Values{}
	if filters.Status != "" {
		q.Set("status", filters.Status)
	}
	if filters.Priority != "" {
		q.Set("priority", filters.Priority)
	}
	if filters.Creator != "" {
		q.Set("creator", filters.Creator)
	}
	if filters.UpdatedSince != "" {
		q.Set("updated_since", filters.UpdatedSince)
	}
	path := "/projects/" + url.PathEscape(projectKey) + "/features" + listQuery(q, limit, cursor)
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

// SetFeatureStatus is POST /features/{ref}/status — the single-field
// workflow-status mutation UpdateFeature's full-representation PATCH
// doesn't cover, mirroring updateTicketStatus's own split from
// UpdateTicket.
func (c *Client) SetFeatureStatus(ctx context.Context, ref, status string, expectedVersion int64) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodPost, "/features/"+url.PathEscape(ref)+"/status",
		struct {
			Status string `json:"status"`
		}{Status: status},
		&f, requestOptions{IfMatch: &expectedVersion})
	return f, err
}

// ReorderFeature is POST /features/{ref}/reorder.
func (c *Client) ReorderFeature(ctx context.Context, ref string, afterRef *string, expectedVersion int64) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodPost, "/features/"+url.PathEscape(ref)+"/reorder",
		struct {
			AfterRef *string `json:"after_ref"`
		}{AfterRef: afterRef},
		&f, requestOptions{IfMatch: &expectedVersion})
	return f, err
}

// DeleteFeature is DELETE /features/{ref}?cascade=true|false (soft
// delete). ADR 0013 blocks it by default when the feature still holds
// non-deleted tickets — cascade soft-deletes them too.
func (c *Client) DeleteFeature(ctx context.Context, ref string, cascade bool, expectedVersion int64) (int64, error) {
	path := "/features/" + url.PathEscape(ref)
	if cascade {
		path += "?cascade=true"
	}
	var resp struct {
		Version int64 `json:"version"`
	}
	err := c.do(ctx, http.MethodDelete, path, nil, &resp, requestOptions{IfMatch: &expectedVersion})
	return resp.Version, err
}

// RestoreFeature is POST /features/{ref}/restore.
func (c *Client) RestoreFeature(ctx context.Context, ref string, expectedVersion int64) (Feature, error) {
	var f Feature
	err := c.do(ctx, http.MethodPost, "/features/"+url.PathEscape(ref)+"/restore", nil, &f, requestOptions{IfMatch: &expectedVersion})
	return f, err
}
