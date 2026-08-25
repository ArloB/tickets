package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// TestAdminSearchReindex is Phase 6 Step 8's recovery drill for
// `tickets admin search-reindex` at the actual CLI layer — the
// service-layer rebuild logic already has thorough regression coverage
// (internal/service/search_test.go's TestSearchRebuildIndexRepopulatesFromScratch),
// but nothing previously exercised the command an operator actually
// runs. Seeds a real ticket, corrupts search_documents directly (the
// same "prove rebuild recomputes, not that the incremental path never
// broke" technique the service-layer test uses), runs the CLI command
// against the data directory with no server involved, and confirms
// both the reported count and that a real FTS query against the
// rebuilt index finds the ticket by its actual title again.
func TestAdminSearchReindex(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)
	ctx := context.Background()
	actor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}

	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "RBD", Title: "Reindex Drill"}, actor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "RBD", Type: domain.TicketTypeTask, Title: "Findable widget",
	}, actor, "cid-2", "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	if _, err := st.DB().ExecContext(ctx, `UPDATE search_documents SET title = 'corrupted', body = 'corrupted'`); err != nil {
		t.Fatalf("corrupt search_documents: %v", err)
	}
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO search_fts(search_fts) VALUES('rebuild')`); err != nil {
		t.Fatalf("rebuild fts shadow table after direct corruption: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store before running the CLI command: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runAdmin([]string{"search-reindex", "--data-dir", dataDir}); err != nil {
			t.Fatalf("runAdmin search-reindex: %v", err)
		}
	})
	// The project itself and its auto-created General feature are
	// indexed too, so this is the one ticket plus that one feature plus
	// the project, not just the ticket.
	if !strings.Contains(out, "reindexed 3 search document(s)") {
		t.Errorf("search-reindex output = %q, want it to report reindexing 3 documents", out)
	}

	st2, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("re-open store: %v", err)
	}
	defer func() { _ = st2.Close() }()

	var hits int
	if err := st2.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM search_fts WHERE search_fts MATCH '"widget"'`,
	).Scan(&hits); err != nil {
		t.Fatalf("query rebuilt index: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits for the ticket's real title after reindex = %d, want 1", hits)
	}

	var corruptedHits int
	if err := st2.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM search_fts WHERE search_fts MATCH '"corrupted"'`,
	).Scan(&corruptedHits); err != nil {
		t.Fatalf("query for leftover corrupted text: %v", err)
	}
	if corruptedHits != 0 {
		t.Errorf("hits for the corrupted placeholder text after reindex = %d, want 0 (rebuild should have overwritten it)", corruptedHits)
	}
}
