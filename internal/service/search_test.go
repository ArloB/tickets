package service

import (
	"context"
	"fmt"
	"strings"
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

// TestSearchFindsAttachmentAndLink is Step 9 close-out's regression
// test for product spec §6.3's "attachment names and link metadata"
// requirement (missed by Step 6, caught by Step 9's close-out audit).
func TestSearchFindsAttachmentAndLink(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ATL")
	ticket := mustCreateTicket(t, s, "ATL", "Host")
	ref, _ := domain.Parse(ticket.Ref)

	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		Ref: ref, Title: "spec draft", Kind: domain.AttachmentKindPath, PathValue: "/docs/gronkulator-spec.pdf",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateAttachment: %v", err)
	}
	if _, err := s.AddExternalLink(ctx, AddExternalLinkRequest{
		Ref: ref, Title: "Gronkulator tracker", URL: "https://example.com/gronkulator",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddExternalLink: %v", err)
	}

	result, err := s.Search(ctx, SearchRequest{Query: "gronkulator"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var sawAttachment, sawLink bool
	for _, h := range result.Hits {
		if h.Ref != ticket.Ref {
			continue
		}
		switch h.Kind {
		case "attachment":
			sawAttachment = true
			if h.CommentID != nil {
				t.Errorf("attachment hit has a comment_id (%d) — attachments must never carry one", *h.CommentID)
			}
		case "link":
			sawLink = true
			if h.CommentID != nil {
				t.Errorf("link hit has a comment_id (%d) — links must never carry one", *h.CommentID)
			}
		}
	}
	if !sawAttachment {
		t.Errorf("Search(%q) missing the attachment hit (path_value match): %+v", "gronkulator", result.Hits)
	}
	if !sawLink {
		t.Errorf("Search(%q) missing the link hit (title match): %+v", "gronkulator", result.Hits)
	}

	// The link's URL is searchable too, not just its title.
	byURL, err := s.Search(ctx, SearchRequest{Query: "example.com"})
	if err != nil {
		t.Fatalf("Search by URL: %v", err)
	}
	if len(byURL.Hits) != 1 || byURL.Hits[0].Kind != "link" {
		t.Fatalf("Search(%q) = %+v, want exactly one link hit", "example.com", byURL.Hits)
	}
}

// TestSearchAttachmentSurvivesItsCommentsSoftDelete confirms the
// documented DeleteSearchDocumentForComment behavior: a comment-
// attached attachment's search row is not removed just because the
// comment carrying it was later soft-deleted — the attachment record
// itself is unaffected and still reachable via
// GET /comments/{id}/attachments.
func TestSearchAttachmentSurvivesItsCommentsSoftDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ACS")
	ticket := mustCreateTicket(t, s, "ACS", "Host")
	ref, _ := domain.Parse(ticket.Ref)

	comment, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "see attached"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		CommentID: comment.ID, Title: "wombat photo", Kind: domain.AttachmentKindPath, PathValue: "/tmp/wombat.png",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateAttachment: %v", err)
	}

	if err := s.DeleteComment(ctx, DeleteCommentRequest{CommentID: comment.ID, ExpectedVersion: 1}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}

	result, err := s.Search(ctx, SearchRequest{Query: "wombat"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Kind != "attachment" || result.Hits[0].Ref != ticket.Ref {
		t.Fatalf("Search(%q) after the comment's soft-delete = %+v, want the attachment hit still present", "wombat", result.Hits)
	}
}

// TestSearchAttachmentAndLinkDropOutOnEntityDeleteThenRebuildAgrees is
// the differential test: an incremental index (create, delete) and a
// from-scratch RebuildSearchIndex must agree exactly, including for
// the two kinds Step 9 close-out added — the class of bug a
// rebuild/incremental divergence produces is silent (both "work" in
// isolation, they just disagree), so this compares the raw
// search_documents rows rather than only asserting hit counts.
func TestSearchAttachmentAndLinkDropOutOnEntityDeleteThenRebuildAgrees(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ARB")
	ticket := mustCreateTicket(t, s, "ARB", "Host")
	ref, _ := domain.Parse(ticket.Ref)

	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		Ref: ref, Title: "kept", Kind: domain.AttachmentKindPath, PathValue: "/tmp/kept.txt",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateAttachment (kept): %v", err)
	}
	if _, err := s.AddExternalLink(ctx, AddExternalLinkRequest{
		Ref: ref, Title: "kept link", URL: "https://example.com/kept",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddExternalLink (kept): %v", err)
	}
	// A comment-attached attachment resolves its owner differently in
	// each path (attachmentAuditEntityID's store.GetComment hop
	// incrementally, the LEFT JOIN comments + owners map on rebuild) —
	// exercise it here so a divergence between those two
	// implementations shows up in the snapshot diff.
	keptComment, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "see attached"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment (kept): %v", err)
	}
	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		CommentID: keptComment.ID, Title: "kept comment attachment", Kind: domain.AttachmentKindPath, PathValue: "/tmp/kept-comment.txt",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateAttachment (kept comment): %v", err)
	}

	doomed := mustCreateTicket(t, s, "ARB", "Doomed")
	doomedRef, _ := domain.Parse(doomed.Ref)
	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		Ref: doomedRef, Title: "gone", Kind: domain.AttachmentKindPath, PathValue: "/tmp/gone.txt",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateAttachment (doomed): %v", err)
	}
	if _, err := s.AddExternalLink(ctx, AddExternalLinkRequest{
		Ref: doomedRef, Title: "gone link", URL: "https://example.com/gone",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddExternalLink (doomed): %v", err)
	}
	doomedComment, err := s.AddComment(ctx, AddCommentRequest{Ref: doomedRef, Body: "see attached"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment (doomed): %v", err)
	}
	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		CommentID: doomedComment.ID, Title: "gone comment attachment", Kind: domain.AttachmentKindPath, PathValue: "/tmp/gone-comment.txt",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateAttachment (doomed comment): %v", err)
	}
	if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: doomedRef, ExpectedVersion: 1}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteTicket (doomed): %v", err)
	}

	snapshot := func() []string {
		t.Helper()
		rows, err := s.store.DB().QueryContext(ctx, `
			SELECT source_kind, source_id, entity_id, kind, ref, title, body
			FROM search_documents ORDER BY source_kind, source_id`)
		if err != nil {
			t.Fatalf("query search_documents: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var sourceKind, kind, ref, title, body string
			var sourceID, entityID int64
			if err := rows.Scan(&sourceKind, &sourceID, &entityID, &kind, &ref, &title, &body); err != nil {
				t.Fatalf("scan search_documents row: %v", err)
			}
			out = append(out, fmt.Sprintf("%s/%d entity=%d kind=%s ref=%s title=%q body=%q", sourceKind, sourceID, entityID, kind, ref, title, body))
		}
		return out
	}

	before := snapshot()

	// The doomed ticket's attachment/link must already be gone from
	// the incremental index (removeEntitySearchDocs's cascade).
	for _, row := range before {
		if strings.Contains(row, `title="gone"`) || strings.Contains(row, `title="gone link"`) {
			t.Errorf("incremental index still has the doomed ticket's search row: %s", row)
		}
	}

	if _, err := s.RebuildSearchIndex(ctx); err != nil {
		t.Fatalf("RebuildSearchIndex: %v", err)
	}
	after := snapshot()

	if len(before) != len(after) {
		t.Fatalf("rebuild disagrees with the incremental index: %d rows before, %d after\nbefore: %v\nafter:  %v", len(before), len(after), before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("rebuild disagrees with the incremental index at row %d:\nincremental: %s\nrebuilt:     %s", i, before[i], after[i])
		}
	}
}

