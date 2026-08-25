package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// FeatureBriefRow mirrors internal/httpapi/project_brief.go's
// featureBriefRow — FeatureCompact plus a ticket-progress summary.
type FeatureBriefRow struct {
	Ref          string    `json:"ref"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	Priority     string    `json:"priority"`
	Version      int64     `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
	TicketsTotal int       `json:"tickets_total"`
	TicketsDone  int       `json:"tickets_done"`
}

// ProjectBrief mirrors internal/httpapi/project_brief.go's
// projectBriefView — GET /projects/{key}/brief's response shape,
// product spec §12's orientation read.
type ProjectBrief struct {
	Project         Project              `json:"project"`
	InProgress      []TicketCompact      `json:"in_progress"`
	IssueRegister   []TicketCompact      `json:"issue_register"`
	Features        []FeatureBriefRow    `json:"features"`
	RecentActivity  []ActivityEvent      `json:"recent_activity"`
	RecentDecisions []DecisionCompact    `json:"recent_decisions"`
	RecentPlans     []ContentItemCompact `json:"recent_plans"`
}

// GetProjectBrief is GET /projects/{key}/brief.
func (c *Client) GetProjectBrief(ctx context.Context, key string) (ProjectBrief, error) {
	var brief ProjectBrief
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(key)+"/brief", nil, &brief, requestOptions{})
	return brief, err
}
