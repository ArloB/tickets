package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Decision mirrors internal/httpapi/wire.go's decisionDetail
// field-for-field — Phase 3's minimal slice (product spec §5.8): no
// versioning/diff/supersession fields yet, those are Phase 5's
// extension of this same record.
type Decision struct {
	Ref       string    `json:"ref"`
	Project   string    `json:"project"`
	Title     string    `json:"title"`
	Context   string    `json:"context"`
	Decision  string    `json:"decision"`
	Rationale string    `json:"rationale"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DecisionCompact mirrors internal/httpapi/wire.go's decisionCompact —
// list rows never carry Context/Decision/Rationale, per product spec
// §7.2/§11.
type DecisionCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DecisionsPage is GET /projects/{key}/decisions' response envelope.
type DecisionsPage struct {
	Decisions  []DecisionCompact `json:"decisions"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// CreateDecisionRequest is POST /projects/{key}/decisions' request body.
type CreateDecisionRequest struct {
	Title     string `json:"title"`
	Context   string `json:"context"`
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
}

// UpdateDecisionRequest is PATCH /decisions/{ref}'s request body — a
// full-representation update, matching UpdateFeatureRequest's own
// contract (no partial-field route).
type UpdateDecisionRequest struct {
	Title     string `json:"title"`
	Context   string `json:"context"`
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
	Status    string `json:"status"`
}

// CreateDecision is POST /projects/{key}/decisions.
func (c *Client) CreateDecision(ctx context.Context, projectKey string, req CreateDecisionRequest, idempotencyKey string) (Decision, error) {
	var d Decision
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectKey)+"/decisions", req, &d, requestOptions{IdempotencyKey: idempotencyKey})
	return d, err
}

// GetDecision is GET /decisions/{ref}.
func (c *Client) GetDecision(ctx context.Context, ref string) (Decision, error) {
	var d Decision
	err := c.do(ctx, http.MethodGet, "/decisions/"+url.PathEscape(ref), nil, &d, requestOptions{})
	return d, err
}

// ListDecisions is GET /projects/{key}/decisions. Compact rows only.
func (c *Client) ListDecisions(ctx context.Context, projectKey string, limit int, cursor string) (DecisionsPage, error) {
	var page DecisionsPage
	path := "/projects/" + url.PathEscape(projectKey) + "/decisions" + listQuery(nil, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}

// UpdateDecision is PATCH /decisions/{ref}.
func (c *Client) UpdateDecision(ctx context.Context, ref string, req UpdateDecisionRequest, expectedVersion int64) (Decision, error) {
	var d Decision
	err := c.do(ctx, http.MethodPatch, "/decisions/"+url.PathEscape(ref), req, &d, requestOptions{IfMatch: &expectedVersion})
	return d, err
}
