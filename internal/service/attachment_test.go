package service

import (
	"context"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateAttachmentOnEveryPrincipalKind(t *testing.T) {
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
	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "P"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(plan): %v", err)
	}
	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Doc"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(document): %v", err)
	}

	for _, refStr := range []string{ticket.Ref, feature.Ref, decision.Ref, plan.Ref, doc.Ref} {
		ref, err := domain.Parse(refStr)
		if err != nil {
			t.Fatalf("parse ref %q: %v", refStr, err)
		}
		a, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
			Ref: ref, Title: "notes for " + refStr, Kind: domain.AttachmentKindUpload,
			Content: strings.NewReader("bytes for " + refStr), FileName: "notes.txt",
		}, testActor, testCorrelationID)
		if err != nil {
			t.Fatalf("CreateAttachment on %s: %v", refStr, err)
		}
		if a.OwnerRef != refStr {
			t.Errorf("OwnerRef = %q, want %q", a.OwnerRef, refStr)
		}
		if a.CurrentVersion != 1 {
			t.Errorf("CurrentVersion = %d, want 1", a.CurrentVersion)
		}

		listed, err := s.ListAttachmentsForRef(ctx, ref)
		if err != nil {
			t.Fatalf("ListAttachmentsForRef(%s): %v", refStr, err)
		}
		if len(listed) != 1 || listed[0].ID != a.ID {
			t.Errorf("ListAttachmentsForRef(%s) = %+v, want exactly the one just created", refStr, listed)
		}
	}
}

func TestCreateAttachmentOnComment(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, _ := domain.Parse(ticket.Ref)

	comment, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "see attached"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	a, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		CommentID: comment.ID, Title: "screenshot", Kind: domain.AttachmentKindUpload,
		Content: strings.NewReader("png bytes"), FileName: "shot.png",
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAttachment on comment: %v", err)
	}
	if a.OwnerRef != "" {
		t.Errorf("OwnerRef = %q, want empty for a comment-scoped attachment", a.OwnerRef)
	}
	if a.CommentID != comment.ID {
		t.Errorf("CommentID = %d, want %d", a.CommentID, comment.ID)
	}

	listed, err := s.ListAttachmentsForComment(ctx, comment.ID)
	if err != nil {
		t.Fatalf("ListAttachmentsForComment: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != a.ID {
		t.Errorf("ListAttachmentsForComment = %+v, want exactly the one just created", listed)
	}

	// The ticket's own attachment list must not include the comment's.
	ticketAttachments, err := s.ListAttachmentsForRef(ctx, ref)
	if err != nil {
		t.Fatalf("ListAttachmentsForRef: %v", err)
	}
	if len(ticketAttachments) != 0 {
		t.Errorf("ListAttachmentsForRef(ticket) = %+v, want none (the attachment belongs to the comment)", ticketAttachments)
	}
}

func TestCreateAttachmentRejectsBothOrNeitherOwner(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, _ := domain.Parse(ticket.Ref)
	comment, err := s.AddComment(ctx, AddCommentRequest{Ref: ref, Body: "x"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	// Neither.
	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		Title: "x", Kind: domain.AttachmentKindPath, PathValue: "/tmp/x",
	}, testActor, testCorrelationID); !isValidationError(err, "owner") {
		t.Errorf("CreateAttachment with neither ref nor comment id: err = %v, want a validation error on \"owner\"", err)
	}

	// Both.
	if _, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		Ref: ref, CommentID: comment.ID, Title: "x", Kind: domain.AttachmentKindPath, PathValue: "/tmp/x",
	}, testActor, testCorrelationID); !isValidationError(err, "owner") {
		t.Errorf("CreateAttachment with both ref and comment id: err = %v, want a validation error on \"owner\"", err)
	}
}

// TestAttachmentEventsAppearInActivityFeed confirms attachment_added/
// _replaced/_removed are in the activity feed's allowlist, not
// silently dropped by omission the way a purely-internal event type
// would be.
func TestAttachmentEventsAppearInActivityFeed(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket := mustCreateTicket(t, s, "ABC", "T")
	ref, _ := domain.Parse(ticket.Ref)

	a, err := s.CreateAttachment(ctx, CreateAttachmentRequest{
		Ref: ref, Title: "notes", Kind: domain.AttachmentKindPath, PathValue: "/tmp/notes.txt",
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAttachment: %v", err)
	}
	if err := s.DeleteAttachment(ctx, DeleteAttachmentRequest{ID: a.ID, ExpectedVersion: a.CurrentVersion}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}

	feed, err := s.ListActivity(ctx, "ABC", ActivityListFilters{}, 50, "")
	if err != nil {
		t.Fatalf("ListActivity: %v", err)
	}
	var sawAdded, sawRemoved bool
	for _, e := range feed.Events {
		if e.EventType == eventAttachmentAdded {
			sawAdded = true
		}
		if e.EventType == eventAttachmentRemoved {
			sawRemoved = true
		}
	}
	if !sawAdded {
		t.Error("activity feed missing attachment_added event")
	}
	if !sawRemoved {
		t.Error("activity feed missing attachment_removed event")
	}
}
