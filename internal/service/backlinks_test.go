package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestGetBacklinksFromEntityBody confirms a backlink originating from
// a source entity's own Markdown body reports SourceCommentID == 0.
func TestGetBacklinksFromEntityBody(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	target := mustCreateTicket(t, s, "ABC", "Target")
	targetRef, err := domain.Parse(target.Ref)
	if err != nil {
		t.Fatalf("parse target ref: %v", err)
	}

	source, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Source", Description: "See " + target.Ref}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create source ticket: %v", err)
	}

	backlinks, err := s.GetBacklinks(ctx, targetRef)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(backlinks) != 1 {
		t.Fatalf("backlinks = %+v, want exactly 1", backlinks)
	}
	got := backlinks[0]
	gotRef, err := domain.Format(got.SourceRef)
	if err != nil {
		t.Fatalf("format source ref: %v", err)
	}
	if gotRef != source.Ref {
		t.Errorf("backlink source ref = %q, want %q", gotRef, source.Ref)
	}
	if got.SourceCommentID != 0 {
		t.Errorf("backlink SourceCommentID = %d, want 0 (own-body mention)", got.SourceCommentID)
	}
}

// TestGetBacklinksFromComment confirms a backlink originating from a
// comment reports the comment's id, distinguishing it from an
// own-body mention.
func TestGetBacklinksFromComment(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	target := mustCreateTicket(t, s, "ABC", "Target")
	targetRef, err := domain.Parse(target.Ref)
	if err != nil {
		t.Fatalf("parse target ref: %v", err)
	}
	host := mustCreateTicket(t, s, "ABC", "Host")
	hostRef, err := domain.Parse(host.Ref)
	if err != nil {
		t.Fatalf("parse host ref: %v", err)
	}

	c, err := s.AddComment(ctx, AddCommentRequest{Ref: hostRef, Body: "mentions " + target.Ref}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	backlinks, err := s.GetBacklinks(ctx, targetRef)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(backlinks) != 1 {
		t.Fatalf("backlinks = %+v, want exactly 1", backlinks)
	}
	got := backlinks[0]
	gotRef, err := domain.Format(got.SourceRef)
	if err != nil {
		t.Fatalf("format source ref: %v", err)
	}
	if gotRef != host.Ref {
		t.Errorf("backlink source ref = %q, want %q", gotRef, host.Ref)
	}
	if got.SourceCommentID != c.ID {
		t.Errorf("backlink SourceCommentID = %d, want %d (the comment's own id)", got.SourceCommentID, c.ID)
	}
}

// TestGetBacklinksExcludesTombstonedComment confirms a backlink from a
// since-deleted comment no longer surfaces, even though the comment
// row (and its now-empty derived_mentions edge — cleared by
// rescanMentions on delete) still exists as a tombstone.
func TestGetBacklinksExcludesTombstonedComment(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	target := mustCreateTicket(t, s, "ABC", "Target")
	targetRef, err := domain.Parse(target.Ref)
	if err != nil {
		t.Fatalf("parse target ref: %v", err)
	}
	host := mustCreateTicket(t, s, "ABC", "Host")
	hostRef, err := domain.Parse(host.Ref)
	if err != nil {
		t.Fatalf("parse host ref: %v", err)
	}

	c, err := s.AddComment(ctx, AddCommentRequest{Ref: hostRef, Body: "mentions " + target.Ref}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := s.DeleteComment(ctx, DeleteCommentRequest{CommentID: c.ID, ExpectedVersion: c.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}

	backlinks, err := s.GetBacklinks(ctx, targetRef)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(backlinks) != 0 {
		t.Fatalf("backlinks after comment delete = %+v, want none", backlinks)
	}
}

// TestGetBacklinksExcludesSoftDeletedSource confirms a backlink from a
// since-deleted source ticket no longer surfaces.
func TestGetBacklinksExcludesSoftDeletedSource(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	target := mustCreateTicket(t, s, "ABC", "Target")
	targetRef, err := domain.Parse(target.Ref)
	if err != nil {
		t.Fatalf("parse target ref: %v", err)
	}

	source, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Source", Description: "See " + target.Ref}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create source ticket: %v", err)
	}
	sourceRef, err := domain.Parse(source.Ref)
	if err != nil {
		t.Fatalf("parse source ref: %v", err)
	}
	if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: sourceRef, ExpectedVersion: source.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	backlinks, err := s.GetBacklinks(ctx, targetRef)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(backlinks) != 0 {
		t.Fatalf("backlinks after source delete = %+v, want none", backlinks)
	}
}

// TestGetBacklinksAcrossTargetKinds confirms backlinks work for a
// feature and a decision target, not just tickets.
func TestGetBacklinksAcrossTargetKinds(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create feature: %v", err)
	}
	featureRef, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "mentions the feature", Description: "See " + feature.Ref}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create ticket mentioning feature: %v", err)
	}
	featureBacklinks, err := s.GetBacklinks(ctx, featureRef)
	if err != nil {
		t.Fatalf("GetBacklinks(feature): %v", err)
	}
	if len(featureBacklinks) != 1 {
		t.Fatalf("feature backlinks = %+v, want 1", featureBacklinks)
	}

	decision, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use Postgres"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	decisionRef, err := domain.Parse(decision.Ref)
	if err != nil {
		t.Fatalf("parse decision ref: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "mentions the decision", Description: "See " + decision.Ref}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create ticket mentioning decision: %v", err)
	}
	decisionBacklinks, err := s.GetBacklinks(ctx, decisionRef)
	if err != nil {
		t.Fatalf("GetBacklinks(decision): %v", err)
	}
	if len(decisionBacklinks) != 1 {
		t.Fatalf("decision backlinks = %+v, want 1", decisionBacklinks)
	}
}
