package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
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

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, "", ""); err != nil {
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
			}, "", "")
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
