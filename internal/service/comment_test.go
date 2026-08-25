package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func mustCreateTicket(t *testing.T, s *Service, projectKey, title string) domain.Ticket {
	t.Helper()
	ticket, err := s.CreateTicket(context.Background(), CreateTicketRequest{
		ProjectKey: projectKey, Type: domain.TicketTypeTask, Title: title,
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket %q: %v", title, err)
	}
	return ticket
}

// TestAddAndListComments confirms a comment round-trips through
// AddComment/GetComment/ListComments with the right author and body.
func TestAddAndListComments(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	c, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "First comment"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if c.Body != "First comment" || c.Author != testActor || c.Version != 1 {
		t.Fatalf("comment = %+v, want body=%q author=%v version=1", c, "First comment", testActor)
	}
	if c.DeletedAt != nil {
		t.Errorf("DeletedAt = %v, want nil for a fresh comment", c.DeletedAt)
	}

	got, err := s.GetComment(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetComment: %v", err)
	}
	if got != c {
		t.Errorf("GetComment = %+v, want %+v", got, c)
	}

	list, err := s.ListComments(ctx, ref)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("ListComments = %+v, want one comment with id %d", list, c.ID)
	}
}

// TestEditCommentArchivesPriorBody confirms an edit bumps
// comments.version, changes the live body, and archives the prior
// body into comment_versions (§5.10).
func TestEditCommentArchivesPriorBody(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	c, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "v1"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	edited, err := s.EditComment(ctx, EditCommentRequest{CommentID: c.ID, Body: "v2", ExpectedVersion: c.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("EditComment: %v", err)
	}
	if edited.Body != "v2" || edited.Version != 2 {
		t.Fatalf("edited comment = %+v, want body=v2 version=2", edited)
	}

	history, err := s.GetCommentHistory(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCommentHistory: %v", err)
	}
	if len(history) != 1 || history[0].Version != 1 || history[0].Body != "v1" {
		t.Fatalf("history = %+v, want one archived version=1 body=v1", history)
	}
	if history[0].EditedBy != testActor {
		t.Errorf("history[0].EditedBy = %v, want %v", history[0].EditedBy, testActor)
	}
}

// TestEditCommentVersionConflict confirms a stale ExpectedVersion is
// rejected with the comment's current version, not the parent
// ticket's entities.version (comments version independently).
func TestEditCommentVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	c, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "v1"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	_, err = s.EditComment(ctx, EditCommentRequest{CommentID: c.ID, Body: "v2", ExpectedVersion: c.Version + 1}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("EditComment with stale version = %v, want version_conflict", err)
	}
	if svcErr.CurrentVersion == nil || *svcErr.CurrentVersion != c.Version {
		t.Errorf("CurrentVersion = %v, want %d", svcErr.CurrentVersion, c.Version)
	}
}

