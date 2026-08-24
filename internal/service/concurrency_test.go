package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TestConcurrentTicketCreateReferenceAllocation checks whether ADR
// 0009's safety argument ("SQLite's serialized-writer model means two
// concurrent creates can't both observe no-row-then-write") actually
// holds for the transactions internal/service issues. That argument is
// true for BEGIN IMMEDIATE, but Go's database/sql starts a *deferred*
// transaction by default — no lock until the first write — under which
// two goroutines can both finish their reads and then race on the
// write, one of them hitting SQLITE_BUSY_SNAPSHOT instead of simply
// waiting. The Step 2 spike's concurrency assertion doesn't cover this:
// it used SetMaxOpenConns(1) and single-statement autocommit writes,
// not read-then-write transactions from multiple connections.
//
// Run with -race (task test:race) for the strongest signal.
func TestConcurrentTicketCreateReferenceAllocation(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	refs := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
				ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: fmt.Sprintf("Ticket %d", i),
			}, testActor, testCorrelationID, "", "")
			refs[i] = ticket.Ref
			errs[i] = err
		}(i)
	}
	wg.Wait()

	var failures int
	for i, err := range errs {
		if err != nil {
			failures++
			t.Logf("goroutine %d failed: %v", i, err)
		}
	}
	if failures > 0 {
		t.Fatalf("%d/%d concurrent CreateTicket calls failed — see ADR 0009's implementation note", failures, n)
	}

	seen := make(map[string]bool, n)
	for i, ref := range refs {
		if ref == "" {
			t.Errorf("goroutine %d returned an empty ref with no error", i)
			continue
		}
		if seen[ref] {
			t.Errorf("duplicate reference allocated: %s", ref)
		}
		seen[ref] = true
	}
	if len(seen) != n {
		t.Errorf("got %d distinct references, want %d", len(seen), n)
	}
}

// TestConcurrentActorsUpdateSameTicketOneWins is verification gate 9's
// two-actor case: two different actors race to update the same
// ticket with the same expected version — under BEGIN IMMEDIATE (ADR
// 0003) exactly one write should go through and the other must fail
// with version_conflict carrying the winner's new version, not a
// stale or zero value.
func TestConcurrentActorsUpdateSameTicketOneWins(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Contested",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	actor1 := testActor
	actor2 := domain.ActorRef{Kind: domain.ActorSystem, Name: "system"}

	var wg sync.WaitGroup
	results := make([]struct {
		ticket domain.Ticket
		err    error
	}, 2)
	statuses := []domain.WorkflowStatus{domain.WorkflowStatusReady, domain.WorkflowStatusInProgress}
	actors := []domain.ActorRef{actor1, actor2}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			updated, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
				Ref: ref, NewStatus: statuses[i], ExpectedVersion: ticket.Version,
			}, actors[i], testCorrelationID)
			results[i].ticket, results[i].err = updated, err
		}(i)
	}
	wg.Wait()

	var wins, conflicts int
	var winnerVersion int64
	for _, r := range results {
		if r.err == nil {
			wins++
			winnerVersion = r.ticket.Version
		} else {
			var svcErr *Error
			if !errors.As(r.err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
				t.Fatalf("loser's error = %v, want version_conflict", r.err)
			}
			conflicts++
			if svcErr.CurrentVersion == nil {
				t.Fatalf("loser's CurrentVersion = nil, want set")
			}
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("got %d wins and %d conflicts, want exactly one of each", wins, conflicts)
	}

	for _, r := range results {
		if r.err != nil {
			var svcErr *Error
			_ = errors.As(r.err, &svcErr)
			if *svcErr.CurrentVersion != winnerVersion {
				t.Errorf("loser's CurrentVersion = %d, want the winner's version %d", *svcErr.CurrentVersion, winnerVersion)
			}
		}
	}
}

// TestIdempotencyKeyScopedByActor is the service-level half of ADR
// 0008's actor-scoping fix (internal/store's half lives in
// internal/store/identity_migration_test.go): two different actors
// reusing the same client-chosen Idempotency-Key against the same
// endpoint must create two distinct tickets, not collide or silently
// hand one actor's created ticket back to the other. The same actor
// reusing its own key must still hit the cache as before.
func TestIdempotencyKeyScopedByActor(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	req := CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Shared key ticket"}
	fp, err := Fingerprint("POST", "/api/v1/projects/ABC/tickets", []byte(`{"title":"Shared key ticket","type":"task"}`))
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	actorA := testActor
	actorB := domain.ActorRef{Kind: domain.ActorSystem, Name: "system"}

	first, err := s.CreateTicket(ctx, req, actorA, testCorrelationID, "shared-key", fp)
	if err != nil {
		t.Fatalf("actor A create: %v", err)
	}
	second, err := s.CreateTicket(ctx, req, actorB, testCorrelationID, "shared-key", fp)
	if err != nil {
		t.Fatalf("actor B reusing actor A's idempotency key: want a fresh create to succeed, got %v", err)
	}
	if second.Ref == first.Ref {
		t.Errorf("actor B got actor A's ticket (%s) back instead of creating its own", first.Ref)
	}

	replay, err := s.CreateTicket(ctx, req, actorA, testCorrelationID, "shared-key", fp)
	if err != nil {
		t.Fatalf("actor A replaying its own key: %v", err)
	}
	if replay.Ref != first.Ref {
		t.Errorf("actor A's own key replay = %s, want the original %s", replay.Ref, first.Ref)
	}
}

// BenchmarkConcurrentTicketCreate measures concurrent-writer
// throughput — the direct continuation of ADR 0003/0009's thread
// (product spec §11, Phase 1 plan Step 6). Every mutation takes
// SQLite's write lock up front (_txlock=immediate, ADR 0003) and Phase
// 1 roughly tripled the write-path work per mutation versus Phase 0
// (audit event + mention rescan + position allocation on top of the
// row itself), so this is what actually measures whether that added
// work shows up as serialized throughput loss under contention against
// busy_timeout(5000), not just single-writer latency.
//
// Uses b.RunParallel rather than a fixed goroutine count so it scales
// with GOMAXPROCS the way TestConcurrentTicketCreateReferenceAllocation
// above does not need to (that test only cares about correctness at a
// fixed concurrency level, not throughput at whatever this machine's
// core count is). b.Error, not b.Fatal, inside the parallel closure —
// per testing.B's own documentation, Fatal/FailNow may only be called
// from the goroutine running the benchmark function itself, not from
// RunParallel's worker goroutines.
func BenchmarkConcurrentTicketCreate(b *testing.B) {
	ctx := context.Background()
	st, err := store.Open(b.TempDir())
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	blobs, err := blobstore.Open(b.TempDir())
	if err != nil {
		b.Fatalf("blobstore.Open: %v", err)
	}
	s := New(st, blobs)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "BENCH", Title: "Bench"}, testActor, testCorrelationID, "", ""); err != nil {
		b.Fatalf("create project: %v", err)
	}

	var counter int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := atomic.AddInt64(&counter, 1)
			_, err := s.CreateTicket(ctx, CreateTicketRequest{
				ProjectKey: "BENCH", Type: domain.TicketTypeTask, Title: fmt.Sprintf("Bench ticket %d", n),
			}, testActor, testCorrelationID, "", "")
			if err != nil {
				b.Errorf("CreateTicket: %v", err)
			}
		}
	})
}
