package store

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func testProjectAndTicket(t *testing.T, db Querier) (projID, ticketEntityID int64) {
	t.Helper()
	ctx := context.Background()
	sysID := mustSystemActorID(t, db)
	projID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity project: %v", err)
	}
	if err := InsertProject(ctx, db, projID, "NOTE", "Notifications", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}
	featID, _, err := InsertEntity(ctx, db, &projID, domain.KindFeature, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity feature: %v", err)
	}
	if err := InsertFeature(ctx, db, featID, projID, 1, "General", "", "medium", 1000); err != nil {
		t.Fatalf("InsertFeature: %v", err)
	}
	ticketEntityID, _, err = InsertEntity(ctx, db, &projID, domain.KindTicket, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity ticket: %v", err)
	}
	if err := InsertTicket(ctx, db, ticketEntityID, projID, featID, 1, "task", "T", "", "backlog", "medium", nil, 1000); err != nil {
		t.Fatalf("InsertTicket: %v", err)
	}
	return projID, ticketEntityID
}

func TestSubscriptionsSubscribeUnsubscribeIdempotent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	_, ticketID := testProjectAndTicket(t, db)
	actorID := mustSystemActorID(t, db)

	if err := Subscribe(ctx, db, ticketID, actorID, Now()); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := Subscribe(ctx, db, ticketID, actorID, Now()); err != nil {
		t.Fatalf("Subscribe again (idempotent): %v", err)
	}
	subscribed, err := IsSubscribed(ctx, db, ticketID, actorID)
	if err != nil || !subscribed {
		t.Fatalf("IsSubscribed = %v, %v; want true", subscribed, err)
	}
	ids, err := ListSubscriberActorIDs(ctx, db, ticketID)
	if err != nil || len(ids) != 1 || ids[0] != actorID {
		t.Fatalf("ListSubscriberActorIDs = %v, %v; want [%d]", ids, err, actorID)
	}

	if err := Unsubscribe(ctx, db, ticketID, actorID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if err := Unsubscribe(ctx, db, ticketID, actorID); err != nil {
		t.Fatalf("Unsubscribe again (no-op): %v", err)
	}
	subscribed, err = IsSubscribed(ctx, db, ticketID, actorID)
	if err != nil || subscribed {
		t.Fatalf("IsSubscribed after unsubscribe = %v, %v; want false", subscribed, err)
	}
}

func TestActorMentionsDeleteAndReinsert(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	_, ticketID := testProjectAndTicket(t, db)
	actorID := mustSystemActorID(t, db)

	if err := InsertActorMention(ctx, db, ticketID, 0, actorID, Now()); err != nil {
		t.Fatalf("InsertActorMention: %v", err)
	}
	if err := InsertActorMention(ctx, db, ticketID, 0, actorID, Now()); err != nil {
		t.Fatalf("InsertActorMention duplicate: %v", err)
	}
	if err := DeleteActorMentionsFromSource(ctx, db, ticketID, 0); err != nil {
		t.Fatalf("DeleteActorMentionsFromSource: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM actor_mentions WHERE source_entity_id = ?`, ticketID).Scan(&count); err != nil {
		t.Fatalf("count actor_mentions: %v", err)
	}
	if count != 0 {
		t.Fatalf("actor_mentions count after delete = %d, want 0", count)
	}
}

func TestNotificationsListAndMarkRead(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	_, ticketID := testProjectAndTicket(t, db)
	actorID := mustSystemActorID(t, db)

	for i := 0; i < 3; i++ {
		if err := InsertNotification(ctx, db, actorID, "commented", ticketID, nil, &actorID, Now()); err != nil {
			t.Fatalf("InsertNotification %d: %v", i, err)
		}
	}

	page, err := ListNotificationsForActor(ctx, db, actorID, false, 10, "", 0)
	if err != nil {
		t.Fatalf("ListNotificationsForActor: %v", err)
	}
	if len(page.Notifications) != 3 {
		t.Fatalf("notifications = %d, want 3", len(page.Notifications))
	}
	for _, n := range page.Notifications {
		if n.EntityKind != "ticket" {
			t.Errorf("notification EntityKind = %q, want ticket", n.EntityKind)
		}
		if n.ReadAt != nil {
			t.Errorf("notification ReadAt = %v, want nil before marking read", n.ReadAt)
		}
	}

	unread, err := ListNotificationsForActor(ctx, db, actorID, true, 10, "", 0)
	if err != nil || len(unread.Notifications) != 3 {
		t.Fatalf("unread-only listing = %+v, %v; want 3", unread, err)
	}

	oneID := page.Notifications[0].ID
	n, err := MarkNotificationsRead(ctx, db, actorID, []int64{oneID}, false, Now())
	if err != nil || n != 1 {
		t.Fatalf("MarkNotificationsRead one = %d, %v; want 1", n, err)
	}
	unread, err = ListNotificationsForActor(ctx, db, actorID, true, 10, "", 0)
	if err != nil || len(unread.Notifications) != 2 {
		t.Fatalf("unread after marking one read = %+v, %v; want 2", unread, err)
	}

	n, err = MarkNotificationsRead(ctx, db, actorID, nil, true, Now())
	if err != nil || n != 2 {
		t.Fatalf("MarkNotificationsRead all = %d, %v; want 2", n, err)
	}
	unread, err = ListNotificationsForActor(ctx, db, actorID, true, 10, "", 0)
	if err != nil || len(unread.Notifications) != 0 {
		t.Fatalf("unread after marking all read = %+v, %v; want 0", unread, err)
	}
}

func TestMarkNotificationsReadOnlyAffectsOwnActor(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	db := s.DB()

	_, ticketID := testProjectAndTicket(t, db)
	actorID := mustSystemActorID(t, db)
	otherID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, actorID, Now())
	if err != nil {
		t.Fatalf("setup other id: %v", err)
	}
	// otherID here is just a distinct integer id unlikely to collide
	// with a real actor id; MarkNotificationsRead's WHERE actor_id = ?
	// is what this test actually exercises — using an id that isn't a
	// real actor at all makes the "no rows match" case unambiguous.

	if err := InsertNotification(ctx, db, actorID, "commented", ticketID, nil, &actorID, Now()); err != nil {
		t.Fatalf("InsertNotification: %v", err)
	}
	page, err := ListNotificationsForActor(ctx, db, actorID, false, 10, "", 0)
	if err != nil || len(page.Notifications) != 1 {
		t.Fatalf("setup listing: %+v, %v", page, err)
	}

	n, err := MarkNotificationsRead(ctx, db, otherID, []int64{page.Notifications[0].ID}, false, Now())
	if err != nil {
		t.Fatalf("MarkNotificationsRead as other actor: %v", err)
	}
	if n != 0 {
		t.Fatalf("MarkNotificationsRead as other actor affected %d rows, want 0", n)
	}
}
