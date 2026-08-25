// This file is an external test package (store_test, not store)
// specifically so it can import internal/fixtures, which itself
// imports internal/store — package store's own (internal, white-box)
// test files can't do that without an import cycle. Benchmark
// functions are never invoked by a plain `go test` run (only
// `-bench`), so the lazy full-scale fixture below costs nothing for
// `task ci`; only `task bench` ever builds it.
package store_test

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/fixtures"
	"github.com/ArloB/tickets/internal/store"
)

// benchP95 times each of b.N calls to fn individually and reports two
// custom metrics: "p95-ms/op" (the 95th-percentile latency — product
// spec §11 states all three numeric latency targets as p95, not the
// mean Go's built-in ns/op reports) and "first-iter-ms/op" (this
// call's first iteration).
//
// first-iter-ms/op is deliberately NOT labeled "cold": Go's benchmark
// runner re-invokes the whole Benchmark function, calibration passes
// included, before settling on the final b.N — so durations[0] here
// is preceded by every one of those calibration passes and is not the
// literal first call the process ever makes. Measured across several
// runs it moved by as much as 4x run to run and was sometimes *faster*
// than the reported p95, which a real cold start (slower than steady
// state, by definition) never would be — proof it's one noisy sample,
// not a cold-start measurement. Reported anyway as a single-sample
// sanity check, not as evidence toward §11's cold/warm requirement;
// docs/benchmarks.md's "Warm/cold state" section is what actually
// satisfies that (plan.md:501) by defining terms this harness can
// measure honestly and stating what it can't. Applied only to the
// three benchmarks that back §11's three targets
// (BenchmarkGetTicketByRef, BenchmarkSearchFirstPage here, and
// service.BenchmarkCreateTicket), not all thirteen — a full p95
// harness for every benchmark was judged disproportionate to what §11
// asks for.
func benchP95(b *testing.B, fn func()) {
	b.Helper()
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		fn()
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	if len(durations) > 0 {
		b.ReportMetric(float64(durations[0].Microseconds())/1000, "first-iter-ms/op")
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	idx := int(float64(len(durations)) * 0.95)
	if idx >= len(durations) {
		idx = len(durations) - 1
	}
	b.ReportMetric(float64(durations[idx].Microseconds())/1000, "p95-ms/op")
}

var (
	fullFixtureOnce    sync.Once
	fullFixtureStore   *store.Store
	fullFixtureSummary fixtures.Summary
	fullFixtureDir     string
	fullFixtureErr     error
)

// fullFixture lazily builds product spec §11's reference dataset
// (fixtures.Full: 25 projects / 100,000 tickets / 500,000 comments)
// once per test binary process, shared across every benchmark in this
// file — regenerating it per-benchmark would multiply an already
// ~20s build by however many Benchmark functions a `-bench=.` run
// selects.
func fullFixture(b *testing.B) (*store.Store, fixtures.Summary) {
	b.Helper()
	fullFixtureOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tickets-bench-*")
		if err != nil {
			fullFixtureErr = err
			return
		}
		fullFixtureDir = dir
		st, err := store.Open(dir)
		if err != nil {
			fullFixtureErr = err
			return
		}
		sum, err := fixtures.Generate(context.Background(), st, 42, fixtures.Full)
		if err != nil {
			fullFixtureErr = err
			return
		}
		// RebuildSearchIndex must run inside one transaction (its own
		// doc comment) — both real callers (cmd/tickets/admin.go,
		// internal/backup/import.go) wrap it in a tx. Passing st.DB()
		// directly would turn every one of ~610k UpsertSearchDocument
		// calls into its own autocommit transaction, which is not
		// representative of any real code path and is catastrophically
		// slower than the transactional form this benchmark measures.
		tx, err := st.DB().BeginTx(context.Background(), nil)
		if err != nil {
			fullFixtureErr = err
			return
		}
		if _, err := store.RebuildSearchIndex(context.Background(), tx); err != nil {
			_ = tx.Rollback()
			fullFixtureErr = err
			return
		}
		if err := tx.Commit(); err != nil {
			fullFixtureErr = err
			return
		}
		fullFixtureStore = st
		fullFixtureSummary = sum
	})
	if fullFixtureErr != nil {
		b.Fatalf("build full-scale fixture: %v", fullFixtureErr)
	}
	return fullFixtureStore, fullFixtureSummary
}

