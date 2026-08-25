package httpapi

import (
	"net/http"

	"github.com/ArloB/tickets/internal/service"
)

// featureBriefRow is one entry in projectBriefView's Features section —
// featureCompact plus the ticket-progress summary
// service.FeatureBriefRow carries.
type featureBriefRow struct {
	featureCompact
	TicketsTotal int `json:"tickets_total"`
	TicketsDone  int `json:"tickets_done"`
}

// projectBriefView is GET /projects/{key}/brief's response shape
// (product spec §12, plan.md's Phase 6 Step 5) — every section reuses
// this codebase's existing compact wire types, since a brief is a
// summary of data the full endpoints already expose in detail.
type projectBriefView struct {
	Project         projectDetail        `json:"project"`
	InProgress      []ticketCompact      `json:"in_progress"`
	IssueRegister   []ticketCompact      `json:"issue_register"`
	Features        []featureBriefRow    `json:"features"`
	RecentActivity  []activityEvent      `json:"recent_activity"`
	RecentDecisions []decisionCompact    `json:"recent_decisions"`
	RecentPlans     []contentItemCompact `json:"recent_plans"`
}

func toProjectBriefView(b service.ProjectBrief) projectBriefView {
	inProgress := make([]ticketCompact, len(b.InProgress))
	for i, t := range b.InProgress {
		inProgress[i] = toTicketCompact(t)
	}
	issues := make([]ticketCompact, len(b.IssueRegister))
	for i, t := range b.IssueRegister {
		issues[i] = toTicketCompact(t)
	}
	features := make([]featureBriefRow, len(b.Features))
	for i, f := range b.Features {
		features[i] = featureBriefRow{
			featureCompact: toFeatureCompact(f.Feature),
			TicketsTotal:   f.TicketsTotal,
			TicketsDone:    f.TicketsDone,
		}
	}
	activity := make([]activityEvent, len(b.RecentActivity))
	for i, e := range b.RecentActivity {
		activity[i] = toActivityEvent(e)
	}
	decisions := make([]decisionCompact, len(b.RecentDecisions))
	for i, d := range b.RecentDecisions {
		decisions[i] = toDecisionCompact(d)
	}
	plans := make([]contentItemCompact, len(b.RecentPlans))
	for i, p := range b.RecentPlans {
		plans[i] = toContentItemCompact(p)
	}
	return projectBriefView{
		Project:         toProjectDetail(b.Project),
		InProgress:      inProgress,
		IssueRegister:   issues,
		Features:        features,
		RecentActivity:  activity,
		RecentDecisions: decisions,
		RecentPlans:     plans,
	}
}

func (s *Server) getProjectBrief(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	brief, err := s.svc.ProjectBrief(r.Context(), key)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProjectBriefView(brief))
}
