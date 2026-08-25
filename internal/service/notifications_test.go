package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

var testActorAlice = domain.ActorRef{Kind: domain.ActorHuman, Name: "alice"}

// seedHumanActor inserts a bare human actor with no login credentials
// — AssignTicket/Subscribe/notify all only need a resolvable actors
// row (store.GetActorIDByRef), not a sign-in-capable account; this
// codebase has no public-registration service method to create one
// the ordinary way (§3.2's non-goals), so tests that need a second
// distinct human actor insert one directly the way CreateAgent's own
// implementation does for agents (store.CreateActor).
func seedHumanActor(t *testing.T, s *Service, name string) {
	t.Helper()
	if _, err := store.CreateActor(context.Background(), s.store.DB(), domain.ActorHuman, name, "", nil, store.Now()); err != nil {
		t.Fatalf("seed human actor %q: %v", name, err)
	}
}

func TestAssignTicketNotifiesNewAssigneeNotSelf(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "NOT", Title: "Notify"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "NOT", Type: domain.TicketTypeTask, Title: "Assign me"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	if _, err := s.AssignTicket(ctx, AssignTicketRequest{
		Ref: mustParseRef(t, ticket.Ref), Assignee: &testActorAlice, ExpectedVersion: ticket.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("assign ticket: %v", err)
	}

	result, err := s.ListNotifications(ctx, testActorAlice, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(result.Notifications) != 1 || result.Notifications[0].Kind != notificationKindAssigned {
		t.Fatalf("alice's notifications = %+v, want exactly one 'assigned'", result.Notifications)
	}
	if result.Notifications[0].Entity != ticket.Ref {
		t.Errorf("notification entity = %q, want %q", result.Notifications[0].Entity, ticket.Ref)
	}

	// Self-assignment must not notify the assigning actor.
	updated, err := s.GetTicket(ctx, mustParseRef(t, ticket.Ref))
	if err != nil {
		t.Fatalf("get ticket: %v", err)
	}
	if _, err := s.AssignTicket(ctx, AssignTicketRequest{
		Ref: mustParseRef(t, ticket.Ref), Assignee: &testActor, ExpectedVersion: updated.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("self-assign ticket: %v", err)
	}
	selfResult, err := s.ListNotifications(ctx, testActor, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications for self: %v", err)
	}
	for _, n := range selfResult.Notifications {
		if n.Kind == notificationKindAssigned {
			t.Errorf("self-assignment produced a notification for the assigning actor: %+v", n)
		}
	}
}

func TestActorMentionInDescriptionNotifiesMentionedActor(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "MEN", Title: "Mentions"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "MEN", Type: domain.TicketTypeTask, Title: "Needs review", Description: "cc @human:alice please take a look",
	}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	result, err := s.ListNotifications(ctx, testActorAlice, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(result.Notifications) != 1 || result.Notifications[0].Kind != notificationKindMentioned {
		t.Fatalf("alice's notifications = %+v, want exactly one 'mentioned'", result.Notifications)
	}
}

func TestSelfActorMentionDoesNotNotify(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "SLF", Title: "Self mention"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "SLF", Type: domain.TicketTypeTask, Title: "T", Description: "note to self @human:local",
	}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	result, err := s.ListNotifications(ctx, testActor, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	for _, n := range result.Notifications {
		if n.Kind == notificationKindMentioned {
			t.Errorf("self-mention produced a notification: %+v", n)
		}
	}
}

func TestCommentingSubscribesAndNotifiesExistingSubscribersNotTheCommenter(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "CMT", Title: "Comment"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	// testActor creates the ticket, auto-subscribing itself.
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "CMT", Type: domain.TicketTypeTask, Title: "Discuss"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	// alice comments — should notify testActor (the creator/subscriber),
	// and should subscribe alice too, but not notify alice about her own comment.
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: mustParseRef(t, ticket.Ref), Body: "first reply"}, testActorAlice, testCorrelationID, "", ""); err != nil {
		t.Fatalf("alice comments: %v", err)
	}

	creatorNotifs, err := s.ListNotifications(ctx, testActor, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications for creator: %v", err)
	}
	if len(creatorNotifs.Notifications) != 1 || creatorNotifs.Notifications[0].Kind != notificationKindCommented {
		t.Fatalf("creator's notifications = %+v, want exactly one 'commented'", creatorNotifs.Notifications)
	}

	aliceNotifs, err := s.ListNotifications(ctx, testActorAlice, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications for alice: %v", err)
	}
	if len(aliceNotifs.Notifications) != 0 {
		t.Fatalf("alice's own comment notified alice: %+v", aliceNotifs.Notifications)
	}

	subscribed, err := s.IsSubscribed(ctx, mustParseRef(t, ticket.Ref), testActorAlice)
	if err != nil || !subscribed {
		t.Fatalf("alice's subscription after commenting = %v, %v; want true", subscribed, err)
	}

	// testActor comments again on their own subscribed ticket: no
	// self-notification.
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: mustParseRef(t, ticket.Ref), Body: "second reply"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("creator comments again: %v", err)
	}
	creatorNotifs2, err := s.ListNotifications(ctx, testActor, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications for creator after own comment: %v", err)
	}
	if len(creatorNotifs2.Notifications) != 1 {
		t.Fatalf("creator's own second comment notified themselves: %+v", creatorNotifs2.Notifications)
	}
}

