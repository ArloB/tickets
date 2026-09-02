package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// activityEventTypes is the "selected" audit event types the activity
// feed shows (§5.10) — an explicit allowlist rather than "every event
// type audit.go emits," so a future purely-internal event type can be
// excluded by omission instead of a filter-logic change at every call
// site. Every event type this codebase currently emits is already
// user-meaningful, so this simply lists them all; it is deliberately
// not derived from audit.go's constants automatically, so adding a new
// event type there requires a matching addition here as a conscious
// choice. Also doubles as the valid-value set for the event_type list
// filter below.
var activityEventTypes = map[string]bool{
	eventProjectCreated:        true,
	eventTicketCreated:         true,
	eventTicketStatusChanged:   true,
	eventRelationshipAdded:     true,
	eventRelationshipRemoved:   true,
	eventFeatureCreated:        true,
	eventFeatureUpdated:        true,
	eventFeatureStatusChanged:  true,
	eventTicketUpdated:         true,
	eventTicketAssigned:        true,
	eventTicketMoved:           true,
	eventCommentAdded:          true,
	eventCommentEdited:         true,
	eventCommentDeleted:        true,
	eventTicketReordered:       true,
	eventFeatureReordered:      true,
	eventTicketDeleted:         true,
	eventTicketRestored:        true,
	eventFeatureDeleted:        true,
	eventFeatureRestored:       true,
	eventAssociationAdded:      true,
	eventAssociationRemoved:    true,
	eventDecisionCreated:       true,
	eventDecisionUpdated:       true,
	eventExternalLinkAdded:     true,
	eventExternalLinkRemoved:   true,
	eventContentItemCreated:    true,
	eventContentItemUpdated:    true,
	eventContentItemArchived:   true,
	eventContentItemUnarchived: true,
	eventAttachmentAdded:       true,
	eventAttachmentReplaced:    true,
	eventAttachmentRemoved:     true,
	eventProjectUpdated:        true,
	eventProjectArchived:       true,
	eventProjectUnarchived:     true,
}

// activityEventTypesList is activityEventTypes' keys, computed once —
// the always-applied event_type IN (...) filter ListActivity passes to
// store.ListActivityPage needs a slice, not a set; sorted so the
// resulting SQL is deterministic across runs (helpful for query-plan
// caching and for reading a captured query in a log).
var activityEventTypesList = func() []string {
	out := make([]string, 0, len(activityEventTypes))
	for t := range activityEventTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}()

// ActivityEvent is one row of a project's activity feed (§5.10):
// comments merged with selected audit events, newest first. EntityRef
// is "" only for a project-level event's entity (project_created/
// project_updated) — a project has no seq-numbered reference token to
// format (domain.Format rejects KindProject); the project itself is
// already identified by the ListActivity call's own projectKey.
type ActivityEvent struct {
	ID          int64
	EntityRef   string
	EntityKind  domain.EntityKind
	Actor       domain.ActorRef
	EventType   string
	CommentID   *int64
	CommentBody *string
	Changes     string
	CreatedAt   time.Time
}

// ActivityListFilters is ListActivity's optional narrowing — actor,
// entity kind, and event type, AND-composed like every other list
// filter in this codebase (docs/contracts/list-filters.md).
type ActivityListFilters struct {
	Actor      string // actor ref wire form, e.g. "human:alice"
	EntityKind string
	EventType  string
}

// ActivityListResult is ListActivity's output.
type ActivityListResult struct {
	Events     []ActivityEvent
	NextCursor string
}

