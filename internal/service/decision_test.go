package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateDecisionAllocatesReference(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	d, err := s.CreateDecision(ctx, CreateDecisionRequest{
		ProjectKey: "ABC", Title: "Use SQLite", Context: "We need a store", Decision: "Use SQLite", Rationale: "Simplicity",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	if d.Ref != "ABC-D1" {
		t.Errorf("Ref = %q, want ABC-D1", d.Ref)
	}
	if d.Status != domain.DecisionStatusProposed {
		t.Errorf("Status = %q, want proposed", d.Status)
	}
	if d.Title != "Use SQLite" || d.Context != "We need a store" || d.Decision != "Use SQLite" || d.Rationale != "Simplicity" {
		t.Errorf("decision = %+v, want all four text fields to round-trip", d)
	}
	if d.Version != 1 {
		t.Errorf("Version = %d, want 1", d.Version)
	}
}

func TestCreateDecisionRequiresTitle(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC"}, testActor, testCorrelationID, "", "")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "title" {
		t.Fatalf("CreateDecision with no title = %v, want validation_failed on field title", err)
	}
}

func TestCreateDecisionIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	fp, err := Fingerprint("POST", "/projects/ABC/decisions", []byte(`{"title":"Use SQLite"}`))
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	first, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use SQLite"}, testActor, testCorrelationID, "retry-key", fp)
	if err != nil {
		t.Fatalf("first CreateDecision: %v", err)
	}
	second, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use SQLite"}, testActor, testCorrelationID, "retry-key", fp)
	if err != nil {
		t.Fatalf("second CreateDecision: %v", err)
	}
	if first.Ref != second.Ref {
		t.Errorf("idempotent replay created two decisions: %v vs %v", first.Ref, second.Ref)
	}

	page, err := s.ListDecisions(ctx, "ABC", 10, "")
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(page.Decisions) != 1 {
		t.Errorf("decisions after replay = %d, want exactly 1", len(page.Decisions))
	}
}

func TestUpdateDecisionConditionalVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	d, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use SQLite"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	ref, err := domain.Parse(d.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	updated, err := s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: ref, Title: "Use SQLite (final)", Status: domain.DecisionStatusAccepted, ExpectedVersion: d.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}
	if updated.Title != "Use SQLite (final)" || updated.Status != domain.DecisionStatusAccepted || updated.Version != 2 {
		t.Errorf("updated decision = %+v, want title updated, status=accepted, version=2", updated)
	}

	_, err = s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: ref, Title: "Stale write", Status: domain.DecisionStatusAccepted, ExpectedVersion: d.Version,
	}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("UpdateDecision with a stale version = %v, want version_conflict", err)
	}
}

func TestListDecisionsPaginates(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	for _, title := range []string{"First", "Second", "Third"} {
		if _, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: title}, testActor, testCorrelationID, "", ""); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
	}

	page1, err := s.ListDecisions(ctx, "ABC", 2, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Decisions) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 decisions and a non-empty cursor", page1)
	}
	page2, err := s.ListDecisions(ctx, "ABC", 2, page1.NextCursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Decisions) != 1 || page2.NextCursor != "" {
		t.Fatalf("page2 = %+v, want 1 decision and no next cursor", page2)
	}
}

// TestTicketCanAssociateWithDecision proves the resolveAssociationEndpoint
// extension actually works end to end: a ticket can be associated with
// a decision, in either direction.
func TestTicketCanAssociateWithDecision(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	decision, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use SQLite"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	ticketRef, _ := domain.Parse(ticket.Ref)
	decisionRef, _ := domain.Parse(decision.Ref)

	if err := s.AddAssociation(ctx, AddAssociationRequest{SourceRef: ticketRef, TargetRef: decisionRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddAssociation ticket->decision: %v", err)
	}

	associated, err := s.GetAssociations(ctx, ticketRef)
	if err != nil {
		t.Fatalf("GetAssociations: %v", err)
	}
	found := false
	for _, a := range associated {
		if formatted, ferr := domain.Format(a); ferr == nil && formatted == decision.Ref {
			found = true
		}
	}
	if !found {
		t.Errorf("ticket associations = %+v, want it to include %s", associated, decision.Ref)
	}
}

// TestTicketDescriptionMentionsDecision proves the mentions.go
// extension: a #ABC-D1-style reference inside a ticket's description
// resolves to the decision, not a broken/ignored mention.
func TestTicketDescriptionMentionsDecision(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	decision, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use SQLite"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T", Description: "See #" + decision.Ref,
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ticketRef, _ := domain.Parse(ticket.Ref)

	mentions, err := s.GetTicketMentions(ctx, ticketRef)
	if err != nil {
		t.Fatalf("GetTicketMentions: %v", err)
	}
	found := false
	for _, m := range mentions {
		if formatted, ferr := domain.Format(m); ferr == nil && formatted == decision.Ref {
			found = true
		}
	}
	if !found {
		t.Errorf("ticket mentions = %+v, want it to include %s", mentions, decision.Ref)
	}
}
