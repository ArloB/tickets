package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// sourceOwnBody is derived_mentions.source_comment_id's sentinel for
// "the source entity's own Markdown body" (migration
// 0002_core_domain.sql: NOT NULL DEFAULT 0, never a real comments.id
// since that column AUTOINCREMENTs from 1).
const sourceOwnBody = 0

// rescanMentions deletes and reinserts derived_mentions rows for one
// (sourceEntityID, sourceCommentID) body from the Markdown references
// domain.ScanReferences finds in body (ADR 0015, product spec §5.2).
// scopeProjectKey resolves the project-scoped #123 short form — pass
// the project the body's owning entity belongs to. Must run inside
// the same transaction as the body write it describes: the delete-
// and-reinsert has to be atomic with the edit, or a reader could
// briefly observe stale or missing edges.
//
// A well-formed reference to a kind Phase 1 can't resolve yet
// (decision/plan/document have no tables until Phase 5) or to a
// specific ticket/feature that doesn't exist is silently skipped, not
// an error — "well-formed but unresolvable references are simply not
// stored as edges" (the Phase 1 plan's Step 4). A self-mention is
// also skipped; the primary key doesn't reject target_entity_id ==
// source_entity_id on its own, so this is a real guard.
func rescanMentions(ctx context.Context, tx *sql.Tx, sourceEntityID, sourceCommentID int64, scopeProjectKey, body, now string) error {
	if err := store.DeleteMentionsFromSource(ctx, tx, sourceEntityID, sourceCommentID); err != nil {
		return fmt.Errorf("service: clear mentions: %w", err)
	}
	for _, ref := range domain.ScanReferences(body, scopeProjectKey) {
		targetID, err := resolveMentionTarget(ctx, tx, ref)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("service: resolve mention: %w", err)
		}
		if targetID == sourceEntityID {
			continue
		}
		if err := store.InsertDerivedMention(ctx, tx, sourceEntityID, sourceCommentID, targetID, now); err != nil {
			return fmt.Errorf("service: insert mention: %w", err)
		}
	}
	return nil
}

// resolveMentionTarget resolves a scanned reference to its internal
// entity id, or store.ErrNotFound if it's a kind Phase 1 has no table
// for, or a specific record that doesn't exist.
func resolveMentionTarget(ctx context.Context, tx *sql.Tx, ref domain.Reference) (int64, error) {
	switch ref.Kind {
	case domain.KindTicket:
		row, err := store.GetTicketByRef(ctx, tx, ref)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	case domain.KindFeature:
		row, err := store.GetFeatureByRef(ctx, tx, ref)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	case domain.KindDecision:
		row, err := store.GetDecisionByRef(ctx, tx, ref)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	default:
		return 0, store.ErrNotFound
	}
}

// mentionTargetRef resolves a mention edge's bare target entity id
// back to a public reference, dispatching on the target's kind — a
// mention target is a ticket, feature, or (Phase 3) decision, the only
// kinds resolveMentionTarget can ever have stored.
func mentionTargetRef(ctx context.Context, q store.Querier, entityID int64) (domain.Reference, error) {
	kind, err := store.GetEntityKindByID(ctx, q, entityID)
	if err != nil {
		return domain.Reference{}, err
	}
	switch kind {
	case domain.KindTicket:
		return store.GetTicketRefByEntityID(ctx, q, entityID)
	case domain.KindFeature:
		return store.GetFeatureRefByEntityID(ctx, q, entityID)
	case domain.KindDecision:
		return store.GetDecisionRefByEntityID(ctx, q, entityID)
	default:
		return domain.Reference{}, fmt.Errorf("service: unexpected mention target kind %q", kind)
	}
}

// GetTicketMentions returns the current outgoing mention targets of a
// ticket's description, as public references — for tests (gate 7) and
// any future caller that wants to show a ticket's mentions.
func (s *Service) GetTicketMentions(ctx context.Context, ref domain.Reference) ([]domain.Reference, error) {
	row, err := store.GetTicketByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("ticket not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: get ticket: %w", err)
	}
	return s.resolveMentionRefs(ctx, row.ID, sourceOwnBody)
}

// GetCommentMentions is GetTicketMentions' counterpart for a
// comment's body.
func (s *Service) GetCommentMentions(ctx context.Context, commentID int64) ([]domain.Reference, error) {
	row, err := store.GetComment(ctx, s.store.DB(), commentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("comment not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: get comment: %w", err)
	}
	return s.resolveMentionRefs(ctx, row.EntityID, commentID)
}

func (s *Service) resolveMentionRefs(ctx context.Context, sourceEntityID, sourceCommentID int64) ([]domain.Reference, error) {
	targetIDs, err := store.ListMentionTargetsFromSource(ctx, s.store.DB(), sourceEntityID, sourceCommentID)
	if err != nil {
		return nil, fmt.Errorf("service: list mentions: %w", err)
	}
	out := make([]domain.Reference, 0, len(targetIDs))
	for _, id := range targetIDs {
		ref, err := mentionTargetRef(ctx, s.store.DB(), id)
		if err != nil {
			return nil, fmt.Errorf("service: resolve mention target: %w", err)
		}
		out = append(out, ref)
	}
	return out, nil
}
