package store

import (
	"context"
	"strconv"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// testProjectWithTickets creates a project, its General feature, and
// n tickets (each with the given priority, in insertion order), and
// returns the project's internal id plus the tickets' internal ids in
// insertion order. Every ticket is inserted at position 0 — these
// tests only need distinct priorities/severities to prove rank
// ordering, not distinct positions (created_at breaks position ties).
func testProjectWithTickets(t *testing.T, db Querier, key string, specs []struct {
	priority   string
	severity   *string
	ticketType string
}) (projID int64, ticketIDs []int64) {
	t.Helper()
	ctx := context.Background()
	sysID := mustSystemActorID(t, db)

	projID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity project: %v", err)
	}
	if err := InsertProject(ctx, db, projID, key, "Example", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}
	featID, _, err := InsertEntity(ctx, db, &projID, domain.KindFeature, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity feature: %v", err)
	}
	if err := InsertFeature(ctx, db, featID, projID, 1, "General", "", "medium", 1000); err != nil {
		t.Fatalf("InsertFeature: %v", err)
	}

	for i, spec := range specs {
		ticketID, _, err := InsertEntity(ctx, db, &projID, domain.KindTicket, sysID, Now())
		if err != nil {
			t.Fatalf("InsertEntity ticket %d: %v", i, err)
		}
		ticketType := spec.ticketType
		if ticketType == "" {
			ticketType = "task"
		}
		if err := InsertTicket(ctx, db, ticketID, projID, featID, int64(i+1), ticketType, "Title", "", "backlog", spec.priority, spec.severity, 0); err != nil {
			t.Fatalf("InsertTicket %d: %v", i, err)
		}
		ticketIDs = append(ticketIDs, ticketID)
	}
	return projID, ticketIDs
}

// TestPriorityQueueOrdersByRankNotText is Phase 1 verification gate 4
// at the repository level: tickets inserted in an order that
// deliberately contradicts priority must still come back critical,
// high, medium, low. A TEXT-sorted priority column would return
// critical, high, low, medium instead — this is the regression the
// priority_rank column (migration 0002_core_domain.sql) exists to
// prevent.
func TestPriorityQueueOrdersByRankNotText(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	db := s.DB()

	// Insertion order deliberately contradicts priority order.
	projID, ticketIDs := testProjectWithTickets(t, db, "ABC", []struct {
		priority   string
		severity   *string
		ticketType string
	}{
		{priority: "low"},
		{priority: "medium"},
		{priority: "critical"},
		{priority: "high"},
	})

	page, err := PriorityQueue(context.Background(), db, projID, TicketFilters{}, 10, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("PriorityQueue: %v", err)
	}
	if len(page.Tickets) != 4 {
		t.Fatalf("got %d tickets, want 4", len(page.Tickets))
	}
	wantOrder := []domain.Priority{domain.PriorityCritical, domain.PriorityHigh, domain.PriorityMedium, domain.PriorityLow}
	for i, want := range wantOrder {
		if got := page.Tickets[i].Entity.Priority; got != want {
			t.Errorf("position %d: priority = %s, want %s", i, got, want)
		}
	}
	_ = ticketIDs
}

// TestPriorityQueueOrdersWithinGroupByPositionThenAge asserts the
// secondary/tertiary sort: within one priority group, position first,
// then creation time.
func TestPriorityQueueOrdersWithinGroupByPositionThenAge(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	db := s.DB()

	projID, ticketIDs := testProjectWithTickets(t, db, "ABC", []struct {
		priority   string
		severity   *string
		ticketType string
	}{
		{priority: "high"}, // inserted first -> earliest created_at
		{priority: "high"}, // inserted second -> later created_at
	})
	// Reverse position so ticket B (index 1) should now sort before A.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE tickets SET position = 100 WHERE id = ?`, ticketIDs[0]); err != nil {
		t.Fatalf("set position A: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tickets SET position = 50 WHERE id = ?`, ticketIDs[1]); err != nil {
		t.Fatalf("set position B: %v", err)
	}

	page, err := PriorityQueue(ctx, db, projID, TicketFilters{}, 10, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("PriorityQueue: %v", err)
	}
	if len(page.Tickets) != 2 {
		t.Fatalf("got %d tickets, want 2", len(page.Tickets))
	}
	if page.Tickets[0].ID != ticketIDs[1] || page.Tickets[1].ID != ticketIDs[0] {
		t.Errorf("position ordering wrong: got IDs [%d %d], want [%d %d] (lower position first)",
			page.Tickets[0].ID, page.Tickets[1].ID, ticketIDs[1], ticketIDs[0])
	}
}

func TestPriorityQueuePagination(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	db := s.DB()

	projID, _ := testProjectWithTickets(t, db, "ABC", []struct {
		priority   string
		severity   *string
		ticketType string
	}{
		{priority: "critical"},
		{priority: "high"},
		{priority: "medium"},
	})

	ctx := context.Background()
	page1, err := PriorityQueue(ctx, db, projID, TicketFilters{}, 2, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("PriorityQueue page 1: %v", err)
	}
	if len(page1.Tickets) != 2 || page1.NextCursor == "" {
		t.Fatalf("page 1 = %d tickets, cursor=%q; want 2 tickets and a non-empty cursor", len(page1.Tickets), page1.NextCursor)
	}

	parts, err := DecodeCursor(page1.NextCursor, 4)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	afterRank, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		t.Fatalf("parse afterRank: %v", err)
	}
	afterPosition, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("parse afterPosition: %v", err)
	}
	afterID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		t.Fatalf("parse afterID: %v", err)
	}

	page2, err := PriorityQueue(ctx, db, projID, TicketFilters{}, 2, afterRank, afterPosition, parts[2], afterID)
	if err != nil {
		t.Fatalf("PriorityQueue page 2: %v", err)
	}
	if len(page2.Tickets) != 1 || page2.NextCursor != "" {
		t.Fatalf("page 2 = %d tickets, cursor=%q; want 1 ticket and no next cursor", len(page2.Tickets), page2.NextCursor)
	}
	if page2.Tickets[0].Entity.Priority != domain.PriorityMedium {
		t.Errorf("page 2 ticket priority = %s, want medium", page2.Tickets[0].Entity.Priority)
	}
}

