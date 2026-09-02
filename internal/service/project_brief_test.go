package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestProjectBriefComposesEveryExpectedSection is this feature's core
// regression test: a project with an in-progress ticket, a done
// ticket, a bug ticket (issue register), a second feature, an accepted
// decision, a proposed decision, and a plan — the brief must surface
// the in-progress/bug tickets but not the done one, the accepted
// decision but not the proposed one, both features with correct
// ticket-progress counts, and the plan.
func TestProjectBriefComposesEveryExpectedSection(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Second feature"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create feature: %v", err)
	}

	inProgress, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "In progress ticket",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create in-progress ticket: %v", err)
	}
	if _, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
		Ref: mustParse(t, inProgress.Ref), NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: inProgress.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("mark ticket in progress: %v", err)
	}

	done, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Done ticket",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create done ticket: %v", err)
	}
	if _, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
		Ref: mustParse(t, done.Ref), NewStatus: domain.WorkflowStatusDone, ExpectedVersion: done.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("mark ticket done: %v", err)
	}

	sev := domain.SeverityHigh
	bug, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "A bug", Severity: &sev,
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create bug ticket: %v", err)
	}

	decision, err := s.CreateDecision(ctx, CreateDecisionRequest{
		ProjectKey: "ABC", Title: "An accepted decision", Decision: "do the thing",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	if _, err := s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: mustParse(t, decision.Ref), Title: decision.Title, Decision: decision.Decision,
		Status: domain.DecisionStatusAccepted, ExpectedVersion: decision.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("accept decision: %v", err)
	}
	if _, err := s.CreateDecision(ctx, CreateDecisionRequest{
		ProjectKey: "ABC", Title: "A proposed decision", Decision: "maybe do the thing",
	}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create proposed decision: %v", err)
	}

	if _, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindPlan, Title: "A plan", Representation: domain.ContentRepresentationMarkdown, Body: "the plan",
	}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	brief, err := s.ProjectBrief(ctx, "ABC")
	if err != nil {
		t.Fatalf("ProjectBrief: %v", err)
	}

	if brief.Project.Key != "ABC" {
		t.Errorf("brief.Project.Key = %q, want %q", brief.Project.Key, "ABC")
	}

	inProgressRefs := ticketRefs(brief.InProgress)
	if !briefContainsRef(inProgressRefs, inProgress.Ref) {
		t.Errorf("InProgress = %v, want it to contain %q", inProgressRefs, inProgress.Ref)
	}
	if briefContainsRef(inProgressRefs, done.Ref) {
		t.Errorf("InProgress = %v, want it to exclude the done ticket %q", inProgressRefs, done.Ref)
	}

	issueRefs := ticketRefs(brief.IssueRegister)
	if !briefContainsRef(issueRefs, bug.Ref) {
		t.Errorf("IssueRegister = %v, want it to contain the bug %q", issueRefs, bug.Ref)
	}

	if len(brief.Features) != 2 {
		t.Fatalf("len(Features) = %d, want 2 (General + %q)", len(brief.Features), feature.Ref)
	}
	var generalRow, secondRow *FeatureBriefRow
	for i := range brief.Features {
		switch brief.Features[i].Feature.Ref {
		case feature.Ref:
			secondRow = &brief.Features[i]
		default:
			generalRow = &brief.Features[i]
		}
	}
	if generalRow == nil || secondRow == nil {
		t.Fatalf("Features = %+v, want one row for %q and one for General", brief.Features, feature.Ref)
	}
	if generalRow.TicketsTotal != 3 || generalRow.TicketsDone != 1 {
		t.Errorf("General feature progress = %d/%d done, want 1/3 done", generalRow.TicketsDone, generalRow.TicketsTotal)
	}
	if secondRow.TicketsTotal != 0 {
		t.Errorf("second feature TicketsTotal = %d, want 0 (no tickets created in it)", secondRow.TicketsTotal)
	}

	decisionTitles := make([]string, len(brief.RecentDecisions))
	for i, d := range brief.RecentDecisions {
		decisionTitles[i] = d.Title
	}
	if len(brief.RecentDecisions) != 1 || brief.RecentDecisions[0].Title != "An accepted decision" {
		t.Errorf("RecentDecisions = %v, want exactly [%q]", decisionTitles, "An accepted decision")
	}

	if len(brief.RecentPlans) != 1 || brief.RecentPlans[0].Title != "A plan" {
		t.Errorf("RecentPlans = %+v, want exactly one plan titled %q", brief.RecentPlans, "A plan")
	}

	if len(brief.RecentActivity) == 0 {
		t.Error("RecentActivity is empty, want at least the creation events above")
	}
}

