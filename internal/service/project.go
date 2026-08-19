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

	key, err := s.createProjectTx(ctx, req, title, actor, correlationID, idemKey, fingerprint)
	if err != nil {
		return domain.Project{}, err
	}
	return s.GetProject(ctx, key)
}

func (s *Service) createProjectTx(ctx context.Context, req CreateProjectRequest, title string, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (string, error) {
	var result string
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if cached, found, err := checkIdempotency(ctx, tx, idemKey, fingerprint); err != nil {
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
		if err := rescanMentions(ctx, tx, projectEntityID, sourceOwnBody, req.Key, req.Description, now); err != nil {
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

		changes := auditChanges(map[string]any{"key": req.Key, "title": title})
		if err := store.InsertAuditEvent(ctx, tx, projectEntityID, actorID, eventProjectCreated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		if err := recordIdempotency(ctx, tx, idemKey, fingerprint, req.Key, now); err != nil {
			return err
		}
		result = req.Key
		return nil
	})
	if err != nil {
		return "", err
	}
	return result, nil
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
// ordered by (created_at, id).
func (s *Service) ListProjects(ctx context.Context, limit int, cursor string) (ListProjectsResult, error) {
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

	page, err := store.ListProjects(ctx, s.store.DB(), limit, afterCreatedAt, afterID)
	if err != nil {
		return ListProjectsResult{}, fmt.Errorf("service: list projects: %w", err)
	}
	return ListProjectsResult{Projects: page.Projects, NextCursor: page.NextCursor}, nil
}