// TestMain owns cleanup of the shared fixture's temp directory —
// b.TempDir() isn't usable here since its cleanup is tied to whichever
// single benchmark first created it, and every other benchmark in this
// file needs the store to stay open for the rest of the run.
func TestMain(m *testing.M) {
	code := m.Run()
	if fullFixtureStore != nil {
		_ = fullFixtureStore.Close()
	}
	if fullFixtureDir != "" {
		_ = os.RemoveAll(fullFixtureDir)
	}
	os.Exit(code)
}

// BenchmarkGetTicketByRef is product spec §11's "indexed detail
// fetch" target (p95 < 100ms at the reference dataset scale). It's
// one of the three benchmarks (alongside BenchmarkSearchFirstPage and
// service.BenchmarkCreateTicket) that additionally reports a real p95
// via benchP95 — §11 states all three numeric targets as p95 latency,
// not mean, and Go's built-in ns/op is a mean.
func BenchmarkGetTicketByRef(b *testing.B) {
	st, sum := fullFixture(b)
	ref, err := domain.Parse(sum.SampleTicketRef)
	if err != nil {
		b.Fatalf("parse sample ref: %v", err)
	}
	ctx := context.Background()

	benchP95(b, func() {
		if _, err := store.GetTicketByRef(ctx, st.DB(), ref); err != nil {
			b.Fatalf("GetTicketByRef: %v", err)
		}
	})
}

// BenchmarkListProjectsFirstPage is §11's "first-page list" target
// applied to the project list.
func BenchmarkListProjectsFirstPage(b *testing.B) {
	st, _ := fullFixture(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.ListProjects(ctx, st.DB(), 20, "", 0, false); err != nil {
			b.Fatalf("ListProjects: %v", err)
		}
	}
}

