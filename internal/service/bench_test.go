package service

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/fixtures"
	"github.com/ArloB/tickets/internal/store"
)

// benchP95 is internal/store/bench_test.go's helper, duplicated here
// rather than shared across packages (this codebase's stated
// preference — see ADR 0011's note on duplicated store functions).
// See that copy's doc comment for the full rationale, in particular
// why its "first-iter-ms/op" metric is deliberately not called "cold."
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
	fullFixtureService *Service
	fullFixtureSummary fixtures.Summary
	fullFixtureDir     string
	fullFixtureErr     error
)

// fullFixtureSvc lazily builds product spec §11's reference dataset
// once per test binary process, wrapped in a real *Service — the same
// lazy, shared, TestMain-cleaned-up pattern internal/store/bench_test.go
// uses and explains in more detail. Never triggered by a plain `go
// test` run.
func fullFixtureSvc(b *testing.B) (*Service, fixtures.Summary) {
	b.Helper()
	fullFixtureOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tickets-svc-bench-*")
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
		blobs, berr := blobstore.Open(dir)
		if berr != nil {
			fullFixtureErr = berr
			return
		}
		fullFixtureService = New(st, blobs)
		fullFixtureSummary = sum
	})
	if fullFixtureErr != nil {
		b.Fatalf("build full-scale fixture: %v", fullFixtureErr)
	}
	return fullFixtureService, fullFixtureSummary
}

func TestMain(m *testing.M) {
	code := m.Run()
	if fullFixtureService != nil {
		_ = fullFixtureService.store.Close()
	}
	if fullFixtureDir != "" {
		_ = os.RemoveAll(fullFixtureDir)
	}
	os.Exit(code)
}

// BenchmarkCreateTicket is product spec §11's "ordinary non-upload
// mutation" target (p95 < 250ms), run against the full reference
// dataset (25 projects / 100k tickets / 500k comments already
// present) rather than an empty store, so the measurement reflects a
// warm, realistically sized database as §11 specifies.
func BenchmarkCreateTicket(b *testing.B) {
	svc, sum := fullFixtureSvc(b)
	ctx := context.Background()
	req := CreateTicketRequest{
		ProjectKey: sum.SampleProjectKey, Type: domain.TicketTypeTask, Title: "Benchmark ticket",
	}

	benchP95(b, func() {
		if _, err := svc.CreateTicket(ctx, req, testActor, testCorrelationID, "", ""); err != nil {
			b.Fatalf("CreateTicket: %v", err)
		}
	})
}

// BenchmarkListActivityFirstPage is §11's "first-page list" target
// applied to the project activity feed — activity.go:160-170 flags its
// per-call actor/entity-ref caches as "worth revisiting if a benchmark
// against the §11 reference dataset ever shows this page is still too
// slow." This is that benchmark: the sample project's feed, built from
// its 4,000 tickets' worth of ticket_created/comment_added events.
func BenchmarkListActivityFirstPage(b *testing.B) {
	svc, sum := fullFixtureSvc(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.ListActivity(ctx, sum.SampleProjectKey, ActivityListFilters{}, 20, ""); err != nil {
			b.Fatalf("ListActivity: %v", err)
		}
	}
}

// BenchmarkConcurrentReadWrite is §11's "concurrent readers/writers"
// requirement: b.RunParallel drives many goroutines reading the sample
// ticket via GetTicketByRef while one dedicated goroutine keeps
// creating tickets, so the read benchmark's timing reflects real
// contention against SQLite's single writer (WAL mode, ADR 0003)
// rather than an artificially read-only database.
func BenchmarkConcurrentReadWrite(b *testing.B) {
	svc, sum := fullFixtureSvc(b)
	ctx := context.Background()
	ref, err := domain.Parse(sum.SampleTicketRef)
	if err != nil {
		b.Fatalf("parse sample ref: %v", err)
	}

	stop := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		req := CreateTicketRequest{ProjectKey: sum.SampleProjectKey, Type: domain.TicketTypeTask, Title: "Concurrent writer ticket"}
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := svc.CreateTicket(ctx, req, testActor, testCorrelationID, "", ""); err != nil {
					b.Error(err)
					return
				}
			}
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := svc.GetTicket(ctx, ref); err != nil {
				b.Fatalf("GetTicket: %v", err)
			}
		}
	})
	b.StopTimer()
	close(stop)
	<-writerDone
}
