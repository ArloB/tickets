package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// CreateFeatureRequest is CreateFeature's input.
type CreateFeatureRequest struct {
	ProjectKey  string
	Title       string
	Description string
	Priority    domain.Priority
}

// CreateFeature allocates a reference and creates a feature in the
// given project, attributed to actor and tagged with correlationID on
// its audit event. No Idempotency-Key handling: unlike the two Phase 0
// creates, nothing in Phase 1's plan calls for retry-safety on this
// below-the-API-line operation, and adding it speculatively would be
// untested surface no caller exercises.
func (s *Service) CreateFeature(ctx context.Context, req CreateFeatureRequest, actor domain.ActorRef, correlationID string) (domain.Feature, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Feature{}, newValidationError("title", "title is required")
	}
	priority := req.Priority
	if priority == "" {
		priority = domain.PriorityMedium
	}
	if !priority.Valid() {
		return domain.Feature{}, newValidationError("priority", "invalid priority %q", req.Priority)
	}

	var result domain.Reference
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		proj, err := store.GetProjectByKey(ctx, tx, req.ProjectKey)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("project %q not found", req.ProjectKey)
		}
		if err != nil {
			return fmt.Errorf("service: look up project: %w", err)
		}

		featureEntityID, _, err := store.InsertEntity(ctx, tx, &proj.ID, domain.KindFeature, actorID, now)
		if err != nil {
			return fmt.Errorf("service: create feature entity: %w", err)
		}
		seq, err := store.AllocateReference(ctx, tx, proj.ID, domain.KindFeature)
		if err != nil {
			return fmt.Errorf("service: allocate feature reference: %w", err)
		}
		maxPos, err := store.FeatureGroupMaxPositionByPriority(ctx, tx, proj.ID, string(priority))
		if err != nil {
			return fmt.Errorf("service: load priority group: %w", err)
		}
		if err := store.InsertFeature(ctx, tx, featureEntityID, proj.ID, seq, title, req.Description, string(priority), domain.TailPosition(maxPos)); err != nil {
			return fmt.Errorf("service: create feature: %w", err)
		}
		if err := rescanMentions(ctx, tx, featureEntityID, sourceOwnBody, req.ProjectKey, req.Description, now); err != nil {
			return err
		}

		ref := domain.Reference{ProjectKey: req.ProjectKey, Kind: domain.KindFeature, Seq: seq}
		refStr, err := domain.Format(ref)
		if err != nil {
			return fmt.Errorf("service: format created feature ref: %w", err)
		}

		changes := auditChanges(map[string]any{"ref": refStr, "title": title})
		if err := store.InsertAuditEvent(ctx, tx, featureEntityID, actorID, eventFeatureCreated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		result = ref
		return nil
	})
	if err != nil {
		return domain.Feature{}, err
	}
	return s.GetFeature(ctx, result)
}

// GetFeature looks up a feature by its parsed reference.
func (s *Service) GetFeature(ctx context.Context, ref domain.Reference) (domain.Feature, error) {
	row, err := store.GetFeatureByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Feature{}, newNotFoundError("feature not found")
	}
	if err != nil {
		return domain.Feature{}, fmt.Errorf("service: get feature: %w", err)
	}
	return row.Entity, nil
}

// ListFeatures returns every non-deleted feature in a project, ordered
// by priority then position. Unpaginated (store.ListFeaturesForProject's
// doc explains why).
func (s *Service) ListFeatures(ctx context.Context, projectKey string) ([]domain.Feature, error) {
	proj, err := store.GetProjectByKey(ctx, s.store.DB(), projectKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("project %q not found", projectKey)
	}
	if err != nil {
		return nil, fmt.Errorf("service: look up project: %w", err)
	}
	rows, err := store.ListFeaturesForProject(ctx, s.store.DB(), proj.ID)
	if err != nil {
		return nil, fmt.Errorf("service: list features: %w", err)
	}
	out := make([]domain.Feature, len(rows))
	for i, row := range rows {
		out[i] = row.Entity
	}
	return out, nil
}

// UpdateFeatureRequest is UpdateFeature's input.
type UpdateFeatureRequest struct {
	Ref             domain.Reference
	Title           string
	Description     string
	Priority        domain.Priority
	ExpectedVersion int64
}

