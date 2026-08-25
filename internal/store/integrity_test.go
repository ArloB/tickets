package store

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestIntegrityCheckOnHealthyDatabaseReportsOK confirms a freshly
// migrated database — the state every other test in this package
// starts from — passes PRAGMA integrity_check cleanly, so the
// baseline "no findings" case is proven rather than assumed.
func TestIntegrityCheckOnHealthyDatabaseReportsOK(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	ok, messages, err := IntegrityCheck(context.Background(), s.DB())
	if err != nil {
		t.Fatalf("IntegrityCheck: %v", err)
	}
	if !ok {
		t.Errorf("ok = false, messages = %v, want ok on a healthy database", messages)
	}
}

// TestForeignKeyCheckOnHealthyDatabaseReportsNoViolations confirms
// the same baseline for PRAGMA foreign_key_check: zero rows on a
// database that only ever went through this codebase's own
// FK-enforced write paths.
func TestForeignKeyCheckOnHealthyDatabaseReportsNoViolations(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	violations, err := ForeignKeyCheck(context.Background(), s.DB())
	if err != nil {
		t.Fatalf("ForeignKeyCheck: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %+v, want none", violations)
	}
}

// TestForeignKeyCheckDetectsDanglingReference confirms the pragma
// actually catches a real violation when one exists — proving the
// zero-violations case above means "checked and clean," not "the
// query silently returns nothing." foreign_keys enforcement (ADR
// 0003) blocks this at INSERT time, so the violation is manufactured
// by disabling it for one connection-local pragma, matching how a
// real violation could only ever arise (external tooling bypassing
// the pragma, or a pre-ADR-0003 legacy row).
func TestForeignKeyCheckDetectsDanglingReference(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	if _, err := s.DB().ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign_keys: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		`INSERT INTO comments(entity_id, author_id, body, version, created_at, updated_at) VALUES (999999, 999999, 'orphan', 1, 'x', 'x')`,
	); err != nil {
		t.Fatalf("insert dangling comment: %v", err)
	}

	violations, err := ForeignKeyCheck(ctx, s.DB())
	if err != nil {
		t.Fatalf("ForeignKeyCheck: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("violations = none, want at least one from the manufactured dangling row")
	}
	found := false
	for _, v := range violations {
		if v.Table == "comments" {
			found = true
		}
	}
	if !found {
		t.Errorf("violations = %+v, want one naming table \"comments\"", violations)
	}
}

// TestListReferencedBlobHashesCoversAllFourColumns confirms the union
// query actually reaches attachments, attachment_versions,
// content_items, and content_versions — each seeded with a distinct
// hash directly (bypassing the full service-layer create/update flow,
// which internal/service's own attachment/content-item tests already
// exercise) so a query that only checked two of the four tables would
// still fail this test.
func TestListReferencedBlobHashesCoversAllFourColumns(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()
	now := Now()
	actorID := mustSystemActorID(t, db)

	projID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, actorID, now)
	if err != nil {
		t.Fatalf("insert project entity: %v", err)
	}
	if err := InsertProject(ctx, db, projID, "HSH", "Hashes", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}

	ticketEntityID, _, err := InsertEntity(ctx, db, &projID, domain.KindTicket, actorID, now)
	if err != nil {
		t.Fatalf("insert ticket entity: %v", err)
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO attachments(entity_id, kind, title, current_version, file_hash, created_at, created_by) VALUES (?, 'upload', 't', 1, 'hash-attachments-current', ?, ?)`,
		ticketEntityID, now, actorID,
	)
	if err != nil {
		t.Fatalf("insert attachments row: %v", err)
	}
	attachmentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("attachment last insert id: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO attachment_versions(attachment_id, version, kind, file_hash, uploaded_by, created_at) VALUES (?, 0, 'upload', 'hash-attachment-versions', ?, ?)`,
		attachmentID, actorID, now,
	); err != nil {
		t.Fatalf("insert attachment_versions row: %v", err)
	}

	contentEntityID, _, err := InsertEntity(ctx, db, &projID, domain.KindPlan, actorID, now)
	if err != nil {
		t.Fatalf("insert content item entity: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO content_items(id, project_id, kind, seq, title, representation, file_hash) VALUES (?, ?, 'plan', 1, 't', 'file', 'hash-content-items-current')`,
		contentEntityID, projID,
	); err != nil {
		t.Fatalf("insert content_items row: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO content_versions(content_item_id, version, representation, title, file_hash, edited_by, created_at) VALUES (?, 0, 'file', 't', 'hash-content-versions', ?, ?)`,
		contentEntityID, actorID, now,
	); err != nil {
		t.Fatalf("insert content_versions row: %v", err)
	}

	hashes, err := ListReferencedBlobHashes(ctx, db)
	if err != nil {
		t.Fatalf("ListReferencedBlobHashes: %v", err)
	}
	for _, want := range []string{
		"hash-attachments-current", "hash-attachment-versions",
		"hash-content-items-current", "hash-content-versions",
	} {
		if !hashes[want] {
			t.Errorf("hashes = %v, want it to include %q", hashes, want)
		}
	}
	if len(hashes) != 4 {
		t.Errorf("hashes = %v, want exactly 4 distinct entries", hashes)
	}
}
