package fixtures

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TestGenerateSmallIsDeterministic confirms the Phase 1 plan's core
// requirement: the same seed produces the same project keys, the same
// sample ticket reference, and the same row counts on two independent
// runs.
func TestGenerateSmallIsDeterministic(t *testing.T) {
	ctx := context.Background()

	st1, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open (1): %v", err)
	}
	defer func() { _ = st1.Close() }()
	sum1, err := Generate(ctx, st1, 42, Small)
	if err != nil {
		t.Fatalf("Generate (1): %v", err)
	}

	st2, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open (2): %v", err)
	}
	defer func() { _ = st2.Close() }()
	sum2, err := Generate(ctx, st2, 42, Small)
	if err != nil {
		t.Fatalf("Generate (2): %v", err)
	}

	if len(sum1.ProjectKeys) != len(sum2.ProjectKeys) {
		t.Fatalf("project count = %d vs %d", len(sum1.ProjectKeys), len(sum2.ProjectKeys))
	}
	for i := range sum1.ProjectKeys {
		if sum1.ProjectKeys[i] != sum2.ProjectKeys[i] {
			t.Errorf("project key %d = %q vs %q", i, sum1.ProjectKeys[i], sum2.ProjectKeys[i])
		}
	}
	if sum1.SampleTicketRef != sum2.SampleTicketRef {
		t.Errorf("SampleTicketRef = %q vs %q", sum1.SampleTicketRef, sum2.SampleTicketRef)
	}
	if sum1.TicketCount != sum2.TicketCount || sum1.CommentCount != sum2.CommentCount {
		t.Errorf("counts = (%d, %d) vs (%d, %d)", sum1.TicketCount, sum1.CommentCount, sum2.TicketCount, sum2.CommentCount)
	}

	// Timestamps must match too, not just counts/keys — pull the first
	// ticket's created_at from each store directly.
	var t1, t2 string
	if err := st1.DB().QueryRowContext(ctx, `SELECT created_at FROM entities ORDER BY id ASC LIMIT 1`).Scan(&t1); err != nil {
		t.Fatalf("query created_at (1): %v", err)
	}
	if err := st2.DB().QueryRowContext(ctx, `SELECT created_at FROM entities ORDER BY id ASC LIMIT 1`).Scan(&t2); err != nil {
		t.Fatalf("query created_at (2): %v", err)
	}
	if t1 != t2 {
		t.Errorf("first entity created_at = %q vs %q, want identical (deterministic clock)", t1, t2)
	}
}

// TestGenerateSmallRowCounts confirms the generator produces exactly
// the row counts Scale specifies — no off-by-one from batching.
func TestGenerateSmallRowCounts(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	sum, err := Generate(ctx, st, 7, Small)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	wantTickets := Small.Projects * Small.TicketsPerProject
	if sum.TicketCount != wantTickets {
		t.Errorf("Summary.TicketCount = %d, want %d", sum.TicketCount, wantTickets)
	}
	wantComments := wantTickets * Small.CommentsPerTicket
	if sum.CommentCount != wantComments {
		t.Errorf("Summary.CommentCount = %d, want %d", sum.CommentCount, wantComments)
	}

	var gotProjects, gotFeatures, gotTickets, gotComments, gotDecisions, gotPlans, gotDocuments int
	db := st.DB()
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&gotProjects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM features`).Scan(&gotFeatures); err != nil {
		t.Fatalf("count features: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets`).Scan(&gotTickets); err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments`).Scan(&gotComments); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM decisions`).Scan(&gotDecisions); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_items WHERE kind = 'plan'`).Scan(&gotPlans); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_items WHERE kind = 'document'`).Scan(&gotDocuments); err != nil {
		t.Fatalf("count documents: %v", err)
	}

	if gotProjects != Small.Projects {
		t.Errorf("projects rows = %d, want %d", gotProjects, Small.Projects)
	}
	if gotFeatures != Small.Projects*Small.FeaturesPerProject {
		t.Errorf("features rows = %d, want %d", gotFeatures, Small.Projects*Small.FeaturesPerProject)
	}
	if gotTickets != wantTickets {
		t.Errorf("tickets rows = %d, want %d", gotTickets, wantTickets)
	}
	if gotComments != wantComments {
		t.Errorf("comments rows = %d, want %d", gotComments, wantComments)
	}
	if wantDecisions := Small.Projects * Small.DecisionsPerProject; gotDecisions != wantDecisions || sum.DecisionCount != wantDecisions {
		t.Errorf("decisions rows = %d, Summary.DecisionCount = %d, want %d", gotDecisions, sum.DecisionCount, wantDecisions)
	}
	if wantPlans := Small.Projects * Small.PlansPerProject; gotPlans != wantPlans || sum.PlanCount != wantPlans {
		t.Errorf("plan rows = %d, Summary.PlanCount = %d, want %d", gotPlans, sum.PlanCount, wantPlans)
	}
	if wantDocuments := Small.Projects * Small.DocumentsPerProject; gotDocuments != wantDocuments || sum.DocumentCount != wantDocuments {
		t.Errorf("document rows = %d, Summary.DocumentCount = %d, want %d", gotDocuments, sum.DocumentCount, wantDocuments)
	}
}

// TestGenerateProducesQueryableData confirms the generated rows are
// actually valid and readable through internal/store's normal query
// paths, not just present — the sample ticket resolves, and the
// project's priority queue returns a full, correctly ordered page.
func TestGenerateProducesQueryableData(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	sum, err := Generate(ctx, st, 99, Small)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if sum.SampleTicketRef == "" {
		t.Fatal("SampleTicketRef is empty")
	}

	ref, err := domain.Parse(sum.SampleTicketRef)
	if err != nil {
		t.Fatalf("parse sample ref %q: %v", sum.SampleTicketRef, err)
	}
	row, err := store.GetTicketByRef(ctx, st.DB(), ref)
	if err != nil {
		t.Fatalf("GetTicketByRef(%s): %v", sum.SampleTicketRef, err)
	}
	if row.Entity.Ref != sum.SampleTicketRef {
		t.Errorf("resolved ticket ref = %q, want %q", row.Entity.Ref, sum.SampleTicketRef)
	}

	proj, err := store.GetProjectByKey(ctx, st.DB(), sum.SampleProjectKey)
	if err != nil {
		t.Fatalf("GetProjectByKey(%s): %v", sum.SampleProjectKey, err)
	}
	page, err := store.PriorityQueue(ctx, st.DB(), proj.ID, store.TicketFilters{}, Small.TicketsPerProject+1, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("PriorityQueue: %v", err)
	}
	if len(page.Tickets) != Small.TicketsPerProject {
		t.Fatalf("PriorityQueue returned %d tickets, want %d", len(page.Tickets), Small.TicketsPerProject)
	}
	for i := 1; i < len(page.Tickets); i++ {
		prev, cur := page.Tickets[i-1], page.Tickets[i]
		if prev.PriorityRank > cur.PriorityRank {
			t.Fatalf("PriorityQueue not sorted: index %d rank %d > index %d rank %d", i-1, prev.PriorityRank, i, cur.PriorityRank)
		}
	}
}
