package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// setupThreeTickets creates project ABC with three tickets (ABC-1,
// ABC-2, ABC-3) and returns their refs, for the relationship tests
// below that need a chain of distinct tickets.
func setupThreeTickets(t *testing.T, s *Service) (a, b, c domain.Reference) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	refs := make([]domain.Reference, 3)
	for i := range refs {
		ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
			ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T",
		}, testActor, testCorrelationID, "", "")
		if err != nil {
			t.Fatalf("create ticket %d: %v", i, err)
		}
		ref, err := domain.Parse(ticket.Ref)
		if err != nil {
			t.Fatalf("parse ref: %v", err)
		}
		refs[i] = ref
	}
	return refs[0], refs[1], refs[2]
}

// TestAddRelationshipRelatedToVisibleFromBothEnds is verification gate
// 6's related_to assertion: A↔B is stored once and shows up when
// listing either end's relationships.
func TestAddRelationshipRelatedToVisibleFromBothEnds(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	a, b, _ := setupThreeTickets(t, s)

	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipRelatedTo}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}

	aRels, err := s.GetTicketRelationships(ctx, a)
	if err != nil {
		t.Fatalf("GetTicketRelationships(a): %v", err)
	}
	if len(aRels) != 1 || aRels[0].Type != domain.RelationshipRelatedTo || aRels[0].Other.Seq != b.Seq {
		t.Fatalf("a's relationships = %+v, want one related_to -> b", aRels)
	}

	bRels, err := s.GetTicketRelationships(ctx, b)
	if err != nil {
		t.Fatalf("GetTicketRelationships(b): %v", err)
	}
	if len(bRels) != 1 || bRels[0].Type != domain.RelationshipRelatedTo || bRels[0].Other.Seq != a.Seq {
		t.Fatalf("b's relationships = %+v, want one related_to -> a", bRels)
	}
}

// TestAddRelationshipChildOfCanonicalizesToParentOf confirms a
// caller-supplied "view" type (child_of) is stored canonically
// (parent_of, endpoints swapped) and surfaces correctly from both
// ends: the child sees "parent_of -> parent" as child_of, per
// Inverse(); the parent sees the edge as stored, parent_of -> child.
func TestAddRelationshipChildOfCanonicalizesToParentOf(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	child, parent, _ := setupThreeTickets(t, s)

	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: child, TargetRef: parent, Type: domain.RelationshipChildOf}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}

	childRels, err := s.GetTicketRelationships(ctx, child)
	if err != nil {
		t.Fatalf("GetTicketRelationships(child): %v", err)
	}
	if len(childRels) != 1 || childRels[0].Type != domain.RelationshipChildOf || childRels[0].Other.Seq != parent.Seq {
		t.Fatalf("child's relationships = %+v, want one child_of -> parent", childRels)
	}

	parentRels, err := s.GetTicketRelationships(ctx, parent)
	if err != nil {
		t.Fatalf("GetTicketRelationships(parent): %v", err)
	}
	if len(parentRels) != 1 || parentRels[0].Type != domain.RelationshipParentOf || parentRels[0].Other.Seq != child.Seq {
		t.Fatalf("parent's relationships = %+v, want one parent_of -> child", parentRels)
	}
}

// TestAddRelationshipRejectsSelfLink covers verification gate 6's
// self-link case.
func TestAddRelationshipRejectsSelfLink(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	a, _, _ := setupThreeTickets(t, s)

	err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: a, Type: domain.RelationshipBlocks}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("AddRelationship self-link = %v, want validation_failed", err)
	}
}

// TestAddRelationshipRejectsDuplicate confirms a second identical add
// is already_exists, not a silent no-op or a second row.
func TestAddRelationshipRejectsDuplicate(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	a, b, _ := setupThreeTickets(t, s)

	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipBlocks}, testActor, testCorrelationID); err != nil {
		t.Fatalf("first AddRelationship: %v", err)
	}
	err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipBlocks}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrAlreadyExists {
		t.Fatalf("second AddRelationship = %v, want already_exists", err)
	}
}

// TestAddRelationshipDetectsBlocksCycle is verification gate 6's core
// case: A blocks B, B blocks C, then C blocks A must be rejected.
func TestAddRelationshipDetectsBlocksCycle(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	a, b, c := setupThreeTickets(t, s)

	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipBlocks}, testActor, testCorrelationID); err != nil {
		t.Fatalf("A blocks B: %v", err)
	}
	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: b, TargetRef: c, Type: domain.RelationshipBlocks}, testActor, testCorrelationID); err != nil {
		t.Fatalf("B blocks C: %v", err)
	}

	err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: c, TargetRef: a, Type: domain.RelationshipBlocks}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrRelationshipCycle {
		t.Fatalf("C blocks A = %v, want relationship_cycle", err)
	}
}

// TestAddRelationshipDetectsParentOfCycle mirrors the blocks case for
// parent_of, and additionally proves the cycle check sees through
// mixed parent_of/child_of input (both canonicalize to the same
// stored type before the walk).
func TestAddRelationshipDetectsParentOfCycle(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	a, b, c := setupThreeTickets(t, s)

	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipParentOf}, testActor, testCorrelationID); err != nil {
		t.Fatalf("A parent_of B: %v", err)
	}
	// Expressed as child_of instead of parent_of — must still
	// canonicalize into the same graph the cycle check walks.
	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: c, TargetRef: b, Type: domain.RelationshipChildOf}, testActor, testCorrelationID); err != nil {
		t.Fatalf("C child_of B: %v", err)
	}

	err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: c, Type: domain.RelationshipChildOf}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrRelationshipCycle {
		t.Fatalf("A child_of C = %v, want relationship_cycle", err)
	}
}

// TestRemoveRelationship confirms removal works from either end's
// caller-supplied order and that a second removal is not_found.
func TestRemoveRelationship(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	a, b, _ := setupThreeTickets(t, s)

	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipBlocks}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}
	if err := s.RemoveRelationship(ctx, RemoveRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipBlocks}, testActor, testCorrelationID); err != nil {
		t.Fatalf("RemoveRelationship: %v", err)
	}

	rels, err := s.GetTicketRelationships(ctx, a)
	if err != nil {
		t.Fatalf("GetTicketRelationships: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("relationships after removal = %+v, want none", rels)
	}

	err = s.RemoveRelationship(ctx, RemoveRelationshipRequest{SourceRef: a, TargetRef: b, Type: domain.RelationshipBlocks}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("second RemoveRelationship = %v, want not_found", err)
	}
}

// TestAddRelationshipEmitsAuditEvent confirms the audit event lands on
// the caller-supplied source ticket, even when canonicalization stores
// the edge with swapped endpoints (child_of).
func TestAddRelationshipEmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	child, parent, _ := setupThreeTickets(t, s)

	if err := s.AddRelationship(ctx, AddRelationshipRequest{SourceRef: child, TargetRef: parent, Type: domain.RelationshipChildOf}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}

	childTicket, err := s.GetTicket(ctx, child)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	entityID := mustEntityIDByUUID(t, s, childTicket.UUID)
	events, err := store.ListAuditEvents(ctx, s.store.DB(), entityID)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.EventType == eventRelationshipAdded {
			found = true
		}
	}
	if !found {
		t.Fatalf("child's audit events = %+v, want %q on the caller-supplied source", events, eventRelationshipAdded)
	}
}