func TestUpdateTicketStatusNotifiesSubscribersNotTheActor(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "CHG", Title: "Changed"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "CHG", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if err := s.Subscribe(ctx, SubscribeRequest{Ref: mustParseRef(t, ticket.Ref)}, testActorAlice, testCorrelationID); err != nil {
		t.Fatalf("alice subscribes: %v", err)
	}

	if _, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{
		Ref: mustParseRef(t, ticket.Ref), NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: ticket.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("update status: %v", err)
	}

	aliceNotifs, err := s.ListNotifications(ctx, testActorAlice, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications for alice: %v", err)
	}
	if len(aliceNotifs.Notifications) != 1 || aliceNotifs.Notifications[0].Kind != notificationKindChanged {
		t.Fatalf("alice's notifications = %+v, want exactly one 'changed'", aliceNotifs.Notifications)
	}

	creatorNotifs, err := s.ListNotifications(ctx, testActor, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications for creator: %v", err)
	}
	for _, n := range creatorNotifs.Notifications {
		if n.Kind == notificationKindChanged {
			t.Errorf("the acting actor was notified of their own change: %+v", n)
		}
	}
}

func TestUnsubscribeStopsFutureNotifications(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "UNS", Title: "Unsubscribe"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "UNS", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	subscribed, err := s.IsSubscribed(ctx, mustParseRef(t, ticket.Ref), testActor)
	if err != nil || !subscribed {
		t.Fatalf("creator's subscription = %v, %v; want true (auto-subscribed on create)", subscribed, err)
	}

	if err := s.Unsubscribe(ctx, SubscribeRequest{Ref: mustParseRef(t, ticket.Ref)}, testActor, testCorrelationID); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	subscribed, err = s.IsSubscribed(ctx, mustParseRef(t, ticket.Ref), testActor)
	if err != nil || subscribed {
		t.Fatalf("creator's subscription after unsubscribe = %v, %v; want false", subscribed, err)
	}

	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: mustParseRef(t, ticket.Ref), Body: "reply from alice"}, testActorAlice, testCorrelationID, "", ""); err != nil {
		t.Fatalf("alice comments: %v", err)
	}
	notifs, err := s.ListNotifications(ctx, testActor, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(notifs.Notifications) != 0 {
		t.Fatalf("unsubscribed creator still got notified: %+v", notifs.Notifications)
	}
}

func TestMarkNotificationsReadByIDAndAll(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "RD1", Title: "Read"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "RD1", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if _, err := s.AssignTicket(ctx, AssignTicketRequest{Ref: mustParseRef(t, ticket.Ref), Assignee: &testActorAlice, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	before, err := s.ListNotifications(ctx, testActorAlice, false, 10, "")
	if err != nil || len(before.Notifications) != 1 {
		t.Fatalf("setup listing: %+v, %v", before, err)
	}

	n, err := s.MarkNotificationsRead(ctx, MarkNotificationsReadRequest{IDs: []int64{before.Notifications[0].ID}}, testActorAlice, testCorrelationID)
	if err != nil || n != 1 {
		t.Fatalf("MarkNotificationsRead by id = %d, %v; want 1", n, err)
	}
	unread, err := s.ListNotifications(ctx, testActorAlice, true, 10, "")
	if err != nil || len(unread.Notifications) != 0 {
		t.Fatalf("unread after marking read = %+v, %v; want 0", unread, err)
	}

	n, err = s.MarkNotificationsRead(ctx, MarkNotificationsReadRequest{All: true}, testActorAlice, testCorrelationID)
	if err != nil || n != 0 {
		t.Fatalf("MarkNotificationsRead all with nothing unread = %d, %v; want 0", n, err)
	}
}

// TestCommentOnProjectNotifiesSubscriberWithEmptyEntityRef is Phase 6
// Step 2's regression test for toNotification/activityEntityRef: a
// project is a new kind of comment owner (comments were ticket-only
// before this phase), and activityEntityRef already special-cases
// domain.KindProject to return "" rather than calling domain.Format
// (which rejects KindProject) — this confirms that path is actually
// exercised end to end (ListNotifications doesn't error) rather than
// just trusting the existing dispatch code.
func TestCommentOnProjectNotifiesSubscriberWithEmptyEntityRef(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	seedHumanActor(t, s, "alice")

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "PJC", Title: "Project comments"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectRef := domain.Reference{ProjectKey: "PJC", Kind: domain.KindProject}
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: projectRef, Body: "kickoff notes"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("testActor comments on project: %v", err)
	}
	if _, err := s.AddComment(ctx, AddCommentRequest{Ref: projectRef, Body: "reply"}, testActorAlice, testCorrelationID, "", ""); err != nil {
		t.Fatalf("alice comments on project: %v", err)
	}

	result, err := s.ListNotifications(ctx, testActor, false, 10, "")
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(result.Notifications) != 1 || result.Notifications[0].Kind != notificationKindCommented {
		t.Fatalf("testActor's notifications = %+v, want exactly one 'commented'", result.Notifications)
	}
	if result.Notifications[0].Entity != "" {
		t.Errorf("notification Entity = %q, want empty for a project-kind entity", result.Notifications[0].Entity)
	}
	if result.Notifications[0].EntityKind != domain.KindProject {
		t.Errorf("notification EntityKind = %q, want %q", result.Notifications[0].EntityKind, domain.KindProject)
	}
}
