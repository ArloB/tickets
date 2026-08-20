package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestAddAssociationVisibleFromBothEnds confirms an associated_with
// edge is symmetric — stored once, visible from either endpoint,
// mirroring TestAddRelationshipRelatedToVisibleFromBothEnds.
func TestAddAssociationVisibleFromBothEnds(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	aRef, err := domain.Parse(a.Ref)
	if err != nil {
		t.Fatalf("parse a ref: %v", err)
	}
	bRef, err := domain.Parse(b.Ref)
	if err != nil {
		t.Fatalf("parse b ref: %v", err)
	}

	if err := s.AddAssociation(ctx, AddAssociationRequest{SourceRef: aRef, TargetRef: bRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddAssociation: %v", err)
	}

	aAssocs, err := s.GetAssociations(ctx, aRef)
	if err != nil {
		t.Fatalf("GetAssociations(a): %v", err)
	}
	if !containsRef(t, aAssocs, b.Ref) {
		t.Fatalf("a's associations = %+v, want %q", aAssocs, b.Ref)
	}

	bAssocs, err := s.GetAssociations(ctx, bRef)
	if err != nil {
		t.Fatalf("GetAssociations(b): %v", err)
	}
	if !containsRef(t, bAssocs, a.Ref) {
		t.Fatalf("b's associations = %+v, want %q", bAssocs, a.Ref)
	}
}

// TestAddAssociationTicketAndFeature confirms associations connect
// mixed entity kinds — a ticket and a feature — not just two tickets.
func TestAddAssociationTicketAndFeature(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	featureRef, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}

	if err := s.AddAssociation(ctx, AddAssociationRequest{SourceRef: ticketRef, TargetRef: featureRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddAssociation: %v", err)
	}

	assocs, err := s.GetAssociations(ctx, ticketRef)
	if err != nil {
		t.Fatalf("GetAssociations: %v", err)
	}
	if !containsRef(t, assocs, feature.Ref) {
		t.Fatalf("ticket's associations = %+v, want %q", assocs, feature.Ref)
	}
}

// TestAddAssociationRejectsSelfLink and
// TestAddAssociationRejectsDuplicate mirror the equivalent relationship
// tests.
func TestAddAssociationRejectsSelfLink(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	aRef, err := domain.Parse(a.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	err = s.AddAssociation(ctx, AddAssociationRequest{SourceRef: aRef, TargetRef: aRef}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("AddAssociation self-link = %v, want validation_failed", err)
	}
}

func TestAddAssociationRejectsDuplicate(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	aRef, err := domain.Parse(a.Ref)
	if err != nil {
		t.Fatalf("parse a ref: %v", err)
	}
	bRef, err := domain.Parse(b.Ref)
	if err != nil {
		t.Fatalf("parse b ref: %v", err)
	}

	if err := s.AddAssociation(ctx, AddAssociationRequest{SourceRef: aRef, TargetRef: bRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("first AddAssociation: %v", err)
	}
	// Reversed endpoints must still collide — the edge is symmetric.
	err = s.AddAssociation(ctx, AddAssociationRequest{SourceRef: bRef, TargetRef: aRef}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrAlreadyExists {
		t.Fatalf("reversed duplicate AddAssociation = %v, want already_exists", err)
	}
}

// TestRemoveAssociation confirms removal works and a second removal
// is not_found.
func TestRemoveAssociation(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	aRef, err := domain.Parse(a.Ref)
	if err != nil {
		t.Fatalf("parse a ref: %v", err)
	}
	bRef, err := domain.Parse(b.Ref)
	if err != nil {
		t.Fatalf("parse b ref: %v", err)
	}

	if err := s.AddAssociation(ctx, AddAssociationRequest{SourceRef: aRef, TargetRef: bRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddAssociation: %v", err)
	}
	if err := s.RemoveAssociation(ctx, RemoveAssociationRequest{SourceRef: bRef, TargetRef: aRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("RemoveAssociation: %v", err)
	}

	assocs, err := s.GetAssociations(ctx, aRef)
	if err != nil {
		t.Fatalf("GetAssociations: %v", err)
	}
	if len(assocs) != 0 {
		t.Fatalf("associations after removal = %+v, want none", assocs)
	}

	err = s.RemoveAssociation(ctx, RemoveAssociationRequest{SourceRef: aRef, TargetRef: bRef}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("second RemoveAssociation = %v, want not_found", err)
	}
}

// TestGetAssociationsHidesSoftDeletedEndpoint mirrors
// TestGetTicketRelationshipsHidesSoftDeletedEndpoint for associations.
func TestGetAssociationsHidesSoftDeletedEndpoint(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	aRef, err := domain.Parse(a.Ref)
	if err != nil {
		t.Fatalf("parse a ref: %v", err)
	}
	bRef, err := domain.Parse(b.Ref)
	if err != nil {
		t.Fatalf("parse b ref: %v", err)
	}
	if err := s.AddAssociation(ctx, AddAssociationRequest{SourceRef: aRef, TargetRef: bRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddAssociation: %v", err)
	}

	if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: bRef, ExpectedVersion: b.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteTicket(b): %v", err)
	}

	assocs, err := s.GetAssociations(ctx, aRef)
	if err != nil {
		t.Fatalf("GetAssociations(a) after b deleted: %v", err)
	}
	if len(assocs) != 0 {
		t.Fatalf("associations after b's soft-delete = %+v, want none", assocs)
	}
}
