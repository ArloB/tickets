package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TestDeleteFeatureBlockedByDependents is verification gate 8's core
// case: a non-empty feature returns has_dependents by default.
func TestDeleteFeatureBlockedByDependents(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	featureRef, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	if _, err := s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: ticketRef, NewFeatureRef: featureRef, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("MoveTicketFeature: %v", err)
	}

	_, err = s.DeleteFeature(ctx, DeleteFeatureRequest{Ref: featureRef, Cascade: false, ExpectedVersion: feature.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrHasDependents {
		t.Fatalf("DeleteFeature without cascade = %v, want has_dependents", err)
	}

	// Feature must still exist, unaffected by the rejected attempt.
	if _, err := s.GetFeature(ctx, featureRef); err != nil {
		t.Fatalf("GetFeature after blocked delete: %v", err)
	}
}

// TestDeleteFeatureCascadeDeletesTicketsToo is gate 8's cascade case:
// with cascade, both the feature and its tickets are soft-deleted in
// one transaction, and both vanish from every read path.
func TestDeleteFeatureCascadeDeletesTicketsToo(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	featureRef, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	if _, err := s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: ticketRef, NewFeatureRef: featureRef, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("MoveTicketFeature: %v", err)
	}

	newVersion, err := s.DeleteFeature(ctx, DeleteFeatureRequest{Ref: featureRef, Cascade: true, ExpectedVersion: feature.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("DeleteFeature with cascade: %v", err)
	}
	if newVersion != feature.Version+1 {
		t.Errorf("DeleteFeature returned newVersion = %d, want %d", newVersion, feature.Version+1)
	}

	if _, err := s.GetFeature(ctx, featureRef); !isNotFound(err) {
		t.Errorf("GetFeature after cascade delete = %v, want not_found", err)
	}
	if _, err := s.GetTicket(ctx, ticketRef); !isNotFound(err) {
		t.Errorf("GetTicket after cascade delete = %v, want not_found", err)
	}

	features, err := s.ListFeatures(ctx, "ABC")
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	for _, f := range features {
		if f.Ref == feature.Ref {
			t.Errorf("ListFeatures still includes cascade-deleted feature %q", feature.Ref)
		}
	}
}

// TestDeleteGeneralFeatureRejected is gate 8's General-feature case
// (ADR 0001): it cannot be deleted regardless of dependents.
func TestDeleteGeneralFeatureRejected(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	generalRef := domain.Reference{ProjectKey: "ABC", Kind: domain.KindFeature, Seq: 1}
	general, err := s.GetFeature(ctx, generalRef)
	if err != nil {
		t.Fatalf("GetFeature(General): %v", err)
	}

	_, err = s.DeleteFeature(ctx, DeleteFeatureRequest{Ref: generalRef, Cascade: true, ExpectedVersion: general.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("DeleteFeature(General) = %v, want validation_failed", err)
	}
}

// TestDeleteTicketVanishesFromReadPaths covers gate 8's "deleted
// records vanish from every list, get" for a plain ticket delete
// (not via a feature cascade).
func TestDeleteTicketVanishesFromReadPaths(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	newVersion, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if newVersion != ticket.Version+1 {
		t.Errorf("DeleteTicket returned newVersion = %d, want %d", newVersion, ticket.Version+1)
	}

	if _, err := s.GetTicket(ctx, ref); !isNotFound(err) {
		t.Errorf("GetTicket after delete = %v, want not_found", err)
	}
	if _, err := s.ListComments(ctx, ref); !isNotFound(err) {
		t.Errorf("ListComments on deleted ticket = %v, want not_found", err)
	}
}

// TestDeleteTicketTwiceIsNotFound mirrors comment/relationship
// double-deletion semantics.
func TestDeleteTicketTwiceIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("first DeleteTicket: %v", err)
	}

	_, err = s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if !isNotFound(err) {
		t.Fatalf("second DeleteTicket = %v, want not_found", err)
	}
}

