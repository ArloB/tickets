package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// briefSectionLimit caps every ProjectBrief section at once — reusing
// defaultPageLimit, product spec §11's "MCP and CLI list responses
// default to at most 20 compact records," which the plan's own Step 5
// text cites for exactly this purpose. A brief's whole point is a
// fixed-size orientation read, not a full listing, so every section
// stays capped even where the underlying store query could return more.
const briefSectionLimit = defaultPageLimit

// FeatureBriefRow is one feature entry in a ProjectBrief: the feature
// itself plus a ticket-progress summary (store.FeatureTicketCounts) —
// "how much of this feature is done," not a full ticket listing.
type FeatureBriefRow struct {
	Feature      domain.Feature
	TicketsTotal int
	TicketsDone  int
}

// ProjectBrief is the composed orientation read `tickets project
// brief`, GET /projects/{key}/brief, and the project_brief MCP tool
// all return (product spec §12, plan.md's Phase 6 Step 5) — the
// recommended first call when getting oriented in a project, before
// any detail call narrows in on one record. Every field here is data
// this project already tracks at full size elsewhere; this is a
// summary view over it, not new data.
//
// InProgress/IssueRegister exclude done/cancelled tickets ("in
// progress and upcoming work," not history); RecentDecisions is
// accepted decisions only (a proposed or rejected decision isn't
// something a newly-oriented reader needs surfaced first). Every
// section is capped at briefSectionLimit.
type ProjectBrief struct {
	Project         domain.Project
	InProgress      []domain.Ticket
	IssueRegister   []domain.Ticket
	Features        []FeatureBriefRow
	RecentActivity  []ActivityEvent
	RecentDecisions []domain.Decision
	RecentPlans     []domain.ContentItem
}

// ProjectBrief composes existing reads — ListTickets (both views),
// ListFeatures plus per-feature ticket counts, ListActivity, and the
// two Recent* store queries Step 5 added — into one response, so a
// caller gets oriented in a project with a single call instead of the
// half-dozen list calls it replaces.
func (s *Service) ProjectBrief(ctx context.Context, key string) (ProjectBrief, error) {
	row, err := store.GetProjectByKey(ctx, s.store.DB(), key)
	if errors.Is(err, store.ErrNotFound) {
		return ProjectBrief{}, newNotFoundError("project %q not found", key)
	}
	if err != nil {
		return ProjectBrief{}, fmt.Errorf("service: look up project: %w", err)
	}

	inProgress, err := s.briefTickets(ctx, key, TicketListViewPriorityQueue)
	if err != nil {
		return ProjectBrief{}, err
	}
	issues, err := s.briefTickets(ctx, key, TicketListViewIssueRegister)
	if err != nil {
		return ProjectBrief{}, err
	}
	features, err := s.briefFeatures(ctx, row.ID)
	if err != nil {
		return ProjectBrief{}, err
	}
	activity, err := s.ListActivity(ctx, key, ActivityListFilters{}, briefSectionLimit, "")
	if err != nil {
		return ProjectBrief{}, err
	}

	decisionRows, err := store.RecentAcceptedDecisions(ctx, s.store.DB(), row.ID, briefSectionLimit)
	if err != nil {
		return ProjectBrief{}, fmt.Errorf("service: recent decisions: %w", err)
	}
	recentDecisions := make([]domain.Decision, len(decisionRows))
	for i, d := range decisionRows {
		recentDecisions[i] = d.Entity
	}

	planRows, err := store.RecentContentItems(ctx, s.store.DB(), row.ID, domain.KindPlan, briefSectionLimit)
	if err != nil {
		return ProjectBrief{}, fmt.Errorf("service: recent plans: %w", err)
	}
	recentPlans := make([]domain.ContentItem, len(planRows))
	for i, p := range planRows {
		recentPlans[i] = p.Entity
	}

	return ProjectBrief{
		Project:         row.Entity,
		InProgress:      inProgress,
		IssueRegister:   issues,
		Features:        features,
		RecentActivity:  activity.Events,
		RecentDecisions: recentDecisions,
		RecentPlans:     recentPlans,
	}, nil
}

// briefPageScan bounds how many pages briefTickets will walk looking
// for enough non-done/non-cancelled tickets to fill a section. Neither
// view's ordering (priority_rank/severity_rank, not status) pushes
// done/cancelled tickets to the back, so a project with many completed
// high-priority tickets needs more than one over-fetched page before
// the in-progress/upcoming work surfaces — this is what makes the scan
// bounded-but-not-single-page.
const briefPageScan = 10

// briefTickets walks view (priority queue or issue register) page by
// page, filtering out done/cancelled tickets, until briefSectionLimit
// survive or briefPageScan pages have been read. TicketListFilters
// only supports a single Status value, not an excluded set, so
// filtering in Go — rather than one query per candidate status — is
// what keeps this affordable; the page walk (instead of one large
// over-fetch) is what keeps it correct on a project where completed
// tickets outrank in-progress ones.
func (s *Service) briefTickets(ctx context.Context, key string, view TicketListView) ([]domain.Ticket, error) {
	out := make([]domain.Ticket, 0, briefSectionLimit)
	cursor := ""
	for page := 0; page < briefPageScan && len(out) < briefSectionLimit; page++ {
		result, err := s.ListTickets(ctx, key, view, briefSectionLimit*3, cursor)
		if err != nil {
			return nil, err
		}
		for _, t := range result.Tickets {
			if t.Status == domain.WorkflowStatusDone || t.Status == domain.WorkflowStatusCancelled {
				continue
			}
			out = append(out, t)
			if len(out) == briefSectionLimit {
				break
			}
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	return out, nil
}

func (s *Service) briefFeatures(ctx context.Context, projectEntityID int64) ([]FeatureBriefRow, error) {
	rows, err := store.ListFeaturesForProject(ctx, s.store.DB(), projectEntityID)
	if err != nil {
		return nil, fmt.Errorf("service: list features: %w", err)
	}
	if len(rows) > briefSectionLimit {
		rows = rows[:briefSectionLimit]
	}
	counts, err := store.FeatureTicketCountsForProject(ctx, s.store.DB(), projectEntityID)
	if err != nil {
		return nil, fmt.Errorf("service: feature ticket counts: %w", err)
	}
	out := make([]FeatureBriefRow, len(rows))
	for i, r := range rows {
		c := counts[r.ID]
		out[i] = FeatureBriefRow{Feature: r.Entity, TicketsTotal: c.Total, TicketsDone: c.Done}
	}
	return out, nil
}
