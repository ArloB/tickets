package store

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// testProjectForSearch creates a bare project entity (no General
// feature — these tests only need entities.id/projects.id, not a full
// ticket-creation path) for use as search_documents.project_id.
func testProjectForSearch(t *testing.T, db Querier, key string) int64 {
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
	return projID
}

func TestUpsertSearchDocumentAndSearch(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	projID := testProjectForSearch(t, db, "SRCH")
	sysID := mustSystemActorID(t, db)
	entID, _, err := InsertEntity(ctx, db, &projID, domain.KindTicket, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity ticket: %v", err)
	}

	if err := UpsertSearchDocument(ctx, db, "entity", entID, SearchDocumentFields{
		EntityID: entID, Kind: "ticket", ProjectID: projID,
		Ref: "SRCH-1", Status: "backlog", Title: "Reticulate the splines",
		Body: "The renderer needs a faster spline reticulation pass.",
	}); err != nil {
		t.Fatalf("UpsertSearchDocument: %v", err)
	}

	page, err := Search(ctx, db, domain.SanitizeFTSQuery("reticulate"), SearchFilters{}, 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Hits) != 1 || page.Hits[0].Ref != "SRCH-1" {
		t.Fatalf("Search(%q) = %+v, want one hit for SRCH-1", "reticulate", page.Hits)
	}

	// A query with FTS5 syntax metacharacters must not error — this is
	// exactly what domain.SanitizeFTSQuery exists to prevent.
	if _, err := Search(ctx, db, domain.SanitizeFTSQuery(`foo: bar" AND *`), SearchFilters{}, 10, 0); err != nil {
		t.Fatalf("Search with metacharacter-laden query errored: %v", err)
	}

	// Project/kind/status filters narrow correctly.
	if _, err := Search(ctx, db, domain.SanitizeFTSQuery("reticulate"), SearchFilters{ProjectID: projID + 999}, 10, 0); err != nil {
		t.Fatalf("Search with project filter: %v", err)
	} else if page2, _ := Search(ctx, db, domain.SanitizeFTSQuery("reticulate"), SearchFilters{ProjectID: projID + 999}, 10, 0); len(page2.Hits) != 0 {
		t.Fatalf("Search with wrong project filter returned %d hits, want 0", len(page2.Hits))
	}
	if page3, err := Search(ctx, db, domain.SanitizeFTSQuery("reticulate"), SearchFilters{Kinds: []string{"feature"}}, 10, 0); err != nil {
		t.Fatalf("Search with kind filter: %v", err)
	} else if len(page3.Hits) != 0 {
		t.Fatalf("Search with non-matching kind filter returned %d hits, want 0", len(page3.Hits))
	}

	// Deleting the document removes it from search.
	if err := DeleteSearchDocumentsForEntity(ctx, db, entID); err != nil {
		t.Fatalf("DeleteSearchDocumentsForEntity: %v", err)
	}
	if page4, err := Search(ctx, db, domain.SanitizeFTSQuery("reticulate"), SearchFilters{}, 10, 0); err != nil {
		t.Fatalf("Search after delete: %v", err)
	} else if len(page4.Hits) != 0 {
		t.Fatalf("Search after DeleteSearchDocumentsForEntity returned %d hits, want 0", len(page4.Hits))
	}
}

// TestSearchFTSIndexIntegrity runs FTS5's own 'integrity-check' command
// after a mixed insert/update/delete sequence — this is what catches a
// wrong old.title/old.body in the AFTER UPDATE trigger's delete-side
// INSERT: a silently corrupted external-content index still answers
// queries, just with wrong results, so "search finds the row" alone
// would not catch this class of bug.
func TestSearchFTSIndexIntegrity(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	projID := testProjectForSearch(t, db, "INTG")
	sysID := mustSystemActorID(t, db)

	var ids []int64
	for i := 0; i < 5; i++ {
		entID, _, err := InsertEntity(ctx, db, &projID, domain.KindTicket, sysID, Now())
		if err != nil {
			t.Fatalf("InsertEntity: %v", err)
		}
		ids = append(ids, entID)
		if err := UpsertSearchDocument(ctx, db, "entity", entID, SearchDocumentFields{
			EntityID: entID, Kind: "ticket", ProjectID: projID,
			Ref: "INTG-1", Title: "alpha", Body: "beta gamma",
		}); err != nil {
			t.Fatalf("UpsertSearchDocument insert %d: %v", i, err)
		}
	}
	// Update every other row (exercises the AFTER UPDATE trigger's
	// delete-then-reinsert pair).
	for i, entID := range ids {
		if i%2 != 0 {
			continue
		}
		if err := UpsertSearchDocument(ctx, db, "entity", entID, SearchDocumentFields{
			EntityID: entID, Kind: "ticket", ProjectID: projID,
			Ref: "INTG-1", Title: "alpha-updated", Body: "delta epsilon",
		}); err != nil {
			t.Fatalf("UpsertSearchDocument update: %v", err)
		}
	}
	// Delete a couple.
	if err := DeleteSearchDocumentsForEntity(ctx, db, ids[0]); err != nil {
		t.Fatalf("DeleteSearchDocumentsForEntity: %v", err)
	}
	if err := DeleteSearchDocumentsForEntity(ctx, db, ids[len(ids)-1]); err != nil {
		t.Fatalf("DeleteSearchDocumentsForEntity: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO search_fts(search_fts) VALUES('integrity-check')`); err != nil {
		t.Fatalf("FTS5 integrity-check failed (search_fts has drifted from search_documents): %v", err)
	}
}

func TestSearchOffsetCursorRoundTripAndCap(t *testing.T) {
	cur := EncodeSearchOffsetCursor(40)
	got, err := DecodeSearchOffsetCursor(cur)
	if err != nil || got != 40 {
		t.Fatalf("round trip offset 40: got (%d, %v)", got, err)
	}
	if _, err := DecodeSearchOffsetCursor("not-base64!!"); err == nil {
		t.Fatalf("expected error decoding malformed cursor")
	}
	if _, err := DecodeSearchOffsetCursor(EncodeSearchOffsetCursor(maxSearchOffset + 1)); err == nil {
		t.Fatalf("expected error decoding an offset beyond maxSearchOffset")
	}
}
