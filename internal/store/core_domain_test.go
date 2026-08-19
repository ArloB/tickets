package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
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

// TestInsertEntityStampsCreatedBy is Step 4a's replacement for the
// migration-backfill test this used to be: InsertEntity now takes an
// explicit createdBy actor id (ADR 0012) and must write it, not leave
// entities.created_by NULL the way pre-Step-4a code did.
// entities.created_by stays schema-nullable (SQLite forbids a NOT NULL
// ALTER TABLE ADD COLUMN with a REFERENCES clause under
// foreign_keys=ON) but is a Go-level invariant from here on.
func TestInsertEntityStampsCreatedBy(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	sysID := mustSystemActorID(t, s.DB())

	id, _, err := InsertEntity(ctx, s.DB(), nil, domain.KindProject, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	var createdBy *int64
	if err := s.DB().QueryRow(`SELECT created_by FROM entities WHERE id = ?`, id).Scan(&createdBy); err != nil {
		t.Fatalf("query created_by: %v", err)
	}
	if createdBy == nil {
		t.Fatal("InsertEntity left created_by NULL, want the given actor id")
	}
	if *createdBy != sysID {
		t.Errorf("created_by = %d, want %d (the system actor)", *createdBy, sysID)
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
	sysID := mustSystemActorID(t, db)

	projID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity project: %v", err)
	}
	if err := InsertProject(ctx, db, projID, "ABC", "Example", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}
	featID, _, err := InsertEntity(ctx, db, &projID, domain.KindFeature, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity feature: %v", err)
	}
	if err := InsertFeature(ctx, db, featID, projID, 1, "General"); err != nil {
		t.Fatalf("InsertFeature: %v", err)
	}

	ticketID, _, err := InsertEntity(ctx, db, &projID, domain.KindTicket, sysID, Now())
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

// TestInsertFeatureDefaultRankMatchesDefaultPriority guards an
// invariant that holds by coincidence today, not by construction:
// InsertFeature takes no priority parameter, so every feature is
// created with the column defaults ('medium' priority, rank 2 —
// medium's rank per rank.go). Those two defaults were chosen to agree.
// If InsertFeature ever gains an explicit priority parameter (Phase 4,
// when features get their own create/update surface) without also
// writing priority_rank the way InsertTicket already does, this test
// is what catches the drift immediately instead of leaving newly
// created non-default-priority features silently unranked.
func TestInsertFeatureDefaultRankMatchesDefaultPriority(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()
	sysID := mustSystemActorID(t, db)

	projID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity project: %v", err)
	}
	if err := InsertProject(ctx, db, projID, "ABC", "Example", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}
	featID, _, err := InsertEntity(ctx, db, &projID, domain.KindFeature, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity feature: %v", err)
	}
	if err := InsertFeature(ctx, db, featID, projID, 1, "General"); err != nil {
		t.Fatalf("InsertFeature: %v", err)
	}

	var priority string
	var rank int
	if err := db.QueryRow(`SELECT priority, priority_rank FROM features WHERE id = ?`, featID).
		Scan(&priority, &rank); err != nil {
		t.Fatalf("query feature priority/rank: %v", err)
	}
	if priority != "medium" {
		t.Errorf("default feature priority = %q, want \"medium\"", priority)
	}
	if want := priorityRank(priority); rank != want {
		t.Errorf("default feature priority_rank = %d, want %d (priorityRank(%q), the value that must match the column default)", rank, want, priority)
	}
}

// TestGetTicketByRefRejectsWrongKind is the regression for the bug
// found while planning Phase 1: GetTicketByRef matched on
// (ProjectKey, Seq) alone, so a feature reference sharing a ticket's
// sequence number (ABC-F1 vs. ticket ABC-1) would silently resolve to
// the ticket instead of returning not found.
func TestGetTicketByRefRejectsWrongKind(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()
	sysID := mustSystemActorID(t, db)

	projID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity project: %v", err)
	}
	if err := InsertProject(ctx, db, projID, "ABC", "Example", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}
	featID, _, err := InsertEntity(ctx, db, &projID, domain.KindFeature, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity feature: %v", err)
	}
	if err := InsertFeature(ctx, db, featID, projID, 1, "General"); err != nil {
		t.Fatalf("InsertFeature: %v", err)
	}
	ticketID, _, err := InsertEntity(ctx, db, &projID, domain.KindTicket, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity ticket: %v", err)
	}
	if err := InsertTicket(ctx, db, ticketID, projID, featID, 1, "task", "Title", "", "backlog", "medium", nil); err != nil {
		t.Fatalf("InsertTicket: %v", err)
	}

	// ABC-1 (a real ticket) resolves.
	if _, err := GetTicketByRef(ctx, db, domain.Reference{ProjectKey: "ABC", Kind: domain.KindTicket, Seq: 1}); err != nil {
		t.Fatalf("GetTicketByRef(ABC-1): %v", err)
	}
	// ABC-F1 (a feature reference with the same seq) must NOT resolve
	// to the ticket.
	_, err = GetTicketByRef(ctx, db, domain.Reference{ProjectKey: "ABC", Kind: domain.KindFeature, Seq: 1})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTicketByRef(ABC-F1) = %v, want ErrNotFound", err)
	}
}