// TestIssueRegisterOrdersBySeverityThenPriority is Phase 1
// verification gate 5: bug/security tickets only, ordered severity,
// then priority, then position, then age. Non-issue ticket types
// (task, chore) must not appear even if they'd otherwise sort first.
func TestIssueRegisterOrdersBySeverityThenPriority(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	db := s.DB()

	low, medium, high, critical := "low", "medium", "high", "critical"
	projID, _ := testProjectWithTickets(t, db, "ABC", []struct {
		priority   string
		severity   *string
		ticketType string
	}{
		{priority: "critical", severity: &low, ticketType: "bug"},      // lowest severity despite highest priority
		{priority: "low", severity: &critical, ticketType: "security"}, // highest severity despite lowest priority
		{priority: "medium", severity: &high, ticketType: "bug"},
		{priority: "high", severity: &medium, ticketType: "bug"},
		{priority: "critical", ticketType: "task"}, // not an issue type; must be excluded regardless of priority
	})

	page, err := IssueRegister(context.Background(), db, projID, TicketFilters{}, 10, 0, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("IssueRegister: %v", err)
	}
	if len(page.Tickets) != 4 {
		t.Fatalf("got %d tickets, want 4 (task ticket must be excluded)", len(page.Tickets))
	}
	wantSeverity := []domain.Severity{domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow}
	for i, want := range wantSeverity {
		got := page.Tickets[i].Entity.Severity
		if got == nil || *got != want {
			t.Errorf("position %d: severity = %v, want %s", i, got, want)
		}
	}
}

// TestIssueRegisterOrdersWithinGroupByPositionThenAge is
// TestPriorityQueueOrdersWithinGroupByPositionThenAge's counterpart
// for IssueRegister — gate 5's full ordering (severity, then
// priority, then position, then age) is only fully covered once the
// position/age legs are checked here too, not just severity/priority.
func TestIssueRegisterOrdersWithinGroupByPositionThenAge(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	db := s.DB()

	high := "high"
	projID, ticketIDs := testProjectWithTickets(t, db, "ABC", []struct {
		priority   string
		severity   *string
		ticketType string
	}{
		{priority: "medium", severity: &high, ticketType: "bug"}, // inserted first -> earliest created_at
		{priority: "medium", severity: &high, ticketType: "bug"}, // inserted second -> later created_at
	})
	// Reverse position so ticket B (index 1) should now sort before A,
	// same as the PriorityQueue equivalent.
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE tickets SET position = 100 WHERE id = ?`, ticketIDs[0]); err != nil {
		t.Fatalf("set position A: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tickets SET position = 50 WHERE id = ?`, ticketIDs[1]); err != nil {
		t.Fatalf("set position B: %v", err)
	}

	page, err := IssueRegister(ctx, db, projID, TicketFilters{}, 10, 0, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("IssueRegister: %v", err)
	}
	if len(page.Tickets) != 2 {
		t.Fatalf("got %d tickets, want 2", len(page.Tickets))
	}
	if page.Tickets[0].ID != ticketIDs[1] || page.Tickets[1].ID != ticketIDs[0] {
		t.Errorf("position ordering wrong: got IDs [%d %d], want [%d %d] (lower position first)",
			page.Tickets[0].ID, page.Tickets[1].ID, ticketIDs[1], ticketIDs[0])
	}
}

func TestPurgeIdempotencyKeysOlderThan(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	actorID := mustSystemActorID(t, db)
	old := "2020-01-01T00:00:00.000000000Z"
	recent := "2030-01-01T00:00:00.000000000Z"
	if _, err := db.ExecContext(ctx, `INSERT INTO idempotency_keys(key, actor_id, fingerprint, ref_key, created_at) VALUES (?, ?, ?, ?, ?)`,
		"old-key", actorID, "fp1", "ABC-1", old); err != nil {
		t.Fatalf("insert old key: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO idempotency_keys(key, actor_id, fingerprint, ref_key, created_at) VALUES (?, ?, ?, ?, ?)`,
		"recent-key", actorID, "fp2", "ABC-2", recent); err != nil {
		t.Fatalf("insert recent key: %v", err)
	}

	cutoff := "2025-01-01T00:00:00.000000000Z"
	deleted, err := PurgeIdempotencyKeysOlderThan(ctx, db, cutoff)
	if err != nil {
		t.Fatalf("PurgeIdempotencyKeysOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM idempotency_keys`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining rows = %d, want 1", remaining)
	}
	var remainingKey string
	if err := db.QueryRow(`SELECT key FROM idempotency_keys`).Scan(&remainingKey); err != nil {
		t.Fatalf("query remaining key: %v", err)
	}
	if remainingKey != "recent-key" {
		t.Errorf("remaining key = %q, want %q", remainingKey, "recent-key")
	}
}