// BenchmarkPriorityQueueFirstPage is §11's "first-page list" target
// applied to the priority queue (product spec §5.6), the busiest read
// path Phase 1 adds — 4,000 tickets in the sample project's queue.
func BenchmarkPriorityQueueFirstPage(b *testing.B) {
	st, sum := fullFixture(b)
	proj, err := store.GetProjectByKey(context.Background(), st.DB(), sum.SampleProjectKey)
	if err != nil {
		b.Fatalf("GetProjectByKey: %v", err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.PriorityQueue(ctx, st.DB(), proj.ID, store.TicketFilters{}, 20, 0, 0, "", 0); err != nil {
			b.Fatalf("PriorityQueue: %v", err)
		}
	}
}

// BenchmarkPriorityQueueFilteredByAssignee is the benchmark
// docs/contracts/list-filters.md's "Index coverage" section calls for:
// idx_tickets_priority_queue covers (project_id, priority_rank,
// position) only, so a selective filter on top of it (assignee, here)
// still scans in ordering order and discards non-matching rows rather
// than seeking directly to matches — this measures whether that scan-
// and-discard pattern meets §11's p95 target at the reference dataset
// scale (100,000 tickets), rather than assuming it does. The fixture
// generator doesn't assign any ticket to an actor, so this creates one
// agent actor and assigns exactly one of the sample project's 4,000
// tickets to it — the selective (1-of-4000), not pathological, case.
func BenchmarkPriorityQueueFilteredByAssignee(b *testing.B) {
	st, sum := fullFixture(b)
	ctx := context.Background()
	proj, err := store.GetProjectByKey(ctx, st.DB(), sum.SampleProjectKey)
	if err != nil {
		b.Fatalf("GetProjectByKey: %v", err)
	}
	ref, err := domain.Parse(sum.SampleTicketRef)
	if err != nil {
		b.Fatalf("parse sample ref: %v", err)
	}
	ticket, err := store.GetTicketByRef(ctx, st.DB(), ref)
	if err != nil {
		b.Fatalf("GetTicketByRef: %v", err)
	}
	// Go's benchmark runner re-invokes this whole function (including
	// setup above the b.N loop) several times while calibrating
	// iteration count, so "create the assignee actor" must be
	// idempotent, not one-shot — reuse it if a prior calibration pass
	// already created it (unique on kind+name).
	now := time.Now().UTC().Format(store.TimeLayout)
	assigneeID, err := store.CreateActor(ctx, st.DB(), domain.ActorAgent, "bench-assignee", "", nil, now)
	if err != nil {
		assigneeID, err = store.GetActorIDByRef(ctx, st.DB(), domain.ActorAgent, "bench-assignee")
		if err != nil {
			b.Fatalf("CreateActor (and fallback lookup): %v", err)
		}
	}
	// Re-fetch the ticket's current version too — a prior calibration
	// pass's AssignTicket call already bumped it.
	ticket, err = store.GetTicketByRef(ctx, st.DB(), ref)
	if err != nil {
		b.Fatalf("GetTicketByRef (refresh before assign): %v", err)
	}
	if ticket.Entity.Assignee == nil || ticket.Entity.Assignee.Name != "bench-assignee" {
		if _, err := store.AssignTicket(ctx, st.DB(), ticket.ID, &assigneeID, ticket.Entity.Version, now); err != nil {
			b.Fatalf("AssignTicket: %v", err)
		}
	}

	benchP95(b, func() {
		if _, err := store.PriorityQueue(ctx, st.DB(), proj.ID, store.TicketFilters{AssigneeID: assigneeID}, 20, 0, 0, "", 0); err != nil {
			b.Fatalf("PriorityQueue (assignee filter): %v", err)
		}
	})
}

// BenchmarkIssueRegisterFirstPage mirrors BenchmarkPriorityQueueFirstPage
// for the issue register (product spec §5.5).
func BenchmarkIssueRegisterFirstPage(b *testing.B) {
	st, sum := fullFixture(b)
	proj, err := store.GetProjectByKey(context.Background(), st.DB(), sum.SampleProjectKey)
	if err != nil {
		b.Fatalf("GetProjectByKey: %v", err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.IssueRegister(ctx, st.DB(), proj.ID, store.TicketFilters{}, 20, 0, 0, 0, "", 0); err != nil {
			b.Fatalf("IssueRegister: %v", err)
		}
	}
}

// BenchmarkSearchFirstPage is §11's "full-text search first-page"
// target (p95 < 200ms), against a search_fts index built by
// RebuildSearchIndex over fullFixture's full reference dataset —
// tickets, comments, decisions, plans, and documents are all indexed
// (ADR 0018). The query is "2001 fixture": every fixture ticket embeds
// its own sequence number in its title, and every one of its comments
// repeats that number in the body (flushTicketBatch's "Fixture comment
// %d on ticket %d" format), so this is a realistic *selective* search
// — a handful of hits per project, ~150 total — not the pathological
// case BenchmarkSearchFirstPageCommonTerm below measures separately.
func BenchmarkSearchFirstPage(b *testing.B) {
	st, _ := fullFixture(b)
	ctx := context.Background()
	query := domain.SanitizeFTSQuery("2001 fixture")

	benchP95(b, func() {
		if _, err := store.Search(ctx, st.DB(), query, store.SearchFilters{}, 20, 0); err != nil {
			b.Fatalf("Search: %v", err)
		}
	})
}

// BenchmarkSearchFirstPageCommonTerm is a deliberately worst-case
// companion to BenchmarkSearchFirstPage: "fixture" appears in every
// fixture-generated ticket/comment/decision/plan/document's title or
// body, so this query matches essentially the entire ~615,000-row
// corpus. SQLite FTS5's bm25() ranking cost scales with the number of
// *matching* rows, not just the page size — ORDER BY bm25(...) LIMIT
// still has to score the whole match set before it can sort and trim
// — so this benchmark exists to measure and document that known
// limitation (docs/benchmarks.md) rather than to represent typical
// search traffic. A real installation's vocabulary is far more varied
// than this generator's repeated filler text; BenchmarkSearchFirstPage
// above is the representative case.
func BenchmarkSearchFirstPageCommonTerm(b *testing.B) {
	st, _ := fullFixture(b)
	ctx := context.Background()
	query := domain.SanitizeFTSQuery("fixture")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Search(ctx, st.DB(), query, store.SearchFilters{}, 20, 0); err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}
