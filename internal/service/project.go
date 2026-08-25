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

const generalFeatureTitle = "General"

// CreateProjectRequest is CreateProject's input.
type CreateProjectRequest struct {
	Key         string
	Title       string
	Description string
}

// CreateProject creates a project and its mandatory General feature
// (ADR 0001) in one transaction, attributed to actor (ADR 0012) and
// tagged with correlationID on its audit event. idemKey/fingerprint
// may be empty, meaning the caller supplied no Idempotency-Key.
//
// The idempotency cache stores only the project's key, never a
// snapshot of the response (see idempotency.go and the migration's
// comment on idempotency_keys.ref_key) — so both a fresh create and a
// replayed one finish by re-fetching the live project via GetProject,
// and both return identically shaped, fully current data.
func (s *Service) CreateProject(ctx context.Context, req CreateProjectRequest, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (domain.Project, error) {
	if !domain.ValidProjectKey(req.Key) {
		return domain.Project{}, newValidationError("key", "project key must be 2-10 uppercase letters/digits starting with a letter")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Project{}, newValidationError("title", "title is required")
	}

	key, notifiedIDs, err := s.createProjectTx(ctx, req, title, actor, correlationID, idemKey, fingerprint)
	if err != nil {
		return domain.Project{}, err
	}
	proj, err := s.GetProject(ctx, key)
	if err != nil {
		return domain.Project{}, err
	}
	s.broadcast(ChangeHint{Kind: HintEntityChanged, Ref: proj.Key, Project: proj.Key})
	s.publishNotified(ctx, notifiedIDs)
	return proj, nil
}

func (s *Service) createProjectTx(ctx context.Context, req CreateProjectRequest, title string, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (string, []int64, error) {
	var result string
	var notifiedIDs []int64
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if cached, found, err := checkIdempotency(ctx, tx, idemKey, actorID, fingerprint); err != nil {
			return err
		} else if found {
			result = cached
			return nil // no writes happened on this path; committing is a no-op
		}

		if _, err := store.GetProjectByKey(ctx, tx, req.Key); err == nil {
			return newAlreadyExistsError("key", "a project with key %q already exists", req.Key)
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("service: check existing project: %w", err)
		}

		projectEntityID, _, err := store.InsertEntity(ctx, tx, nil, domain.KindProject, actorID, now)
		if err != nil {
			return fmt.Errorf("service: create project entity: %w", err)
		}
		if err := store.InsertProject(ctx, tx, projectEntityID, req.Key, title, req.Description); err != nil {
			return fmt.Errorf("service: create project: %w", err)
		}
		ids, err := rescanMentions(ctx, tx, projectEntityID, sourceOwnBody, req.Key, req.Description, now, actorID)
		if err != nil {
			return err
		}
		notifiedIDs = ids
		if err := indexProjectSearchDoc(ctx, tx, projectEntityID, domain.Project{
			Key: req.Key, Title: title, Description: req.Description, Status: domain.ProjectStatusActive,
		}); err != nil {
			return err
		}

		// Mandatory General feature (ADR 0001), created in the same
		// transaction so a project never briefly exists without one.
		featureEntityID, _, err := store.InsertEntity(ctx, tx, &projectEntityID, domain.KindFeature, actorID, now)
		if err != nil {
			return fmt.Errorf("service: create general feature entity: %w", err)
		}
		featureSeq, err := store.AllocateReference(ctx, tx, projectEntityID, domain.KindFeature)
		if err != nil {
			return fmt.Errorf("service: allocate general feature reference: %w", err)
		}
		// The General feature is the project's first, so its (project,
		// priority) group is empty — domain.TailPosition(0) is its tail
		// position without a query to confirm that.
		if err := store.InsertFeature(ctx, tx, featureEntityID, projectEntityID, featureSeq, generalFeatureTitle, "", string(domain.PriorityMedium), domain.TailPosition(0)); err != nil {
			return fmt.Errorf("service: create general feature: %w", err)
		}
		if err := store.SetProjectGeneralFeature(ctx, tx, projectEntityID, featureEntityID); err != nil {
			return fmt.Errorf("service: link general feature: %w", err)
		}
		generalFeatureRef, err := domain.Format(domain.Reference{ProjectKey: req.Key, Kind: domain.KindFeature, Seq: featureSeq})
		if err != nil {
			return fmt.Errorf("service: format general feature ref: %w", err)
		}
		if err := indexFeatureSearchDoc(ctx, tx, featureEntityID, projectEntityID, domain.Feature{
			Ref: generalFeatureRef, Status: domain.WorkflowStatusBacklog, Title: generalFeatureTitle,
		}); err != nil {
			return err
		}

		changes := auditChanges(map[string]any{"key": req.Key, "title": title})
		if err := store.InsertAuditEvent(ctx, tx, projectEntityID, actorID, eventProjectCreated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		if err := recordIdempotency(ctx, tx, idemKey, actorID, fingerprint, req.Key, now); err != nil {
			return err
		}
		result = req.Key
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return result, notifiedIDs, nil
}

// GetProject looks up a project by key.
func (s *Service) GetProject(ctx context.Context, key string) (domain.Project, error) {
	row, err := store.GetProjectByKey(ctx, s.store.DB(), key)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Project{}, newNotFoundError("project %q not found", key)
	}
	if err != nil {
		return domain.Project{}, fmt.Errorf("service: get project: %w", err)
	}
	return row.Entity, nil
}

// ListProjectsResult is ListProjects' output.
type ListProjectsResult struct {
	Projects   []domain.Project
	NextCursor string
}

const defaultPageLimit = 20 // product spec §11: MCP/CLI lists default to at most 20 compact records
const maxPageLimit = 100

// ListProjects returns a cursor-paginated, compact page of projects
// ordered by (created_at, id). includeArchived false (the default)
// restricts to active projects (ADR 0021); true also returns archived
// ones.
func (s *Service) ListProjects(ctx context.Context, limit int, cursor string, includeArchived bool) (ListProjectsResult, error) {
	afterCreatedAt, afterID, err := store.DecodeCreatedAtIDCursor(cursor)
	if err != nil {
		return ListProjectsResult{}, newValidationError("cursor", "invalid cursor")
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	page, err := store.ListProjects(ctx, s.store.DB(), limit, afterCreatedAt, afterID, includeArchived)
	if err != nil {
		return ListProjectsResult{}, fmt.Errorf("service: list projects: %w", err)
	}
	return ListProjectsResult{Projects: page.Projects, NextCursor: page.NextCursor}, nil
}

// UpdateProjectRequest is UpdateProject's input.
type UpdateProjectRequest struct {
	Key             string
	Title           string
	Description     string
	ExpectedVersion int64
}

// UpdateProject applies a conditional title/description update (ADR
// 0008), mirroring UpdateFeature. Status is not settable here — see
// SetProjectStatus — matching the same split UpdateFeature/
// UpdateFeatureStatus and UpdateTicketFields/UpdateTicketStatus use
// elsewhere, so a plain field edit never risks clobbering a concurrent
// archive/unarchive.
func (s *Service) UpdateProject(ctx context.Context, req UpdateProjectRequest, actor domain.ActorRef, correlationID string) (domain.Project, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Project{}, newValidationError("title", "title is required")
	}

	var result domain.Project
	var notifiedIDs []int64
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetProjectByKey(ctx, tx, req.Key)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("project %q not found", req.Key)
		}
		if err != nil {
			return fmt.Errorf("service: look up project: %w", err)
		}

		if _, err := store.UpdateProjectFields(ctx, tx, row.ID, title, req.Description, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update project: %w", err)
		}
		mentioned, err := rescanMentions(ctx, tx, row.ID, sourceOwnBody, req.Key, req.Description, now, actorID)
		if err != nil {
			return err
		}
		notifiedIDs = mentioned

		changes := auditChanges(map[string]any{"title": title})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventProjectUpdated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetProjectByKey(ctx, tx, req.Key)
		if err != nil {
			return fmt.Errorf("service: reload updated project: %w", err)
		}
		if err := indexProjectSearchDoc(ctx, tx, row.ID, updated.Entity); err != nil {
			return err
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Project{}, err
	}
	s.broadcast(ChangeHint{Kind: HintEntityChanged, Ref: result.Key, Project: result.Key})
	s.publishNotified(ctx, notifiedIDs)
	return result, nil
}

// SetProjectStatusRequest is SetProjectStatus's input.
type SetProjectStatusRequest struct {
	Key             string
	NewStatus       domain.ProjectStatus
	ExpectedVersion int64
}

// SetProjectStatus archives or unarchives a project (ADR 0021):
// visibility only. It does not soft-delete the project (ADR 0013) and
// does not cascade to its tickets, features, or knowledge records,
// which stay fully readable and writable regardless of the project's
// own status.
func (s *Service) SetProjectStatus(ctx context.Context, req SetProjectStatusRequest, actor domain.ActorRef, correlationID string) (domain.Project, error) {
	if !req.NewStatus.Valid() {
		return domain.Project{}, newValidationError("status", "invalid status %q", req.NewStatus)
	}

	var result domain.Project
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetProjectByKey(ctx, tx, req.Key)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("project %q not found", req.Key)
		}
		if err != nil {
			return fmt.Errorf("service: look up project: %w", err)
		}

		if _, err := store.UpdateProjectStatus(ctx, tx, row.ID, string(req.NewStatus), req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update project status: %w", err)
		}

		eventType := eventProjectArchived
		if req.NewStatus == domain.ProjectStatusActive {
			eventType = eventProjectUnarchived
		}
		changes := auditChanges(map[string]any{"status": string(req.NewStatus)})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventType, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetProjectByKey(ctx, tx, req.Key)
		if err != nil {
			return fmt.Errorf("service: reload updated project: %w", err)
		}
		if err := indexProjectSearchDoc(ctx, tx, row.ID, updated.Entity); err != nil {
			return err
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Project{}, err
	}
	s.broadcast(ChangeHint{Kind: HintEntityChanged, Ref: result.Key, Project: result.Key})
	return result, nil
}
