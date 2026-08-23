package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestListFeaturesFilteredByStatusAndPriority confirms feature filters
// narrow the result and compose with AND. Every feature in this
// service test suite is 'backlog' status (no feature status-update
// endpoint exists yet — Phase 4's kanban write-flow work adds one), so
// this exercises status as a match-everything filter narrowed further
// by priority, rather than status doing the narrowing itself.
func TestListFeaturesFilteredByStatusAndPriority(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	// ABC-F1 (General) already exists at priority=medium, status=backlog.

	critical1, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Critical", Priority: domain.PriorityCritical}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create critical feature: %v", err)
	}
	critical2, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Also critical", Priority: domain.PriorityCritical}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create second critical feature: %v", err)
	}

	page, err := s.ListFeaturesFiltered(ctx, "ABC", 20, "", FeatureListFilters{
		Priority: domain.PriorityCritical, Status: domain.WorkflowStatusBacklog,
	})
	if err != nil {
		t.Fatalf("ListFeaturesFiltered(priority+status): %v", err)
	}
	if len(page.Features) != 2 {
		t.Fatalf("filtered page = %+v, want exactly the 2 critical features (General is medium priority, excluded)", page.Features)
	}
	gotRefs := map[string]bool{page.Features[0].Ref: true, page.Features[1].Ref: true}
	if !gotRefs[critical1.Ref] || !gotRefs[critical2.Ref] {
		t.Fatalf("filtered page refs = %v, want [%s %s]", gotRefs, critical1.Ref, critical2.Ref)
	}
}

// TestListFeaturesFilteredInvalidEnum mirrors the ticket-filter
// version: an invalid status/priority value is validation_failed on
// the offending field.
func TestListFeaturesFilteredInvalidEnum(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	cases := []struct {
		name    string
		filters FeatureListFilters
		field   string
	}{
		{"status", FeatureListFilters{Status: "bogus"}, "status"},
		{"priority", FeatureListFilters{Priority: "bogus"}, "priority"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.ListFeaturesFiltered(ctx, "ABC", 20, "", c.filters)
			var svcErr *Error
			if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != c.field {
				t.Fatalf("ListFeaturesFiltered(%s=bogus) error = %v, want validation_failed on field %s", c.name, err, c.field)
			}
		})
	}
}

// TestListFeaturesFilteredByCreator confirms the creator filter
// resolves the actor ref and matches — every feature in this test
// suite is created by testActor, so this exercises the resolve path
// end to end even though it can't exercise "excludes a non-matching
// creator" without a second actor identity, which Phase 2's HTTP
// surface still has no self-serve way to create in a plain service
// test (mustCreateNonAdminHuman-equivalent lives in httpapi's test
// package, not here).
func TestListFeaturesFilteredByCreator(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	page, err := s.ListFeaturesFiltered(ctx, "ABC", 20, "", FeatureListFilters{Creator: testActor.String()})
	if err != nil {
		t.Fatalf("ListFeaturesFiltered(creator): %v", err)
	}
	if len(page.Features) != 1 || page.Features[0].Ref != "ABC-F1" {
		t.Fatalf("filtered page = %+v, want exactly the General feature ABC-F1", page.Features)
	}
}

// TestListFeaturesUnfilteredStillWorks confirms ListFeatures (the
// existing, unfiltered entry point) is unaffected by
// ListFeaturesFiltered's addition.
func TestListFeaturesUnfilteredStillWorks(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	page, err := s.ListFeatures(ctx, "ABC", 20, "")
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	if len(page.Features) != 1 || page.Features[0].Ref != "ABC-F1" {
		t.Fatalf("ListFeatures = %+v, want exactly the General feature ABC-F1", page.Features)
	}
}
