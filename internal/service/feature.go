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
		if err := store.InsertFeature(ctx, tx, featureEntityID, proj.ID, seq, title, req.Description, string(priority)); err != nil {
			return fmt.Errorf("service: create feature: %w", err)
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

		if _, err := store.UpdateFeatureFields(ctx, tx, row.ID, title, req.Description, string(req.Priority), req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update feature: %w", err)
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
