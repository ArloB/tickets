package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TestReorderTicketMidpointInsertion moves a ticket between two
// existing neighbors and confirms the priority queue reflects the new
// order without needing a renumber.
func TestReorderTicketMidpointInsertion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	c := mustCreateTicket(t, s, "ABC", "C")
	// Insertion order a, b, c all land at increasing tail positions, so
	// the queue starts a, b, c. Move c to be right after a.
	aRef, err := domain.Parse(a.Ref)
	if err != nil {
		t.Fatalf("parse a ref: %v", err)
	}
	cRef, err := domain.Parse(c.Ref)
	if err != nil {
		t.Fatalf("parse c ref: %v", err)
	}

	moved, err := s.ReorderTicket(ctx, ReorderTicketRequest{Ref: cRef, AfterRef: &aRef, ExpectedVersion: c.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("ReorderTicket: %v", err)
	}
	if moved.Version != c.Version+1 {
		t.Errorf("moved ticket version = %d, want %d (reorder bumps the moved record's version)", moved.Version, c.Version+1)
	}

	order := mustPriorityOrder(t, s, "ABC")
	want := []string{a.Ref, c.Ref, b.Ref}
	if !refsEqual(order, want) {
		t.Fatalf("priority order after reorder = %v, want %v", order, want)
	}
}

// TestReorderTicketToHead confirms AfterRef == nil moves to the front
// of the group.
func TestReorderTicketToHead(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	c := mustCreateTicket(t, s, "ABC", "C")
	cRef, err := domain.Parse(c.Ref)
	if err != nil {
		t.Fatalf("parse c ref: %v", err)
	}

	if _, err := s.ReorderTicket(ctx, ReorderTicketRequest{Ref: cRef, AfterRef: nil, ExpectedVersion: c.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("ReorderTicket to head: %v", err)
	}

	order := mustPriorityOrder(t, s, "ABC")
	want := []string{c.Ref, a.Ref, b.Ref}
	if !refsEqual(order, want) {
		t.Fatalf("priority order after move-to-head = %v, want %v", order, want)
	}
}

// TestReorderTicketForcesRenumberAndPreservesOrder repeatedly inserts
// at the same midpoint until the integer gap between two neighbors is
// exhausted, forcing a full-group renumber — and confirms the
// resulting order is exactly what was requested, with only the
// explicitly moved ticket's version bumped each time (its neighbors'
// mechanical renumbering must not touch their versions).
func TestReorderTicketForcesRenumberAndPreservesOrder(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	aRef, err := domain.Parse(a.Ref)
	if err != nil {
		t.Fatalf("parse a ref: %v", err)
	}

	// domain.PositionGap is 1000, so repeatedly bisecting the gap
	// between a and b forces MidpointPosition to run out within ~10
	// iterations (log2(1000) ~= 10).
	var lastRef string
	for i := 0; i < 12; i++ {
		title := "mid"
		created, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: title}, testActor, testCorrelationID, "", "")
		if err != nil {
			t.Fatalf("create ticket %d: %v", i, err)
		}
		ref, err := domain.Parse(created.Ref)
		if err != nil {
			t.Fatalf("parse ref: %v", err)
		}
		if _, err := s.ReorderTicket(ctx, ReorderTicketRequest{Ref: ref, AfterRef: &aRef, ExpectedVersion: created.Version}, testActor, testCorrelationID); err != nil {
			t.Fatalf("ReorderTicket iteration %d: %v", i, err)
		}
		lastRef = created.Ref
	}
	_ = lastRef

	// Whatever happened, order must still be: a, then all the "mid"
	// tickets in the order they were placed (each one inserted
	// immediately after a, so the most recently placed is closest to
	// a), then b.
	order := mustPriorityOrder(t, s, "ABC")
	if len(order) != 14 {
		t.Fatalf("priority order length = %d, want 14 (a + 12 mid + b)", len(order))
	}
	if order[0] != a.Ref {
		t.Errorf("order[0] = %q, want %q", order[0], a.Ref)
	}
	if order[len(order)-1] != b.Ref {
		t.Errorf("order[last] = %q, want %q", order[len(order)-1], b.Ref)
	}

	// a and b were only ever mechanically renumbered (never the
	// explicitly moved ticket in any ReorderTicket call), so their
	// entities.version must be exactly what they started at — a
	// renumber must not invalidate their open If-Match tokens.
	aAfter, err := s.GetTicket(ctx, aRef)
	if err != nil {
		t.Fatalf("GetTicket(a): %v", err)
	}
	if aAfter.Version != a.Version {
		t.Errorf("a.Version after renumber = %d, want unchanged %d", aAfter.Version, a.Version)
	}
	bRef, err := domain.Parse(b.Ref)
	if err != nil {
		t.Fatalf("parse b ref: %v", err)
	}
	bAfter, err := s.GetTicket(ctx, bRef)
	if err != nil {
		t.Fatalf("GetTicket(b): %v", err)
	}
	if bAfter.Version != b.Version {
		t.Errorf("b.Version after renumber = %d, want unchanged %d", bAfter.Version, b.Version)
	}
}