// TestRestoreTicketRoundTrip confirms a deleted ticket comes back and
// is indistinguishable from a normal one on every read path.
func TestRestoreTicketRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	newVersion, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	restored, err := s.RestoreTicket(ctx, RestoreTicketRequest{Ref: ref, ExpectedVersion: newVersion}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("RestoreTicket: %v", err)
	}
	if restored.DeletedAt != nil {
		t.Errorf("restored ticket DeletedAt = %v, want nil", restored.DeletedAt)
	}

	got, err := s.GetTicket(ctx, ref)
	if err != nil {
		t.Fatalf("GetTicket after restore: %v", err)
	}
	if got.Ref != ticket.Ref {
		t.Errorf("GetTicket after restore = %+v, want ref %q", got, ticket.Ref)
	}
}

// TestRestoreTicketRefusesWhenFeatureDeleted is the plan's explicit
// decision: a cascade-deleted ticket cannot be restored on its own —
// the feature must be restored first.
func TestRestoreTicketRefusesWhenFeatureDeleted(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	featureRef, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse feature ref: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	if _, err := s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: ticketRef, NewFeatureRef: featureRef, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("MoveTicketFeature: %v", err)
	}
	featureNewVersion, err := s.DeleteFeature(ctx, DeleteFeatureRequest{Ref: featureRef, Cascade: true, ExpectedVersion: feature.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("DeleteFeature cascade: %v", err)
	}
	if featureNewVersion != feature.Version+1 {
		t.Errorf("DeleteFeature cascade returned newVersion = %d, want %d", featureNewVersion, feature.Version+1)
	}
	// Cascade-deleted tickets go through
	// store.SoftDeleteEntityUnconditional, which — unlike DeleteTicket's
	// own store.SoftDeleteEntity — has no expected/new version to hand
	// back to a caller (DeleteFeature's doc comment explains why). Read
	// the version straight from the store rather than hand-computing it
	// from an arithmetic assumption about how many writes the cascade
	// made.
	deletedTicketRow, err := store.GetTicketByRefAnyDeletion(ctx, s.store.DB(), ticketRef)
	if err != nil {
		t.Fatalf("GetTicketByRefAnyDeletion after cascade: %v", err)
	}
	ticketVersionAfterCascade := deletedTicketRow.Entity.Version

	// Restoring the ticket alone must fail — its feature is deleted.
	_, err = s.RestoreTicket(ctx, RestoreTicketRequest{Ref: ticketRef, ExpectedVersion: ticketVersionAfterCascade}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("RestoreTicket with deleted feature = %v, want validation_failed", err)
	}

	// Restore the feature first...
	restoredFeature, err := s.RestoreFeature(ctx, RestoreFeatureRequest{Ref: featureRef, ExpectedVersion: featureNewVersion}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("RestoreFeature: %v", err)
	}
	if restoredFeature.DeletedAt != nil {
		t.Errorf("restored feature DeletedAt = %v, want nil", restoredFeature.DeletedAt)
	}

	// ...but the ticket stays deleted (no auto-restore of dependents).
	if _, err := s.GetTicket(ctx, ticketRef); !isNotFound(err) {
		t.Errorf("GetTicket after feature-only restore = %v, want still not_found", err)
	}

	// Now restoring the ticket succeeds.
	if _, err := s.RestoreTicket(ctx, RestoreTicketRequest{Ref: ticketRef, ExpectedVersion: ticketVersionAfterCascade}, testActor, testCorrelationID); err != nil {
		t.Fatalf("RestoreTicket after feature restored: %v", err)
	}
	if _, err := s.GetTicket(ctx, ticketRef); err != nil {
		t.Fatalf("GetTicket after ticket restored: %v", err)
	}
}

// TestRestoreNotDeletedIsValidationError confirms restoring a live
// ticket is rejected rather than treated as a silent no-op.
func TestRestoreNotDeletedIsValidationError(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	_, err = s.RestoreTicket(ctx, RestoreTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("RestoreTicket on a live ticket = %v, want validation_failed", err)
	}
}

// TestDeleteTicketVersionConflict and TestRestoreTicketVersionConflict
// confirm both mutations are version-guarded like every other
// mutation in this package.
func TestDeleteTicketVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	_, err = s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version + 1}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("DeleteTicket with stale version = %v, want version_conflict", err)
	}
}

func TestRestoreTicketVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	_, err = s.RestoreTicket(ctx, RestoreTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("RestoreTicket with stale version = %v, want version_conflict", err)
	}
}

func isNotFound(err error) bool {
	var svcErr *Error
	return errors.As(err, &svcErr) && svcErr.Code == domain.ErrNotFound
}
