package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Decision mirrors internal/httpapi/wire.go's decisionDetail
// field-for-field (product spec §5.8). SupersededBy is set on the
// *old* decision, pointing at the *new* one that replaces it; nil
// until an update links it.
type Decision struct {
	Ref          string    `json:"ref"`
	Project      string    `json:"project"`
	Title        string    `json:"title"`
	Context      string    `json:"context"`
	Decision     string    `json:"decision"`
	Rationale    string    `json:"rationale"`
	Consequences string    `json:"consequences"`
	Status       string    `json:"status"`
	SupersededBy *string   `json:"superseded_by,omitempty"`
	Version      int64     `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// DecisionCompact mirrors internal/httpapi/wire.go's decisionCompact —
// list rows never carry Context/Decision/Rationale/Consequences, per
// product spec §7.2/§11.
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
	Title        string `json:"title"`
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Rationale    string `json:"rationale"`
	Consequences string `json:"consequences"`
}

// UpdateDecisionRequest is PATCH /decisions/{ref}'s request body — a
// full-representation update, matching UpdateFeatureRequest's own
// contract (no partial-field route). SupersededBy "" clears an
// existing supersession link, the same full-representation contract
// every other field here has.
type UpdateDecisionRequest struct {
	Title        string `json:"title"`
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Rationale    string `json:"rationale"`
	Consequences string `json:"consequences"`
	Status       string `json:"status"`
	SupersededBy string `json:"superseded_by"`
}

// DecisionVersion mirrors internal/httpapi/wire.go's
// decisionVersionEntry — one archived prior state of a decision
// (product spec §5.8).
type DecisionVersion struct {
	Version      int64     `json:"version"`
	Title        string    `json:"title"`
	Context      string    `json:"context"`
	Decision     string    `json:"decision"`
	Rationale    string    `json:"rationale"`
	Consequences string    `json:"consequences"`
	Status       string    `json:"status"`
	EditedBy     string    `json:"edited_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// DecisionVersionsPage is GET /decisions/{ref}/versions' response
// envelope.
type DecisionVersionsPage struct {
	Versions []DecisionVersion `json:"versions"`
}

// DiffLine mirrors internal/httpapi/wire.go's diffLine.
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// DecisionDiff is GET /decisions/{ref}/diff's response shape.
type DecisionDiff struct {
	FromVersion  int64      `json:"from_version"`
	ToVersion    int64      `json:"to_version"`
	Title        []DiffLine `json:"title"`
	Context      []DiffLine `json:"context"`
	Decision     []DiffLine `json:"decision"`
	Rationale    []DiffLine `json:"rationale"`
	Consequences []DiffLine `json:"consequences"`
	StatusFrom   string     `json:"status_from"`
	StatusTo     string     `json:"status_to"`
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

// ListDecisionVersions is GET /decisions/{ref}/versions.
func (c *Client) ListDecisionVersions(ctx context.Context, ref string) (DecisionVersionsPage, error) {
	var page DecisionVersionsPage
	err := c.do(ctx, http.MethodGet, "/decisions/"+url.PathEscape(ref)+"/versions", nil, &page, requestOptions{})
	return page, err
}

// GetDecisionDiff is GET /decisions/{ref}/diff?from=&to=.
func (c *Client) GetDecisionDiff(ctx context.Context, ref string, from, to int64) (DecisionDiff, error) {
	var diff DecisionDiff
	path := "/decisions/" + url.PathEscape(ref) + "/diff?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
	err := c.do(ctx, http.MethodGet, path, nil, &diff, requestOptions{})
	return diff, err
}
