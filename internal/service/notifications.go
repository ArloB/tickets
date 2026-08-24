package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// Notification kinds (product spec §6.4's four categories, ADR 0019).
const (
	notificationKindAssigned  = "assigned"
	notificationKindMentioned = "mentioned"
	notificationKindCommented = "commented"
	notificationKindChanged   = "changed"
)

// notify inserts one notification for recipientActorID, unless
// recipientActorID == triggeredBy — an actor never notifies
// themselves for their own action (assigning a ticket to themselves,
// commenting on their own subscribed ticket, editing their own
// subscribed decision). sourceCommentID follows rescanMentions'
// sourceOwnBody convention: 0 means "no comment," stored as SQL NULL.
func notify(ctx context.Context, tx *sql.Tx, recipientActorID int64, kind string, entityID, sourceCommentID, triggeredBy int64, now string) error {
	if recipientActorID == triggeredBy {
		return nil
	}
	var commentID *int64
	if sourceCommentID != sourceOwnBody {
		c := sourceCommentID
		commentID = &c
	}
	tb := triggeredBy
	if err := store.InsertNotification(ctx, tx, recipientActorID, kind, entityID, commentID, &tb, now); err != nil {
		return fmt.Errorf("service: insert notification: %w", err)
	}
	return nil
}

// notifySubscribers fans a notification out to every current
// subscriber of entityID except triggeredBy — the "commented"/
// "changed" categories, which are subscriber-driven rather than
// aimed at one named recipient the way "assigned"/"mentioned" are.
func notifySubscribers(ctx context.Context, tx *sql.Tx, entityID int64, kind string, sourceCommentID, triggeredBy int64, now string) error {
	subscriberIDs, err := store.ListSubscriberActorIDs(ctx, tx, entityID)
	if err != nil {
		return fmt.Errorf("service: list subscribers: %w", err)
	}
	for _, recipientID := range subscriberIDs {
		if err := notify(ctx, tx, recipientID, kind, entityID, sourceCommentID, triggeredBy, now); err != nil {
			return err
		}
	}
	return nil
}

// subscribe records actorID as a subscriber of entityID — the
// create-or-comment auto-subscribe product spec §6.4 calls for.
// Idempotent at the store layer (INSERT OR IGNORE), so calling it on
// every create/comment needs no "already subscribed" check here.
func subscribe(ctx context.Context, tx *sql.Tx, entityID, actorID int64, now string) error {
	if err := store.Subscribe(ctx, tx, entityID, actorID, now); err != nil {
		return fmt.Errorf("service: subscribe: %w", err)
	}
	return nil
}

// SubscribeRequest/UnsubscribeRequest name the entity by its public
// reference — every principal kind is subscribable (§6.4 names
// tickets/features/decisions explicitly as notifying on change;
// subscribing to a plan/document or manually re-subscribing to a
// ticket/feature/decision is allowed on the same terms, since nothing
// about the mechanism is ticket-specific).
type SubscribeRequest struct {
	Ref domain.Reference
}

// Subscribe subscribes the calling actor to ref. Called directly by
// the HTTP "subscribe" route/CLI/MCP tool — distinct from the
// package-private subscribe helper above, which mutating create/
// comment paths call inside their own transaction.
func (s *Service) Subscribe(ctx context.Context, req SubscribeRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, _, now string) error {
		entityID, err := resolveSubscribableEntity(ctx, tx, req.Ref)
		if err != nil {
			return err
		}
		return subscribe(ctx, tx, entityID, actorID, now)
	})
}

// Unsubscribe removes the calling actor's subscription to ref, if
// any.
func (s *Service) Unsubscribe(ctx context.Context, req SubscribeRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, _, now string) error {
		entityID, err := resolveSubscribableEntity(ctx, tx, req.Ref)
		if err != nil {
			return err
		}
		if err := store.Unsubscribe(ctx, tx, entityID, actorID); err != nil {
			return fmt.Errorf("service: unsubscribe: %w", err)
		}
		return nil
	})
}

// IsSubscribed reports whether the calling actor is currently
// subscribed to ref.
func (s *Service) IsSubscribed(ctx context.Context, ref domain.Reference, actor domain.ActorRef) (bool, error) {
	entityID, err := resolveSubscribableEntity(ctx, s.store.DB(), ref)
	if err != nil {
		return false, err
	}
	actorID, err := store.GetActorIDByRef(ctx, s.store.DB(), actor.Kind, actor.Name)
	if err != nil {
		return false, fmt.Errorf("service: resolve actor: %w", err)
	}
	subscribed, err := store.IsSubscribed(ctx, s.store.DB(), entityID, actorID)
	if err != nil {
		return false, fmt.Errorf("service: check subscription: %w", err)
	}
	return subscribed, nil
}

