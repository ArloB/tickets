package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestExternalLinkLifecycle exercises add/list/remove for a ticket,
// the entity kind every other test in this file leaves to the
// cross-kind test below.
func TestExternalLinkLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	link, err := s.AddExternalLink(ctx, AddExternalLinkRequest{Ref: ref, Title: "Design doc", URL: "https://example.com/design"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("AddExternalLink: %v", err)
	}
	if link.ID == 0 || link.Title != "Design doc" || link.URL != "https://example.com/design" {
		t.Fatalf("unexpected link: %+v", link)
	}

	links, err := s.GetExternalLinks(ctx, ref)
	if err != nil {
		t.Fatalf("GetExternalLinks: %v", err)
	}
	if len(links) != 1 || links[0].ID != link.ID {
		t.Fatalf("GetExternalLinks = %+v, want exactly the one link just added", links)
	}

	if err := s.RemoveExternalLink(ctx, RemoveExternalLinkRequest{Ref: ref, LinkID: link.ID}, testActor, testCorrelationID); err != nil {
		t.Fatalf("RemoveExternalLink: %v", err)
	}
	after, err := s.GetExternalLinks(ctx, ref)
	if err != nil {
		t.Fatalf("GetExternalLinks after remove: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("GetExternalLinks after remove = %+v, want none", after)
	}
}

// TestExternalLinkAcrossEntityKinds confirms features and decisions
// can also carry links, not just tickets.
func TestExternalLinkAcrossEntityKinds(t *testing.T) {
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
	if _, err := s.AddExternalLink(ctx, AddExternalLinkRequest{Ref: featureRef, Title: "Spec", URL: "https://example.com/spec"}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddExternalLink(feature): %v", err)
	}
	featureLinks, err := s.GetExternalLinks(ctx, featureRef)
	if err != nil {
		t.Fatalf("GetExternalLinks(feature): %v", err)
	}
	if len(featureLinks) != 1 {
		t.Fatalf("feature links = %+v, want 1", featureLinks)
	}

	decision, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use Postgres"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	decisionRef, err := domain.Parse(decision.Ref)
	if err != nil {
		t.Fatalf("parse decision ref: %v", err)
	}
	if _, err := s.AddExternalLink(ctx, AddExternalLinkRequest{Ref: decisionRef, Title: "RFC", URL: "https://example.com/rfc"}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddExternalLink(decision): %v", err)
	}
	decisionLinks, err := s.GetExternalLinks(ctx, decisionRef)
	if err != nil {
		t.Fatalf("GetExternalLinks(decision): %v", err)
	}
	if len(decisionLinks) != 1 {
		t.Fatalf("decision links = %+v, want 1", decisionLinks)
	}
}

// TestExternalLinkRejectsUnsupportedEntityKind confirms a
// syntactically valid reference to an unsupported kind (e.g. a
// project) is a clean validation error, not a 500 — mirroring
// resolveAssociationEndpoint's own default case.
func TestExternalLinkRejectsUnsupportedEntityKind(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.AddExternalLink(ctx, AddExternalLinkRequest{Ref: domain.Reference{ProjectKey: "ABC", Kind: domain.KindProject}, Title: "T", URL: "https://example.com"}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("AddExternalLink on a project ref = %v, want validation_failed", err)
	}
}

// TestExternalLinkRejectsDisallowedURLScheme confirms
// domain.ValidateLinkURL is actually enforced on the write path, not
// just unit-tested in isolation — javascript:/data: URLs rendered as
// clickable links are a stored-XSS vector (product spec §10).
func TestExternalLinkRejectsDisallowedURLScheme(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	for _, bad := range []string{"javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "not-a-url", ""} {
		_, err := s.AddExternalLink(ctx, AddExternalLinkRequest{Ref: ref, Title: "T", URL: bad}, testActor, testCorrelationID)
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "url" {
			t.Errorf("AddExternalLink(url=%q) error = %v, want validation_failed on field url", bad, err)
		}
	}
}

// TestExternalLinkRejectsEmptyTitle confirms title, like every other
// entity's title field in this codebase, cannot be blank.
func TestExternalLinkRejectsEmptyTitle(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	_, err = s.AddExternalLink(ctx, AddExternalLinkRequest{Ref: ref, Title: "   ", URL: "https://example.com"}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "title" {
		t.Fatalf("AddExternalLink(blank title) error = %v, want validation_failed on field title", err)
	}
}

// TestRemoveExternalLinkScopedToEntity confirms a link id from a
// different entity cannot be deleted through another entity's ref —
// DeleteExternalLink's (entityID, linkID) scoping, exercised through
// the service layer.
func TestRemoveExternalLinkScopedToEntity(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	t1, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T1"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create t1: %v", err)
	}
	t2, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T2"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create t2: %v", err)
	}
	ref1, _ := domain.Parse(t1.Ref)
	ref2, _ := domain.Parse(t2.Ref)

	link, err := s.AddExternalLink(ctx, AddExternalLinkRequest{Ref: ref1, Title: "T", URL: "https://example.com"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("AddExternalLink: %v", err)
	}

	err = s.RemoveExternalLink(ctx, RemoveExternalLinkRequest{Ref: ref2, LinkID: link.ID}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("remove t1's link via t2's ref: error = %v, want not_found", err)
	}

	links, err := s.GetExternalLinks(ctx, ref1)
	if err != nil {
		t.Fatalf("GetExternalLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("t1's links after the cross-entity delete attempt = %+v, want the link to still be there", links)
	}
}