// TestSearchAttachmentAndLinkReindexedOnTicketRestore is a regression
// test for a gap the differential test above can't see (it never
// restores): RestoreTicket already reindexes the ticket itself and
// its comments (reindexCommentsForEntity), but a delete's
// removeEntitySearchDocs also clears the ticket's own attachments and
// links — restore must bring those back too, or they stay
// unsearchable until an admin runs search-reindex.
func TestSearchAttachmentAndLinkReindexedOnTicketRestore(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "RES")
	ticket := mustCreateTicket(t, s, "RES", "Host")
	ref, _ := domain.Parse(ticket.Ref)

	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		Ref: ref, Title: "restorable", Kind: domain.AttachmentKindPath, PathValue: "/tmp/restorable-widget.txt",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("CreateAttachment: %v", err)
	}
	if _, err := s.AddExternalLink(ctx, AddExternalLinkRequest{
		Ref: ref, Title: "restorable-widget link", URL: "https://example.com/restorable-widget",
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddExternalLink: %v", err)
	}

	newVersion, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: 1}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if gone, err := s.Search(ctx, SearchRequest{Query: "restorable-widget"}); err != nil || len(gone.Hits) != 0 {
		t.Fatalf("Search after delete = %+v, %v; want no hits", gone, err)
	}

	if _, err := s.RestoreTicket(ctx, RestoreTicketRequest{Ref: ref, ExpectedVersion: newVersion}, testActor, testCorrelationID); err != nil {
		t.Fatalf("RestoreTicket: %v", err)
	}

	result, err := s.Search(ctx, SearchRequest{Query: "restorable-widget"})
	if err != nil {
		t.Fatalf("Search after restore: %v", err)
	}
	var sawAttachment, sawLink bool
	for _, h := range result.Hits {
		switch h.Kind {
		case "attachment":
			sawAttachment = true
		case "link":
			sawLink = true
		}
	}
	if !sawAttachment || !sawLink {
		t.Fatalf("Search after restore = %+v, want both the attachment and link hits back (attachment=%v link=%v)", result.Hits, sawAttachment, sawLink)
	}
}

// TestSearchFindsCommentsOnNonTicketEntities is Phase 6 Step 2's
// search-side regression test: mvp-acceptance.md's row 12 note says a
// non-ticket comment must be verified findable once comments widen
// past tickets. Adds a comment to a project and to a plan and confirms
// both are searchable, mirroring
// TestSearchFindsTicketFeatureDecisionAndComment's ticket-comment case.
func TestSearchFindsCommentsOnNonTicketEntities(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "NTC")

	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "NTC", Kind: domain.KindPlan, Title: "Rollout plan", Body: "steps",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}

	projectComment, err := s.AddComment(ctx, AddCommentRequest{
		Ref: domain.Reference{ProjectKey: "NTC", Kind: domain.KindProject}, Body: "glorbnaxian status update",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment on project: %v", err)
	}
	planComment, err := s.AddComment(ctx, AddCommentRequest{
		Ref: mustParseRef(t, plan.Ref), Body: "glorbnaxian rollout note",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment on plan: %v", err)
	}

	result, err := s.Search(ctx, SearchRequest{Query: "glorbnaxian"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var sawProjectComment, sawPlanComment bool
	for _, h := range result.Hits {
		if h.Kind != "comment" || h.CommentID == nil {
			continue
		}
		switch *h.CommentID {
		case projectComment.ID:
			sawProjectComment = h.Ref == "NTC"
		case planComment.ID:
			sawPlanComment = h.Ref == plan.Ref
		}
	}
	if !sawProjectComment || !sawPlanComment {
		t.Fatalf("Search(%q) = %+v, want hits for both the project comment (id %d) and the plan comment (id %d)", "glorbnaxian", result.Hits, projectComment.ID, planComment.ID)
	}
}