// UpdateFeature applies a conditional field update (ADR 0008).
func (s *Service) UpdateFeature(ctx context.Context, req UpdateFeatureRequest, actor domain.ActorRef, correlationID string) (domain.Feature, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Feature{}, newValidationError("title", "title is required")
	}
	if !req.Priority.Valid() {
		return domain.Feature{}, newValidationError("priority", "invalid priority %q", req.Priority)
	}

	var result domain.Feature
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetFeatureByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("feature not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up feature: %w", err)
		}

		newPosition := row.Position
		if req.Priority != row.Entity.Priority {
			maxPos, err := store.FeatureGroupMaxPositionByPriority(ctx, tx, row.ProjectEntityID, string(req.Priority))
			if err != nil {
				return fmt.Errorf("service: load new priority group: %w", err)
			}
			newPosition = domain.TailPosition(maxPos)
		}
		if _, err := store.UpdateFeatureFields(ctx, tx, row.ID, title, req.Description, string(req.Priority), newPosition, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update feature: %w", err)
		}
		if err := rescanMentions(ctx, tx, row.ID, sourceOwnBody, row.Entity.ProjectKey, req.Description, now); err != nil {
			return err
		}

		changes := auditChanges(map[string]any{"title": title, "priority": string(req.Priority)})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventFeatureUpdated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetFeatureByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated feature: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Feature{}, err
	}
	return result, nil
}

// ReorderFeatureRequest mirrors ReorderTicketRequest for features.
type ReorderFeatureRequest struct {
	Ref             domain.Reference
	AfterRef        *domain.Reference
	ExpectedVersion int64
}

// ReorderFeature is ReorderTicket's counterpart for features — see
// its doc for the placement/renumber algorithm, which is identical
// modulo the table.
func (s *Service) ReorderFeature(ctx context.Context, req ReorderFeatureRequest, actor domain.ActorRef, correlationID string) (domain.Feature, error) {
	var result domain.Feature
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetFeatureByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("feature not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up feature: %w", err)
		}

		others, err := store.FeatureGroupOrderedExcluding(ctx, tx, row.ProjectEntityID, row.PriorityRank, row.ID)
		if err != nil {
			return fmt.Errorf("service: load priority group: %w", err)
		}

		insertIdx := 0
		if req.AfterRef != nil {
			anchor, err := store.GetFeatureByRef(ctx, tx, *req.AfterRef)
			if errors.Is(err, store.ErrNotFound) {
				return newNotFoundError("after feature not found")
			}
			if err != nil {
				return fmt.Errorf("service: look up after feature: %w", err)
			}
			if anchor.ID == row.ID {
				return newValidationError("after", "cannot reorder a feature after itself")
			}
			if anchor.ProjectEntityID != row.ProjectEntityID || anchor.PriorityRank != row.PriorityRank {
				return newValidationError("after", "after feature %s is not in the same priority group", anchor.Entity.Ref)
			}
			idx := -1
			for i, m := range others {
				if m.EntityID == anchor.ID {
					idx = i
					break
				}
			}
			if idx == -1 {
				return fmt.Errorf("service: after feature %d not found in its own priority group (data integrity)", anchor.ID)
			}
			insertIdx = idx + 1
		}

		plan := planPlacement(row.ID, others, insertIdx)
		newPos := plan.Position
		if plan.Renumber != nil {
			positions := domain.RenumberPositions(len(plan.Renumber))
			for i, id := range plan.Renumber {
				if id == row.ID {
					newPos = positions[i]
					continue
				}
				if err := store.SetFeaturePositionUnversioned(ctx, tx, id, positions[i]); err != nil {
					return fmt.Errorf("service: renumber priority group: %w", err)
				}
			}
		}

		if _, err := store.SetFeaturePositionVersioned(ctx, tx, row.ID, newPos, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: set feature position: %w", err)
		}

		reorderChanges := auditChanges(map[string]any{"position": newPos})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventFeatureReordered, corrID, nil, reorderChanges, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetFeatureByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload reordered feature: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Feature{}, err
	}
	return result, nil
}

// DeleteFeatureRequest is DeleteFeature's input. Cascade opts into
// deleting the feature's non-deleted tickets together with it, in one
// transaction — without it, a non-empty feature is blocked (ADR
// 0013).
type DeleteFeatureRequest struct {
	Ref             domain.Reference
	Cascade         bool
	ExpectedVersion int64
}

