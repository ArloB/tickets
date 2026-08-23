package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// containsRef reports whether refs contains a reference with the
// given formatted wire form.
func containsRef(t *testing.T, refs []domain.Reference, want string) bool {
	t.Helper()
	for _, r := range refs {
		got, err := domain.Format(r)
		if err != nil {
			t.Fatalf("format ref %+v: %v", r, err)
		}
		if got == want {
			return true
		}
	}
	return false
}

// TestTicketDescriptionMentionsProduceEdges is verification gate 7:
// a description mentioning three resolvable tickets in three
// different forms (explicit #ABC-2, bare ABC-3, project-scoped short
// form #4) produces exactly three mention edges — a reference inside
// a fenced code block or inline code span, and a reference to a
// ticket that doesn't exist, are both excluded, tested as two
// separate assertions so the two exclusion reasons aren't conflated
// into one count.
func TestTicketDescriptionMentionsProduceEdges(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	// t2/t3/t4 land on ABC-1/ABC-2/ABC-3 (this is the first project's
	// only tickets so far) — the mention-bearing ticket created below
	// is ABC-4, so referencing t2/t3/t4 by their actual refs (rather
	// than hardcoding "ABC-2"/"ABC-3"/etc.) avoids an accidental
	// self-mention that a hardcoded literal could silently collide with.
	t2 := mustCreateTicket(t, s, "ABC", "Two")
	t3 := mustCreateTicket(t, s, "ABC", "Three")
	t4 := mustCreateTicket(t, s, "ABC", "Four")
	t4Ref, err := domain.Parse(t4.Ref)
	if err != nil {
		t.Fatalf("parse t4 ref: %v", err)
	}

	body := fmt.Sprintf("See #%s explicitly, bare %s too, and short form #%d.\n"+
		"Not this inline: `%s` (code span).\n"+
		"Not this fenced:\n```\n%s\n```\n"+
		"Also mentions #ABC-99 which does not exist.",
		t2.Ref, t3.Ref, t4Ref.Seq, t2.Ref, t3.Ref)
	t5, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Five", Description: body}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket with mentions: %v", err)
	}
	ref5, err := domain.Parse(t5.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	mentions, err := s.GetTicketMentions(ctx, ref5)
	if err != nil {
		t.Fatalf("GetTicketMentions: %v", err)
	}
	if len(mentions) != 3 {
		t.Fatalf("mentions = %+v, want exactly 3", mentions)
	}
	for _, want := range []string{t2.Ref, t3.Ref, t4.Ref} {
		if !containsRef(t, mentions, want) {
			t.Errorf("mentions %+v missing %q", mentions, want)
		}
	}
	if containsRef(t, mentions, "ABC-99") {
		t.Errorf("mentions %+v should not include the unresolvable ABC-99", mentions)
	}

	// Editing the body to remove the t2 mention drops exactly that
	// edge, nothing else.
	newBody := fmt.Sprintf("bare %s too, and short form #%d.", t3.Ref, t4Ref.Seq)
	_, err = s.UpdateTicketFields(ctx, UpdateTicketFieldsRequest{
		Ref: ref5, Type: domain.TicketTypeTask, Title: "Five", Description: newBody,
		Priority: domain.PriorityMedium, ExpectedVersion: t5.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateTicketFields: %v", err)
	}

	mentionsAfter, err := s.GetTicketMentions(ctx, ref5)
	if err != nil {
		t.Fatalf("GetTicketMentions after edit: %v", err)
	}
	if len(mentionsAfter) != 2 {
		t.Fatalf("mentions after edit = %+v, want exactly 2", mentionsAfter)
	}
	if containsRef(t, mentionsAfter, t2.Ref) {
		t.Errorf("mentions after edit = %+v, should no longer include %q", mentionsAfter, t2.Ref)
	}
}

// TestTicketDescriptionSelfMentionSkipped confirms a ticket mentioning
// its own reference in its own description does not create a
// self-edge — the primary key wouldn't reject it on its own, so this
// is testing rescanMentions' explicit guard.
func TestTicketDescriptionSelfMentionSkipped(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	t1 := mustCreateTicket(t, s, "ABC", "One")
	ref1, err := domain.Parse(t1.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	_, err = s.UpdateTicketFields(ctx, UpdateTicketFieldsRequest{
		Ref: ref1, Type: domain.TicketTypeTask, Title: "One", Description: "See " + t1.Ref + " (itself).",
		Priority: domain.PriorityMedium, ExpectedVersion: t1.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateTicketFields: %v", err)
	}

	mentions, err := s.GetTicketMentions(ctx, ref1)
	if err != nil {
		t.Fatalf("GetTicketMentions: %v", err)
	}
	if len(mentions) != 0 {
		t.Fatalf("mentions = %+v, want none (self-mention must be skipped)", mentions)
	}
}

// TestCommentMentionsRescanOnEditAndClearOnDelete exercises the
// comment-body half of ADR 0015: a comment's mentions are scanned on
// add, rescanned on edit, and cleared entirely when the comment is
// soft-deleted (its backlinks shouldn't stay live once tombstoned).
func TestCommentMentionsRescanOnEditAndClearOnDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	target := mustCreateTicket(t, s, "ABC", "Target")
	host := mustCreateTicket(t, s, "ABC", "Host")
	hostRef, err := domain.Parse(host.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	c, err := s.AddComment(ctx, AddCommentRequest{Ref: hostRef, Body: "mentions " + target.Ref}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	mentions, err := s.GetCommentMentions(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCommentMentions: %v", err)
	}
	if len(mentions) != 1 || !containsRef(t, mentions, target.Ref) {
		t.Fatalf("mentions after add = %+v, want one edge to %q", mentions, target.Ref)
	}

	edited, err := s.EditComment(ctx, EditCommentRequest{CommentID: c.ID, Body: "no mention here", ExpectedVersion: c.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("EditComment: %v", err)
	}
	mentionsAfterEdit, err := s.GetCommentMentions(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCommentMentions after edit: %v", err)
	}
	if len(mentionsAfterEdit) != 0 {
		t.Fatalf("mentions after edit = %+v, want none", mentionsAfterEdit)
	}

	// Re-add the mention, then confirm delete clears it.
	edited, err = s.EditComment(ctx, EditCommentRequest{CommentID: c.ID, Body: "mentions " + target.Ref + " again", ExpectedVersion: edited.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("EditComment (re-add mention): %v", err)
	}
	if err := s.DeleteComment(ctx, DeleteCommentRequest{CommentID: c.ID, ExpectedVersion: edited.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	mentionsAfterDelete, err := s.GetCommentMentions(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCommentMentions after delete: %v", err)
	}
	if len(mentionsAfterDelete) != 0 {
		t.Fatalf("mentions after delete = %+v, want none (tombstoned comment's backlinks must not stay live)", mentionsAfterDelete)
	}
}
