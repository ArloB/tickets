package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// canonicalIDs resolves domain.CanonicalRelationship's UUID output
// back to the internal entity ids the two already-looked-up ticket
// rows carry. src/tgt are the caller-supplied (pre-canonicalization)
// endpoints; CanonicalRelationship may return either UUID in either
// position (e.g. it swaps them for child_of), so the mapping is keyed
// by UUID rather than assumed to preserve src/tgt order.
func canonicalIDs(src, tgt store.TicketRow, canonSourceUUID, canonTargetUUID string) (sourceID, targetID int64) {
	byUUID := map[string]int64{src.Entity.UUID: src.ID, tgt.Entity.UUID: tgt.ID}
	return byUUID[canonSourceUUID], byUUID[canonTargetUUID]
}

// needsCycleCheck reports whether relType's graph is one ADR 0014
// scopes cycle detection to (product spec §5.7): blocks and
// parent_of. related_to, duplicate_of, and supersedes have no cycle
// concept — supersedes in particular is a version-history pointer,
// not a dependency graph.
func needsCycleCheck(relType domain.RelationshipType) bool {
	return relType == domain.RelationshipBlocks || relType == domain.RelationshipParentOf
}

// AddRelationshipRequest is AddRelationship's input. Type may be any
// of the eight wire values, including the child_of/blocked_by/
// superseded_by "view" types — CanonicalRelationship maps them to
// what's actually stored.
type AddRelationshipRequest struct {
	SourceRef domain.Reference
	TargetRef domain.Reference
	Type      domain.RelationshipType
}

// AddRelationship validates, canonicalizes, cycle-checks (for blocks/
// parent_of), and stores one ticket_relationships edge, attributing
// the audit event to the caller-supplied source ticket regardless of
// which side canonicalization stores it on — that's the ticket the
// actor took an action on.
func (s *Service) AddRelationship(ctx context.Context, req AddRelationshipRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		src, err := store.GetTicketByRef(ctx, tx, req.SourceRef)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("source ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up source ticket: %w", err)
		}
		tgt, err := store.GetTicketByRef(ctx, tx, req.TargetRef)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("target ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up target ticket: %w", err)
		}

		if err := domain.ValidateRelationship(req.Type, domain.KindTicket, domain.KindTicket, src.Entity.UUID, tgt.Entity.UUID); err != nil {
			return newValidationError("type", "%s", err)
		}

		canonType, canonSourceUUID, canonTargetUUID := domain.CanonicalRelationship(req.Type, src.Entity.UUID, tgt.Entity.UUID)
		canonSourceID, canonTargetID := canonicalIDs(src, tgt, canonSourceUUID, canonTargetUUID)

		exists, err := store.RelationshipExists(ctx, tx, canonSourceID, canonTargetID, canonType)
		if err != nil {
			return fmt.Errorf("service: check existing relationship: %w", err)
		}
		if exists {
			return newAlreadyExistsError("", "this relationship already exists")
		}

		if needsCycleCheck(canonType) {
			cyc, err := store.RelationshipWouldCycle(ctx, tx, canonType, canonSourceID, canonTargetID)
			if err != nil {
				return fmt.Errorf("service: check relationship cycle: %w", err)
			}
			if cyc {
				return newRelationshipCycleError(req.Type)
			}
		}

		if err := store.InsertRelationship(ctx, tx, canonSourceID, canonTargetID, canonType, actorID, now); err != nil {
			return fmt.Errorf("service: insert relationship: %w", err)
		}

		changes := auditChanges(map[string]any{"type": string(req.Type), "target": tgt.Entity.Ref})
		if err := store.InsertAuditEvent(ctx, tx, src.ID, actorID, eventRelationshipAdded, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
}

// RemoveRelationshipRequest is RemoveRelationship's input.
type RemoveRelationshipRequest struct {
	SourceRef domain.Reference
	TargetRef domain.Reference
	Type      domain.RelationshipType
}

// RemoveRelationship deletes the canonical row for the given (source,
// target, type) as supplied from either end — the caller doesn't need
// to know which side it's actually stored on.
func (s *Service) RemoveRelationship(ctx context.Context, req RemoveRelationshipRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		src, err := store.GetTicketByRef(ctx, tx, req.SourceRef)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("source ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up source ticket: %w", err)
		}
		tgt, err := store.GetTicketByRef(ctx, tx, req.TargetRef)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("target ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up target ticket: %w", err)
		}
		if !req.Type.Valid() {
			return newValidationError("type", "invalid relationship type %q", req.Type)
		}

		canonType, canonSourceUUID, canonTargetUUID := domain.CanonicalRelationship(req.Type, src.Entity.UUID, tgt.Entity.UUID)
		canonSourceID, canonTargetID := canonicalIDs(src, tgt, canonSourceUUID, canonTargetUUID)

		deleted, err := store.DeleteRelationship(ctx, tx, canonSourceID, canonTargetID, canonType)
		if err != nil {
			return fmt.Errorf("service: delete relationship: %w", err)
		}
		if !deleted {
			return newNotFoundError("relationship not found")
		}

		changes := auditChanges(map[string]any{"type": string(req.Type), "target": tgt.Entity.Ref})
		if err := store.InsertAuditEvent(ctx, tx, src.ID, actorID, eventRelationshipRemoved, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
}

// RelationshipView is one relationship edge as seen from the ticket
// GetTicketRelationships was asked about — Type is already resolved
// to that ticket's perspective (see store.ListRelationshipsForEntity).
type RelationshipView struct {
	Type  domain.RelationshipType
	Other domain.Reference
}

// GetTicketRelationships returns every relationship touching ref's
// ticket, from that ticket's perspective, both ends included.
func (s *Service) GetTicketRelationships(ctx context.Context, ref domain.Reference) ([]RelationshipView, error) {
	row, err := store.GetTicketByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("ticket not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: get ticket: %w", err)
	}

	edges, err := store.ListRelationshipsForEntity(ctx, s.store.DB(), row.ID)
	if err != nil {
		return nil, fmt.Errorf("service: list relationships: %w", err)
	}

	views := make([]RelationshipView, 0, len(edges))
	for _, e := range edges {
		otherRef, err := store.GetTicketRefByEntityID(ctx, s.store.DB(), e.OtherID)
		if err != nil {
			return nil, fmt.Errorf("service: resolve relationship endpoint: %w", err)
		}
		views = append(views, RelationshipView{Type: e.Type, Other: otherRef})
	}
	return views, nil
}
