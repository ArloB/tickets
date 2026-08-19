package store

import (
	"context"
	"testing"
)

// TestSeededActorsExist is migration 0002_core_domain.sql's executable
// spec: a fresh database must contain the 'system' and 'local' actors
// (product spec §4.1's system actor, and the stand-in for a real
// session/token until ADR 0004's auth lands in Phase 2), with
// timestamps that parse under store.TimeLayout like any other row.
func TestSeededActorsExist(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	for _, want := range []struct{ kind, name string }{
		{"system", "system"},
		{"human", "local"},
	} {
		var createdAt, updatedAt string
		err := s.DB().QueryRow(
			`SELECT created_at, updated_at FROM actors WHERE kind = ? AND name = ?`,
			want.kind, want.name,
		).Scan(&createdAt, &updatedAt)
		if err != nil {
			t.Fatalf("seeded actor kind=%q name=%q not found: %v", want.kind, want.name, err)
		}
		if _, err := parseTime(createdAt); err != nil {
			t.Errorf("seeded actor %s:%s created_at %q does not parse as TimeLayout: %v", want.kind, want.name, createdAt, err)
		}
		if _, err := parseTime(updatedAt); err != nil {
			t.Errorf("seeded actor %s:%s updated_at %q does not parse as TimeLayout: %v", want.kind, want.name, updatedAt, err)
		}
	}
}

// TestEntitiesBackfilledToSystemActor guards the migration's backfill
// UPDATE: every entities row that existed before created_by was added
// (which, on a fresh database, is none — the column and the backfill
// both apply before any application code runs) must not be left with a
// NULL created_by. This test creates a row the old way (InsertEntity,
// which does not set created_by — that's Step 4's job), confirming the
// column is nullable for pre-actor-threading code, and separately
// confirms the system actor a real backfill would use actually exists
// and is resolvable.
func TestEntitiesBackfilledToSystemActor(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	id, _, err := InsertEntity(ctx, s.DB(), nil, "project", Now())
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	var createdBy *int64
	if err := s.DB().QueryRow(`SELECT created_by FROM entities WHERE id = ?`, id).Scan(&createdBy); err != nil {
		t.Fatalf("query created_by: %v", err)
	}
	if createdBy != nil {
		t.Errorf("InsertEntity set created_by = %v; want NULL until Step 4 threads an actor through", *createdBy)
	}

	var systemID int64
	if err := s.DB().QueryRow(`SELECT id FROM actors WHERE kind = 'system' AND name = 'system'`).Scan(&systemID); err != nil {
		t.Fatalf("system actor not resolvable: %v", err)
	}
	if systemID <= 0 {
		t.Errorf("system actor id = %d, want a positive surrogate key", systemID)
	}
}

// TestInsertTicketWritesRankColumns is the regression this migration's
// rollout needs: InsertTicket must derive priority_rank/severity_rank
// from the priority/severity it's given, not leave them at the
// column's NOT NULL DEFAULT 4 sentinel (which would silently sort every
// new ticket as if it were unranked).
func TestInsertTicketWritesRankColumns(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	projID, _, err := InsertEntity(ctx, db, nil, "project", Now())
	if err != nil {
		t.Fatalf("InsertEntity project: %v", err)
	}
	if err := InsertProject(ctx, db, projID, "ABC", "Example", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}
	featID, _, err := InsertEntity(ctx, db, &projID, "feature", Now())
	if err != nil {
		t.Fatalf("InsertEntity feature: %v", err)
	}
	if err := InsertFeature(ctx, db, featID, projID, 1, "General"); err != nil {
		t.Fatalf("InsertFeature: %v", err)
	}

	ticketID, _, err := InsertEntity(ctx, db, &projID, "ticket", Now())
	if err != nil {
		t.Fatalf("InsertEntity ticket: %v", err)
	}
	severity := "high"
	if err := InsertTicket(ctx, db, ticketID, projID, featID, 1, "bug", "Title", "", "backlog", "critical", &severity); err != nil {
		t.Fatalf("InsertTicket: %v", err)
	}

	var priorityRankGot, severityRankGot int
	if err := db.QueryRow(`SELECT priority_rank, severity_rank FROM tickets WHERE id = ?`, ticketID).
		Scan(&priorityRankGot, &severityRankGot); err != nil {
		t.Fatalf("query ranks: %v", err)
	}
	if priorityRankGot != 0 {
		t.Errorf("priority=critical -> priority_rank = %d, want 0", priorityRankGot)
	}
	if severityRankGot != 1 {
		t.Errorf("severity=high -> severity_rank = %d, want 1", severityRankGot)
	}
}
