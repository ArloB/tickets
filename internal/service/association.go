package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// associationEndpoint is a resolved association participant — the
// pieces AddAssociation/RemoveAssociation need regardless of whether
// the ref turned out to be a ticket or a feature.
type associationEndpoint struct {
	EntityID int64
	UUID     string
	Ref      string
}

// resolveAssociationEndpoint resolves a ticket, feature, or decision
// reference for an association edge. domain.ValidAssociationKind also
// allows plan/document, but those still have no tables (Phase 5 work)
// — a syntactically valid reference to one is a validation error here,
// not a 500, since the caller supplied well-formed input the server
// just can't act on yet.
func resolveAssociationEndpoint(ctx context.Context, q store.Querier, field string, ref domain.Reference) (associationEndpoint, error) {
	switch ref.Kind {
	case domain.KindTicket:
		row, err := store.GetTicketByRef(ctx, q, ref)
		if errors.Is(err, store.ErrNotFound) {
			return associationEndpoint{}, newNotFoundError("%s ticket not found", field)
		}
		if err != nil {
			return associationEndpoint{}, fmt.Errorf("service: look up %s ticket: %w", field, err)
		}
		return associationEndpoint{EntityID: row.ID, UUID: row.Entity.UUID, Ref: row.Entity.Ref}, nil
	case domain.KindFeature:
		row, err := store.GetFeatureByRef(ctx, q, ref)
		if errors.Is(err, store.ErrNotFound) {
			return associationEndpoint{}, newNotFoundError("%s feature not found", field)
		}
		if err != nil {
			return associationEndpoint{}, fmt.Errorf("service: look up %s feature: %w", field, err)
		}
		return associationEndpoint{EntityID: row.ID, UUID: row.Entity.UUID, Ref: row.Entity.Ref}, nil
	case domain.KindDecision:
		row, err := store.GetDecisionByRef(ctx, q, ref)
		if errors.Is(err, store.ErrNotFound) {
			return associationEndpoint{}, newNotFoundError("%s decision not found", field)
		}
		if err != nil {
			return associationEndpoint{}, fmt.Errorf("service: look up %s decision: %w", field, err)
		}
		return associationEndpoint{EntityID: row.ID, UUID: row.Entity.UUID, Ref: row.Entity.Ref}, nil
	default:
		return associationEndpoint{}, newValidationError(field, "associations with entity kind %q are not supported yet", ref.Kind)
	}
}

// AddAssociationRequest is AddAssociation's input. SourceRef/TargetRef
// may each independently be a ticket or feature reference — the
// looser associated_with link has no notion of direction or type
// beyond that (§5.7).
type AddAssociationRequest struct {
	SourceRef domain.Reference
	TargetRef domain.Reference
}

// AddAssociation validates, canonicalizes (UUID order,
// domain.CanonicalAssociation), and stores one entity_associations
// edge, attributing the audit event to the caller-supplied source —
// the entity the actor took the action on, regardless of which side
// canonicalization stores it on (mirroring AddRelationship).
func (s *Service) AddAssociation(ctx context.Context, req AddAssociationRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		src, err := resolveAssociationEndpoint(ctx, tx, "source", req.SourceRef)
		if err != nil {
			return err
		}
		tgt, err := resolveAssociationEndpoint(ctx, tx, "target", req.TargetRef)
		if err != nil {
			return err
		}

		if err := domain.ValidateAssociation(req.SourceRef.Kind, req.TargetRef.Kind, src.UUID, tgt.UUID); err != nil {
			return newValidationError("target", "%s", err)
		}

		canonSourceID, canonTargetID := canonicalAssociationIDs(src, tgt)

		exists, err := store.AssociationExists(ctx, tx, canonSourceID, canonTargetID)
		if err != nil {
			return fmt.Errorf("service: check existing association: %w", err)
		}
		if exists {
			return newAlreadyExistsError("", "this association already exists")
		}

		if err := store.InsertAssociation(ctx, tx, canonSourceID, canonTargetID, actorID, now); err != nil {
			return fmt.Errorf("service: insert association: %w", err)
		}

		changes := auditChanges(map[string]any{"target": tgt.Ref})
		if err := store.InsertAuditEvent(ctx, tx, src.EntityID, actorID, eventAssociationAdded, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
}

// canonicalAssociationIDs maps two resolved endpoints to the internal
// ids in domain.CanonicalAssociation's UUID order.
func canonicalAssociationIDs(src, tgt associationEndpoint) (sourceID, targetID int64) {
	canonSourceUUID, _ := domain.CanonicalAssociation(src.UUID, tgt.UUID)
	if canonSourceUUID == src.UUID {
		return src.EntityID, tgt.EntityID
	}
	return tgt.EntityID, src.EntityID
}

// RemoveAssociationRequest is RemoveAssociation's input.
type RemoveAssociationRequest struct {
	SourceRef domain.Reference
	TargetRef domain.Reference
}

// RemoveAssociation deletes the canonical row for the given pair, as
// supplied from either end.
func (s *Service) RemoveAssociation(ctx context.Context, req RemoveAssociationRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		src, err := resolveAssociationEndpoint(ctx, tx, "source", req.SourceRef)
		if err != nil {
			return err
		}
		tgt, err := resolveAssociationEndpoint(ctx, tx, "target", req.TargetRef)
		if err != nil {
			return err
		}

		canonSourceID, canonTargetID := canonicalAssociationIDs(src, tgt)

		deleted, err := store.DeleteAssociation(ctx, tx, canonSourceID, canonTargetID)
		if err != nil {
			return fmt.Errorf("service: delete association: %w", err)
		}
		if !deleted {
			return newNotFoundError("association not found")
		}

		changes := auditChanges(map[string]any{"target": tgt.Ref})
		if err := store.InsertAuditEvent(ctx, tx, src.EntityID, actorID, eventAssociationRemoved, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
}

// GetAssociations returns every entity associated with ref, as public
// references — ticket and feature refs only, the only two kinds
// resolveAssociationEndpoint can ever have stored.
func (s *Service) GetAssociations(ctx context.Context, ref domain.Reference) ([]domain.Reference, error) {
	endpoint, err := resolveAssociationEndpoint(ctx, s.store.DB(), "ref", ref)
	if err != nil {
		return nil, err
	}
	ids, err := store.ListAssociatedEntityIDs(ctx, s.store.DB(), endpoint.EntityID)
	if err != nil {
		return nil, fmt.Errorf("service: list associations: %w", err)
	}
	out := make([]domain.Reference, 0, len(ids))
	for _, id := range ids {
		r, err := mentionTargetRef(ctx, s.store.DB(), id)
		if err != nil {
			return nil, fmt.Errorf("service: resolve associated entity: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}