// ListActivity returns a project's activity feed, newest first,
// cursor-paginated (§5.10). No MCP tool surfaces this (product spec
// §7.2 doesn't name one, and the risk table warns against growing the
// tool surface with tools agents don't need) — HTTP, CLI, and web only.
func (s *Service) ListActivity(ctx context.Context, projectKey string, filters ActivityListFilters, limit int, cursor string) (ActivityListResult, error) {
	proj, err := store.GetProjectByKey(ctx, s.store.DB(), projectKey)
	if errors.Is(err, store.ErrNotFound) {
		return ActivityListResult{}, newNotFoundError("project %q not found", projectKey)
	}
	if err != nil {
		return ActivityListResult{}, fmt.Errorf("service: look up project: %w", err)
	}

	storeFilters := store.ActivityFilters{EventTypes: activityEventTypesList}
	if filters.Actor != "" {
		id, aerr := s.resolveActorFilterID(ctx, "actor", filters.Actor)
		if aerr != nil {
			return ActivityListResult{}, aerr
		}
		storeFilters.ActorID = id
	}
	if filters.EntityKind != "" {
		kind := domain.EntityKind(filters.EntityKind)
		if !kind.Valid() {
			return ActivityListResult{}, newValidationError("entity_kind", "invalid entity kind %q", filters.EntityKind)
		}
		storeFilters.EntityKind = kind
	}
	if filters.EventType != "" {
		if !activityEventTypes[filters.EventType] {
			return ActivityListResult{}, newValidationError("event_type", "invalid event type %q", filters.EventType)
		}
		// Narrows the always-applied allowlist down to exactly the one
		// requested type, rather than an additional AND — EventTypes is an
		// IN (...) set, and the caller's single type is already a member
		// of it.
		storeFilters.EventTypes = []string{filters.EventType}
	}

	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	beforeCreatedAt, beforeID, derr := store.DecodeCreatedAtIDCursor(cursor)
	if derr != nil {
		return ActivityListResult{}, newValidationError("cursor", "invalid cursor")
	}

	page, err := store.ListActivityPage(ctx, s.store.DB(), proj.ID, storeFilters, limit, beforeCreatedAt, beforeID)
	if err != nil {
		return ActivityListResult{}, fmt.Errorf("service: list activity: %w", err)
	}

	// Small per-call caches, not a persistent cache: a page's events
	// routinely repeat the same actor (one person's editing session) and
	// the same entity (several events against one ticket), so this
	// avoids re-resolving an identical (kind, id) more than once per
	// request without the complexity of a real batched/JOIN-based
	// resolution — worth revisiting if a benchmark against the §11
	// reference dataset ever shows this page is still too slow (this
	// codebase's established policy, per docs/contracts/list-filters.md's
	// Index Coverage section: optimize once a benchmark demands it, not
	// speculatively).
	entityRefCache := make(map[int64]string, len(page.Events))
	actorCache := make(map[int64]domain.ActorRef, len(page.Events))

	out := make([]ActivityEvent, 0, len(page.Events))
	for _, row := range page.Events {
		entityRef, cached := entityRefCache[row.EntityID]
		if !cached {
			var rerr error
			entityRef, rerr = activityEntityRef(ctx, s.store.DB(), row.EntityID, row.EntityKind, projectKey)
			if rerr != nil {
				return ActivityListResult{}, fmt.Errorf("service: resolve activity entity ref: %w", rerr)
			}
			entityRefCache[row.EntityID] = entityRef
		}
		actor, cached := actorCache[row.ActorID]
		if !cached {
			var aerr error
			actor, aerr = store.GetActorRefByID(ctx, s.store.DB(), row.ActorID)
			if aerr != nil {
				return ActivityListResult{}, fmt.Errorf("service: resolve activity actor: %w", aerr)
			}
			actorCache[row.ActorID] = actor
		}
		createdAt, terr := time.Parse(store.TimeLayout, row.CreatedAt)
		if terr != nil {
			return ActivityListResult{}, fmt.Errorf("service: parse activity created_at: %w", terr)
		}
		out = append(out, ActivityEvent{
			ID: row.ID, EntityRef: entityRef, EntityKind: row.EntityKind, Actor: actor, EventType: row.EventType,
			CommentID: row.CommentID, CommentBody: row.CommentBody, Changes: row.Changes, CreatedAt: createdAt,
		})
	}
	return ActivityListResult{Events: out, NextCursor: page.NextCursor}, nil
}

// activityEntityRef resolves one activity row's entity id to its public
// reference, dispatching on the already-known entity kind (the store
// row's own join, not a second lookup like mentionTargetRef's). Uses
// the *AnyDeletion store variants — see
// store.GetTicketRefByEntityIDAnyDeletion's doc for why the feed must
// keep resolving a soft-deleted entity's events.
func activityEntityRef(ctx context.Context, q store.Querier, entityID int64, kind domain.EntityKind, projectKey string) (string, error) {
	switch kind {
	case domain.KindProject:
		return "", nil
	case domain.KindTicket:
		ref, err := store.GetTicketRefByEntityIDAnyDeletion(ctx, q, entityID)
		if err != nil {
			return "", err
		}
		return domain.Format(ref)
	case domain.KindFeature:
		ref, err := store.GetFeatureRefByEntityIDAnyDeletion(ctx, q, entityID)
		if err != nil {
			return "", err
		}
		return domain.Format(ref)
	case domain.KindDecision:
		ref, err := store.GetDecisionRefByEntityIDAnyDeletion(ctx, q, entityID)
		if err != nil {
			return "", err
		}
		return domain.Format(ref)
	case domain.KindPlan, domain.KindDocument:
		ref, err := store.GetContentItemRefByEntityIDAnyDeletion(ctx, q, entityID)
		if err != nil {
			return "", err
		}
		return domain.Format(ref)
	default:
		return "", fmt.Errorf("service: unexpected activity entity kind %q", kind)
	}
}