// resolveSubscribableEntity resolves ref to its internal entity id
// across every principal kind — mirrors resolveMentionTarget's
// dispatch, minus the "unresolvable kind" case: a caller here already
// supplied a well-formed Reference (parsed at the HTTP/CLI/MCP
// boundary), not a scanned free-text candidate, so an unresolvable
// kind is a validation error, not a silent skip.
func resolveSubscribableEntity(ctx context.Context, q store.Querier, ref domain.Reference) (int64, error) {
	id, err := resolveMentionTarget(ctx, q, ref)
	if errors.Is(err, store.ErrNotFound) {
		return 0, newNotFoundError("record not found")
	}
	if err != nil {
		return 0, fmt.Errorf("service: resolve subscription target: %w", err)
	}
	return id, nil
}

// Notification is one delivered notification, resolved to public,
// wire-safe fields — mirrors ActivityEvent's shape/reasoning
// (internal/service/activity.go).
type Notification struct {
	ID          int64
	Kind        string
	Entity      string
	EntityKind  domain.EntityKind
	CommentID   *int64
	TriggeredBy *domain.ActorRef
	CreatedAt   time.Time
	ReadAt      *time.Time
}

// NotificationsListResult is ListNotifications' output.
type NotificationsListResult struct {
	Notifications []Notification
	NextCursor    string
}

// ListNotifications returns the calling actor's own notifications,
// newest first, optionally narrowed to unread only.
func (s *Service) ListNotifications(ctx context.Context, actor domain.ActorRef, unreadOnly bool, limit int, cursor string) (NotificationsListResult, error) {
	actorID, err := store.GetActorIDByRef(ctx, s.store.DB(), actor.Kind, actor.Name)
	if err != nil {
		return NotificationsListResult{}, fmt.Errorf("service: resolve actor: %w", err)
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	beforeCreatedAt, beforeID, derr := store.DecodeCreatedAtIDCursor(cursor)
	if derr != nil {
		return NotificationsListResult{}, newValidationError("cursor", "invalid cursor")
	}

	page, err := store.ListNotificationsForActor(ctx, s.store.DB(), actorID, unreadOnly, limit, beforeCreatedAt, beforeID)
	if err != nil {
		return NotificationsListResult{}, fmt.Errorf("service: list notifications: %w", err)
	}

	out := make([]Notification, 0, len(page.Notifications))
	for _, row := range page.Notifications {
		n, err := toNotification(ctx, s.store.DB(), row)
		if err != nil {
			return NotificationsListResult{}, err
		}
		out = append(out, n)
	}
	return NotificationsListResult{Notifications: out, NextCursor: page.NextCursor}, nil
}

// toNotification resolves one store.NotificationRow's entity_id/kind
// to a public reference and its actor ids to ActorRefs, reusing
// activityEntityRef's *AnyDeletion dispatch (internal/service/activity.go):
// a stale entity (soft-deleted since the notification fired) still
// resolves — a notification is a delivered historical record, not a
// live index, so it must not break just because its subject was later
// archived.
func toNotification(ctx context.Context, q store.Querier, row store.NotificationRow) (Notification, error) {
	ref, err := activityEntityRef(ctx, q, row.EntityID, domain.EntityKind(row.EntityKind), "")
	if err != nil {
		return Notification{}, fmt.Errorf("service: resolve notification entity: %w", err)
	}
	createdAt, err := time.Parse(store.TimeLayout, row.CreatedAt)
	if err != nil {
		return Notification{}, fmt.Errorf("service: parse notification created_at: %w", err)
	}
	n := Notification{
		ID: row.ID, Kind: row.Kind, Entity: ref, EntityKind: domain.EntityKind(row.EntityKind),
		CommentID: row.CommentID, CreatedAt: createdAt,
	}
	if row.TriggeredBy != nil {
		actorRef, err := store.GetActorRefByID(ctx, q, *row.TriggeredBy)
		if err != nil {
			return Notification{}, fmt.Errorf("service: resolve notification actor: %w", err)
		}
		n.TriggeredBy = &actorRef
	}
	if row.ReadAt != nil {
		readAt, err := time.Parse(store.TimeLayout, *row.ReadAt)
		if err != nil {
			return Notification{}, fmt.Errorf("service: parse notification read_at: %w", err)
		}
		n.ReadAt = &readAt
	}
	return n, nil
}

// MarkNotificationsReadRequest is MarkNotificationsRead's input. All
// marks every currently-unread notification read regardless of IDs;
// IDs marks exactly those (already-read ones are left alone, not an
// error).
type MarkNotificationsReadRequest struct {
	IDs []int64
	All bool
}

// MarkNotificationsRead marks the calling actor's own notifications
// read, returning how many rows changed.
func (s *Service) MarkNotificationsRead(ctx context.Context, req MarkNotificationsReadRequest, actor domain.ActorRef, correlationID string) (int64, error) {
	var count int64
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, _, now string) error {
		n, err := store.MarkNotificationsRead(ctx, tx, actorID, req.IDs, req.All, now)
		if err != nil {
			return fmt.Errorf("service: mark notifications read: %w", err)
		}
		count = n
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
