package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestSearchFindsTicketFeatureDecisionAndComment(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "SRC", Title: "Search"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "SRC", Type: domain.TicketTypeBug, Title: "Reticulate splines", Description: "the renderer is slow",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	if _, err := s.CreateFeature(ctx, CreateFeatureRequest{
		ProjectKey: "SRC", Title: "Spline reticulation", Description: "batch of related work",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("create feature: %v", err)
	}

	if _, err := s.CreateDecision(ctx, CreateDecisionRequest{
		ProjectKey: "SRC", Title: "Use quadratic splines", Decision: "we reticulate quadratically now",
	}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create decision: %v", err)
	}

	comment, err := s.AddComment(ctx, AddCommentRequest{Ref: mustParseRef(t, ticket.Ref), Body: "spline reticulation looks fixed"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}

	result, err := s.Search(ctx, SearchRequest{Query: "reticulate"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Hits) != 4 {
		t.Fatalf("Search(%q) returned %d hits, want 4: %+v", "reticulate", len(result.Hits), result.Hits)
	}
	var sawTicket, sawFeature, sawDecision, sawComment bool
	for _, h := range result.Hits {
		switch h.Kind {
		case "ticket":
			sawTicket = h.Ref == ticket.Ref
		case "feature":
			sawFeature = true
		case "decision":
			sawDecision = true
		case "comment":
			sawComment = h.CommentID != nil && *h.CommentID == comment.ID && h.Ref == ticket.Ref
		}
	}
	if !sawTicket || !sawFeature || !sawDecision || !sawComment {
		t.Fatalf("missing expected hit kinds: ticket=%v feature=%v decision=%v comment=%v (hits=%+v)", sawTicket, sawFeature, sawDecision, sawComment, result.Hits)
	}
}

func TestSearchDeletedTicketAndCommentsDropOutThenRestoreReindexes(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "DEL", Title: "Deletion"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "DEL", Type: domain.TicketTypeTask, Title: "Widget overhaul"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: mustParseRef(t, ticket.Ref), Body: "widget overhaul comment"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("add comment: %v", err)
	}

	before, err := s.Search(ctx, SearchRequest{Query: "widget"})
	if err != nil || len(before.Hits) != 2 {
		t.Fatalf("Search before delete = %+v, %v; want 2 hits", before, err)
	}

	newVersion, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: mustParseRef(t, ticket.Ref), ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("delete ticket: %v", err)
	}
	afterDelete, err := s.Search(ctx, SearchRequest{Query: "widget"})
	if err != nil || len(afterDelete.Hits) != 0 {
		t.Fatalf("Search after delete = %+v, %v; want 0 hits (ticket and its comment should both drop out)", afterDelete, err)
	}

	if _, err := s.RestoreTicket(ctx, RestoreTicketRequest{Ref: mustParseRef(t, ticket.Ref), ExpectedVersion: newVersion}, testActor, testCorrelationID); err != nil {
		t.Fatalf("restore ticket: %v", err)
	}
	afterRestore, err := s.Search(ctx, SearchRequest{Query: "widget"})
	if err != nil || len(afterRestore.Hits) != 2 {
		t.Fatalf("Search after restore = %+v, %v; want 2 hits (ticket and its comment should both come back)", afterRestore, err)
	}
}

func TestSearchFeatureCascadeDeleteRemovesDependentTicketAndItsComments(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "CAS", Title: "Cascade"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "CAS", Title: "Doomed feature"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create feature: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "CAS", Type: domain.TicketTypeTask, Title: "Cascade ticket"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if _, err := s.MoveTicketFeature(ctx, MoveTicketFeatureRequest{Ref: mustParseRef(t, ticket.Ref), NewFeatureRef: mustParseRef(t, feature.Ref), ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("move ticket into feature: %v", err)
	}
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: mustParseRef(t, ticket.Ref), Body: "cascade ticket comment"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("add comment: %v", err)
	}

	before, err := s.Search(ctx, SearchRequest{Query: "cascade"})
	if err != nil || len(before.Hits) != 2 {
		t.Fatalf("Search before cascade delete = %+v, %v; want 2 hits (ticket + comment)", before, err)
	}

	if _, err := s.DeleteFeature(ctx, DeleteFeatureRequest{Ref: mustParseRef(t, feature.Ref), Cascade: true, ExpectedVersion: feature.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("cascade-delete feature: %v", err)
	}

	after, err := s.Search(ctx, SearchRequest{Query: "cascade"})
	if err != nil || len(after.Hits) != 0 {
		t.Fatalf("Search after cascade delete = %+v, %v; want 0 hits (cascade-deleted ticket and its comment must both leave the index)", after, err)
	}
}

func TestSearchRebuildIndexRepopulatesFromScratch(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "RBD", Title: "Rebuild"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "RBD", Type: domain.TicketTypeTask, Title: "Rebuildable widget"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	// Corrupt the index directly to prove rebuild actually recomputes
	// rather than trivially passing because the incremental path
	// already left it correct.
	if _, err := s.store.DB().ExecContext(ctx, `UPDATE search_documents SET title = 'corrupted', body = 'corrupted'`); err != nil {
		t.Fatalf("corrupt search_documents: %v", err)
	}
	if _, err := s.store.DB().ExecContext(ctx, `INSERT INTO search_fts(search_fts) VALUES('rebuild')`); err != nil {
		t.Fatalf("rebuild fts shadow table after direct corruption: %v", err)
	}

	corrupted, err := s.Search(ctx, SearchRequest{Query: "widget"})
	if err != nil || len(corrupted.Hits) != 0 {
		t.Fatalf("Search after corrupting index = %+v, %v; want 0 hits", corrupted, err)
	}

	count, err := s.RebuildSearchIndex(ctx)
	if err != nil {
		t.Fatalf("RebuildSearchIndex: %v", err)
	}
	// The project's auto-created General feature is indexed too, so
	// this is the one ticket plus that one feature, not just the ticket.
	if count != 2 {
		t.Fatalf("RebuildSearchIndex count = %d, want 2", count)
	}

	rebuilt, err := s.Search(ctx, SearchRequest{Query: "widget"})
	if err != nil || len(rebuilt.Hits) != 1 {
		t.Fatalf("Search after rebuild = %+v, %v; want 1 hit", rebuilt, err)
	}
}

func TestSearchRejectsInvalidCursorAndUnsanitizableEmptyQuery(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.Search(ctx, SearchRequest{Query: "   "}); err == nil {
		t.Fatalf("expected validation error for a whitespace-only query")
	}
	if _, err := s.Search(ctx, SearchRequest{Query: "widget", Cursor: "not-a-valid-cursor!!"}); err == nil {
		t.Fatalf("expected validation error for a malformed cursor")
	}
	if _, err := s.Search(ctx, SearchRequest{Query: "widget", Kinds: []string{"bogus"}}); err == nil {
		t.Fatalf("expected validation error for an invalid kind")
	}
}
