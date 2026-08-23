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
	"sync"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/fixtures"
	"github.com/ArloB/tickets/internal/store"
)

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
// fetch" target (p95 < 100ms at the reference dataset scale).
func BenchmarkGetTicketByRef(b *testing.B) {
	st, sum := fullFixture(b)
	ref, err := domain.Parse(sum.SampleTicketRef)
	if err != nil {
		b.Fatalf("parse sample ref: %v", err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.GetTicketByRef(ctx, st.DB(), ref); err != nil {
			b.Fatalf("GetTicketByRef: %v", err)
		}
	}
}

// BenchmarkListProjectsFirstPage is §11's "first-page list" target
// applied to the project list.
func BenchmarkListProjectsFirstPage(b *testing.B) {
	st, _ := fullFixture(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.ListProjects(ctx, st.DB(), 20, "", 0); err != nil {
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