// DeleteFeature soft-deletes a feature. The General feature can never
// be deleted (ADR 0001). A feature holding non-deleted tickets is
// blocked with has_dependents naming the count unless Cascade is set,
// in which case every dependent ticket is soft-deleted in the same
// transaction, each getting its own ticket_deleted audit event in
// addition to the feature's own feature_deleted event.
//
// Returns the feature's new version — see DeleteTicket's doc for why
// (ADR 0013's restore-discoverability gap). Cascade-deleted tickets
// are deleted unconditionally (store.SoftDeleteEntityUnconditional)
// and so have no per-ticket version to hand back here; each is
// resolvable via a normal GetTicket(AnyDeletion) call instead.
func (s *Service) DeleteFeature(ctx context.Context, req DeleteFeatureRequest, actor domain.ActorRef, correlationID string) (int64, error) {
	var newVersion int64
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetFeatureByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("feature not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up feature: %w", err)
		}

		proj, err := store.GetProjectByKey(ctx, tx, row.Entity.ProjectKey)
		if err != nil {
			return fmt.Errorf("service: look up project: %w", err)
		}
		if proj.GeneralFeatureID == row.ID {
			return newValidationError("ref", "the General feature cannot be deleted")
		}

		dependents, err := store.ListTicketEntityIDsForFeature(ctx, tx, row.ID)
		if err != nil {
			return fmt.Errorf("service: list feature's tickets: %w", err)
		}
		if len(dependents) > 0 && !req.Cascade {
			return newHasDependentsError("feature %s has %d non-deleted ticket(s); retry with cascade to delete them together", row.Entity.Ref, len(dependents))
		}

		v, err := store.SoftDeleteEntity(ctx, tx, row.ID, req.ExpectedVersion, now)
		if err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: soft-delete feature: %w", err)
		}
		newVersion = v

		changes := auditChanges(map[string]any{"ref": row.Entity.Ref, "cascade": req.Cascade, "dependents": len(dependents)})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventFeatureDeleted, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		for _, ticketEntityID := range dependents {
			// Resolve the ref before deleting: GetTicketRefByEntityID
			// filters deleted_at IS NULL like every other read path, so
			// it would return not_found if asked after the delete below.
			ticketRef, err := store.GetTicketRefByEntityID(ctx, tx, ticketEntityID)
			if err != nil {
				return fmt.Errorf("service: resolve cascade-deleted ticket ref: %w", err)
			}
			ticketRefStr, err := domain.Format(ticketRef)
			if err != nil {
				return fmt.Errorf("service: format cascade-deleted ticket ref: %w", err)
			}

			if err := store.SoftDeleteEntityUnconditional(ctx, tx, ticketEntityID, now); err != nil {
				return fmt.Errorf("service: cascade-delete ticket: %w", err)
			}

			ticketChanges := auditChanges(map[string]any{"ref": ticketRefStr, "cascade_from": row.Entity.Ref})
			if err := store.InsertAuditEvent(ctx, tx, ticketEntityID, actorID, eventTicketDeleted, corrID, nil, ticketChanges, now); err != nil {
				return fmt.Errorf("service: record cascade audit event: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// RestoreFeatureRequest is RestoreFeature's input.
type RestoreFeatureRequest struct {
	Ref             domain.Reference
	ExpectedVersion int64
}

// RestoreFeature clears a feature's soft-delete. It does not restore
// any tickets a cascade delete took down with it — each is restored
// individually, which RestoreTicket now allows once its feature is
// live again.
func (s *Service) RestoreFeature(ctx context.Context, req RestoreFeatureRequest, actor domain.ActorRef, correlationID string) (domain.Feature, error) {
	var result domain.Feature
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetFeatureByRefAnyDeletion(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("feature not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up feature: %w", err)
		}
		if row.Entity.DeletedAt == nil {
			return newValidationError("ref", "feature is not deleted")
		}

		if _, err := store.RestoreEntity(ctx, tx, row.ID, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: restore feature: %w", err)
		}

		changes := auditChanges(map[string]any{"ref": row.Entity.Ref})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventFeatureRestored, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetFeatureByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload restored feature: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Feature{}, err
	}
	return result, nil
}
