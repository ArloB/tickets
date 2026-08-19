package service

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
		fullFixtureService = New(st)
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.CreateTicket(ctx, req, testActor, testCorrelationID, "", ""); err != nil {
			b.Fatalf("CreateTicket: %v", err)
		}
	}
}