// TestProjectBriefInProgressSurvivesManyDoneTickets guards against a
// regression where PriorityQueue's ordering (priority_rank/position,
// not status) buries an in-progress ticket behind a wall of completed
// higher-priority ones and briefTickets's single-page over-fetch never
// reaches it.
func TestProjectBriefInProgressSurvivesManyDoneTickets(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	for i := 0; i < briefSectionLimit*3+5; i++ {
		done, err := s.CreateTicket(ctx, CreateTicketRequest{
			ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Done ticket", Priority: domain.PriorityHigh,
		}, testActor, testCorrelationID, "", "")
		if err != nil {
			t.Fatalf("create done ticket %d: %v", i, err)
		}
		if _, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
			Ref: mustParse(t, done.Ref), NewStatus: domain.WorkflowStatusDone, ExpectedVersion: done.Version,
		}, testActor, testCorrelationID); err != nil {
			t.Fatalf("mark ticket %d done: %v", i, err)
		}
	}

	inProgress, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "In progress ticket", Priority: domain.PriorityLow,
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create in-progress ticket: %v", err)
	}
	if _, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
		Ref: mustParse(t, inProgress.Ref), NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: inProgress.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("mark ticket in progress: %v", err)
	}

	brief, err := s.ProjectBrief(ctx, "ABC")
	if err != nil {
		t.Fatalf("ProjectBrief: %v", err)
	}
	if !briefContainsRef(ticketRefs(brief.InProgress), inProgress.Ref) {
		t.Errorf("InProgress = %v, want it to contain the low-priority in-progress ticket %q despite %d higher-priority done tickets",
			ticketRefs(brief.InProgress), inProgress.Ref, briefSectionLimit*3+5)
	}
}

// TestProjectBriefFeaturesSurviveManyDoneFeatures is the Features
// section's counterpart to TestProjectBriefInProgressSurvivesManyDoneTickets
// — the discoverability gap flagged as a foreseeable follow-on to ADR
// 0028: unlike briefTickets, briefFeatures had no done/cancelled
// exclusion or sort at all, so more than briefSectionLimit done
// features (a plausible bulk-migration shape) could fill the whole
// Features section and push an active feature out entirely.
// store.ListFeaturesForProject's done/cancelled-last sort fixes this.
func TestProjectBriefFeaturesSurviveManyDoneFeatures(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	for i := 0; i < briefSectionLimit+5; i++ {
		done, err := s.CreateFeature(ctx, CreateFeatureRequest{
			ProjectKey: "ABC", Title: "Done feature", Priority: domain.PriorityHigh,
		}, testActor, testCorrelationID)
		if err != nil {
			t.Fatalf("create done feature %d: %v", i, err)
		}
		if _, err := s.UpdateFeatureStatus(ctx, UpdateFeatureStatusRequest{
			Ref: mustParse(t, done.Ref), NewStatus: domain.WorkflowStatusDone, ExpectedVersion: done.Version,
		}, testActor, testCorrelationID); err != nil {
			t.Fatalf("mark feature %d done: %v", i, err)
		}
	}

	active, err := s.CreateFeature(ctx, CreateFeatureRequest{
		ProjectKey: "ABC", Title: "Active feature", Priority: domain.PriorityLow,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create active feature: %v", err)
	}

	brief, err := s.ProjectBrief(ctx, "ABC")
	if err != nil {
		t.Fatalf("ProjectBrief: %v", err)
	}
	var sawActive bool
	for _, f := range brief.Features {
		if f.Feature.Ref == active.Ref {
			sawActive = true
		}
	}
	if !sawActive {
		t.Errorf("brief.Features = %+v, want it to contain the low-priority backlog feature %q despite %d higher-priority done features",
			brief.Features, active.Ref, briefSectionLimit+5)
	}
}

func TestProjectBriefNotFound(t *testing.T) {
	s := newTestService(t)
	if _, err := s.ProjectBrief(context.Background(), "ZZZ"); err == nil {
		t.Fatal("ProjectBrief for a nonexistent project: want an error, got nil")
	}
}

func mustParse(t *testing.T, ref string) domain.Reference {
	t.Helper()
	r, err := domain.Parse(ref)
	if err != nil {
		t.Fatalf("parse ref %q: %v", ref, err)
	}
	return r
}

func ticketRefs(tickets []domain.Ticket) []string {
	out := make([]string, len(tickets))
	for i, t := range tickets {
		out[i] = t.Ref
	}
	return out
}

func briefContainsRef(refs []string, ref string) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}