// TestDeleteCommentTombstoneStaysVisible confirms a soft-deleted
// comment still appears in ListComments/GetComment with DeletedAt
// set, rather than vanishing (§5.10's "visible tombstone" — the
// opposite of how a soft-deleted ticket/feature behaves).
func TestDeleteCommentTombstoneStaysVisible(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	c, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "to be deleted"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	if err := s.DeleteComment(ctx, DeleteCommentRequest{CommentID: c.ID, ExpectedVersion: c.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}

	got, err := s.GetComment(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetComment after delete: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("DeletedAt = nil after delete, want set")
	}
	if got.Body != "to be deleted" {
		t.Errorf("Body after delete = %q, want body preserved for the audit trail", got.Body)
	}

	list, err := s.ListComments(ctx, ref)
	if err != nil {
		t.Fatalf("ListComments after delete: %v", err)
	}
	if len(list) != 1 || list[0].DeletedAt == nil {
		t.Fatalf("ListComments after delete = %+v, want the tombstoned comment still listed", list)
	}
}

// TestDeleteCommentTwiceIsNotFound confirms deletion isn't idempotent
// at the API level — a second delete on an already-tombstoned comment
// is not_found, mirroring RemoveRelationship's double-removal case.
func TestDeleteCommentTwiceIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	c, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "body"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := s.DeleteComment(ctx, DeleteCommentRequest{CommentID: c.ID, ExpectedVersion: c.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("first DeleteComment: %v", err)
	}

	err = s.DeleteComment(ctx, DeleteCommentRequest{CommentID: c.ID, ExpectedVersion: c.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("second DeleteComment = %v, want not_found", err)
	}
}

// TestEditDeletedCommentIsNotFound confirms a tombstoned comment
// cannot be edited.
func TestEditDeletedCommentIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	c, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "body"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := s.DeleteComment(ctx, DeleteCommentRequest{CommentID: c.ID, ExpectedVersion: c.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}

	_, err = s.EditComment(ctx, EditCommentRequest{CommentID: c.ID, Body: "new", ExpectedVersion: c.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("EditComment on deleted comment = %v, want not_found", err)
	}
}

// TestAddCommentOnEveryPrincipalEntityKind is Phase 6 Step 2's
// regression test for §5.10: "projects, features, tickets, decisions,
// plans, and documents can receive Markdown comments" — previously
// only tickets could (AddComment hard-coded store.GetTicketByRef).
// Drives AddComment/ListComments/EditComment/DeleteComment through all
// six kinds via one reference each, confirming resolveCommentOwner's
// project-vs-resolveAssociationEndpoint dispatch works uniformly.
func TestAddCommentOnEveryPrincipalEntityKind(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	ticket := mustCreateTicket(t, s, "ABC", "T")
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "F"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	decision, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "D"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "P", Body: "plan body"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(plan): %v", err)
	}
	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Doc", Body: "doc body"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(document): %v", err)
	}

	refs := map[string]domain.Reference{
		"project":  {ProjectKey: "ABC", Kind: domain.KindProject},
		"feature":  mustParseRef(t, feature.Ref),
		"ticket":   mustParseRef(t, ticket.Ref),
		"decision": mustParseRef(t, decision.Ref),
		"plan":     mustParseRef(t, plan.Ref),
		"document": mustParseRef(t, doc.Ref),
	}

	for name, ref := range refs {
		t.Run(name, func(t *testing.T) {
			c, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "hello " + name}, testActor, testCorrelationID, "", "")
			if err != nil {
				t.Fatalf("AddComment on %s: %v", name, err)
			}
			if c.Body != "hello "+name {
				t.Fatalf("comment body = %q, want %q", c.Body, "hello "+name)
			}

			list, err := s.ListComments(ctx, ref)
			if err != nil {
				t.Fatalf("ListComments on %s: %v", name, err)
			}
			if len(list) != 1 || list[0].ID != c.ID {
				t.Fatalf("ListComments on %s = %+v, want one comment with id %d", name, list, c.ID)
			}

			edited, err := s.EditComment(ctx, EditCommentRequest{CommentID: c.ID, Body: "edited " + name, ExpectedVersion: c.Version}, testActor, testCorrelationID)
			if err != nil {
				t.Fatalf("EditComment on %s: %v", name, err)
			}
			if edited.Body != "edited "+name {
				t.Fatalf("edited comment body = %q, want %q", edited.Body, "edited "+name)
			}

			if err := s.DeleteComment(ctx, DeleteCommentRequest{CommentID: c.ID, ExpectedVersion: edited.Version}, testActor, testCorrelationID); err != nil {
				t.Fatalf("DeleteComment on %s: %v", name, err)
			}
			got, err := s.GetComment(ctx, c.ID)
			if err != nil {
				t.Fatalf("GetComment after delete on %s: %v", name, err)
			}
			if got.DeletedAt == nil {
				t.Fatalf("DeletedAt after delete on %s = nil, want set", name)
			}
		})
	}
}

// TestAddCommentOnUnknownProjectIsNotFound confirms a project-kind
// reference to a nonexistent project key is not_found, the project
// half of resolveCommentOwner's dispatch.
func TestAddCommentOnUnknownProjectIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.AddComment(ctx, AddCommentRequest{Ref: domain.Reference{ProjectKey: "ZZZ", Kind: domain.KindProject}, Body: "x"}, testActor, testCorrelationID, "", "")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("AddComment on unknown project = %v, want not_found", err)
	}
}
