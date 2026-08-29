package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func resolvedByRef(t *testing.T, got []ResolvedRef, ref string) ResolvedRef {
	t.Helper()
	for _, r := range got {
		if r.Ref == ref {
			return r
		}
	}
	t.Fatalf("no entry for %q in %+v", ref, got)
	return ResolvedRef{}
}

// TestResolveRefsAcrossKinds covers the whole reference vocabulary in
// one pass: the five seq-numbered kinds plus a bare project key all
// resolve with their titles, and a status comes back only for the
// kinds that have one.
func TestResolveRefsAcrossKinds(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "Fix the parser")
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Parser overhaul"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	decision, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Use a PEG parser"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Rollout plan", Body: "Do the thing",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(plan): %v", err)
	}
	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Reference notes", Body: "Some notes",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(document): %v", err)
	}

	got, err := s.ResolveRefs(ctx, []string{ticket.Ref, feature.Ref, decision.Ref, plan.Ref, doc.Ref, "ABC"})
	if err != nil {
		t.Fatalf("ResolveRefs: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("len(got) = %d, want 6: %+v", len(got), got)
	}

	for _, want := range []struct{ ref, kind, title string }{
		{ticket.Ref, "ticket", "Fix the parser"},
		{feature.Ref, "feature", "Parser overhaul"},
		{decision.Ref, "decision", "Use a PEG parser"},
		{plan.Ref, "plan", "Rollout plan"},
		{doc.Ref, "document", "Reference notes"},
		{"ABC", "project", "Example"},
	} {
		r := resolvedByRef(t, got, want.ref)
		if !r.Exists || r.Kind != want.kind || r.Title != want.title {
			t.Errorf("resolve %q = %+v, want exists with kind %q and title %q", want.ref, r, want.kind, want.title)
		}
	}

	if r := resolvedByRef(t, got, ticket.Ref); r.Status == "" {
		t.Errorf("ticket status = %q, want a workflow status", r.Status)
	}
	if r := resolvedByRef(t, got, plan.Ref); r.Status != "" {
		t.Errorf("plan status = %q, want empty — plans have no status", r.Status)
	}
}

// TestResolveRefsUnresolvable is the best-effort half of the
// contract: every shape a scan over prose can hand this method that
// names nothing comes back exists=false, never an error.
func TestResolveRefsUnresolvable(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	got, err := s.ResolveRefs(ctx, []string{"ABC-99", "ABC-F99", "ABC-P99", "ZZZ-1", "ZZZ", "ABC-X1", "not-a-ref"})
	if err != nil {
		t.Fatalf("ResolveRefs: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("len(got) = %d, want 7: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Exists {
			t.Errorf("resolve %q = %+v, want exists=false", r.Ref, r)
		}
	}
}

// TestResolveRefsDeduplicatesInOrder lets a caller pass a raw scan
// without pre-filtering, which is the point of the guarantee.
func TestResolveRefsDeduplicatesInOrder(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	mustCreateTicket(t, s, "ABC", "One")
	mustCreateTicket(t, s, "ABC", "Two")

	got, err := s.ResolveRefs(ctx, []string{"ABC-2", "ABC-1", "ABC-2", "ABC-1"})
	if err != nil {
		t.Fatalf("ResolveRefs: %v", err)
	}
	if len(got) != 2 || got[0].Ref != "ABC-2" || got[1].Ref != "ABC-1" {
		t.Fatalf("got = %+v, want exactly ABC-2 then ABC-1", got)
	}
}

// TestResolveRefsRejectsOversizedBatch guards maxResolveRefs: one
// request must not fan out into an unbounded number of point reads.
func TestResolveRefsRejectsOversizedBatch(t *testing.T) {
	s := newTestService(t)
	refs := make([]string, maxResolveRefs+1)
	for i := range refs {
		refs[i] = "ABC-1"
	}
	_, err := s.ResolveRefs(context.Background(), refs)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "refs" {
		t.Fatalf("ResolveRefs(oversized) error = %v, want a validation error on field \"refs\"", err)
	}
}

// TestResolveRefsExcludesDeleted keeps a link from being rendered to
// a record that would 404 on the way out.
func TestResolveRefsExcludesDeleted(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "Doomed")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}

	got, err := s.ResolveRefs(ctx, []string{ticket.Ref})
	if err != nil {
		t.Fatalf("ResolveRefs: %v", err)
	}
	if len(got) != 1 || got[0].Exists {
		t.Fatalf("got = %+v, want the deleted ticket unresolved", got)
	}
}