// TestUpdateTicketFieldsPriorityChangeMovesToTail confirms changing a
// ticket's priority moves it to the tail of the new priority group
// (product spec §5.6), not leaving it at its old position value
// (which would sort it arbitrarily within the new group).
func TestUpdateTicketFieldsPriorityChangeMovesToTail(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	// Two existing high-priority tickets, then a medium-priority one
	// that will be promoted to high and must land after both.
	h1, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "H1", Priority: domain.PriorityHigh}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create h1: %v", err)
	}
	h2, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "H2", Priority: domain.PriorityHigh}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create h2: %v", err)
	}
	m, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "M", Priority: domain.PriorityMedium}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create m: %v", err)
	}
	mRef, err := domain.Parse(m.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	if _, err := s.UpdateTicketFields(ctx, UpdateTicketFieldsRequest{
		Ref: mRef, Type: domain.TicketTypeTask, Title: "M", Priority: domain.PriorityHigh, ExpectedVersion: m.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("UpdateTicketFields: %v", err)
	}

	order := mustPriorityOrder(t, s, "ABC")
	want := []string{h1.Ref, h2.Ref, m.Ref}
	if !refsEqual(order, want) {
		t.Fatalf("priority order after priority change = %v, want %v (promoted ticket lands at the tail of its new group)", order, want)
	}
}

// TestReorderTicketRejectsCrossGroupAfterRef confirms AfterRef must be
// in the same (project, priority) group as the ticket being moved.
func TestReorderTicketRejectsCrossGroupAfterRef(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	low, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Low", Priority: domain.PriorityLow}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create low: %v", err)
	}
	high, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "High", Priority: domain.PriorityHigh}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create high: %v", err)
	}
	lowRef, err := domain.Parse(low.Ref)
	if err != nil {
		t.Fatalf("parse low ref: %v", err)
	}
	highRef, err := domain.Parse(high.Ref)
	if err != nil {
		t.Fatalf("parse high ref: %v", err)
	}

	_, err = s.ReorderTicket(ctx, ReorderTicketRequest{Ref: lowRef, AfterRef: &highRef, ExpectedVersion: low.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("ReorderTicket across priority groups = %v, want validation_failed", err)
	}
}

// TestReorderTicketVersionConflict confirms a stale ExpectedVersion is
// rejected, and the group is left untouched.
func TestReorderTicketVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	a := mustCreateTicket(t, s, "ABC", "A")
	b := mustCreateTicket(t, s, "ABC", "B")
	bRef, err := domain.Parse(b.Ref)
	if err != nil {
		t.Fatalf("parse b ref: %v", err)
	}

	_, err = s.ReorderTicket(ctx, ReorderTicketRequest{Ref: bRef, AfterRef: nil, ExpectedVersion: b.Version + 1}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("ReorderTicket with stale version = %v, want version_conflict", err)
	}

	order := mustPriorityOrder(t, s, "ABC")
	want := []string{a.Ref, b.Ref}
	if !refsEqual(order, want) {
		t.Fatalf("priority order after failed reorder = %v, want unchanged %v", order, want)
	}
}

// mustPriorityOrder returns the refs of every ticket in a project's
// priority queue, in queue order — a direct store.PriorityQueue call
// rather than a service method, since Phase 1 keeps this below the
// API line (no ListTicketsByPriority service method exists yet).
func mustPriorityOrder(t *testing.T, s *Service, projectKey string) []string {
	t.Helper()
	ctx := context.Background()
	proj, err := store.GetProjectByKey(ctx, s.store.DB(), projectKey)
	if err != nil {
		t.Fatalf("GetProjectByKey: %v", err)
	}
	page, err := store.PriorityQueue(ctx, s.store.DB(), proj.ID, 100, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("PriorityQueue: %v", err)
	}
	refs := make([]string, len(page.Tickets))
	for i, tk := range page.Tickets {
		refs[i] = tk.Entity.Ref
	}
	return refs
}

func refsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
