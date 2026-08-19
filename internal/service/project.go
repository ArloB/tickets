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

const generalFeatureKind = "feature"
const generalFeatureTitle = "General"

// CreateProjectRequest is CreateProject's input.
type CreateProjectRequest struct {
	Key         string
	Title       string
	Description string
}

// CreateProject creates a project and its mandatory General feature
// (ADR 0001) in one transaction. idemKey/fingerprint may be empty,
// meaning the caller supplied no Idempotency-Key.
func (s *Service) CreateProject(ctx context.Context, req CreateProjectRequest, idemKey, fingerprint string) (domain.Project, error) {
	if !domain.ValidProjectKey(req.Key) {
		return domain.Project{}, newValidationError("key", "project key must be 2-10 uppercase letters/digits starting with a letter")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Project{}, newValidationError("title", "title is required")
	}

	return withIdempotency(ctx, s.store.DB(), idemKey, fingerprint, func(tx *sql.Tx) (domain.Project, error) {
		if _, err := store.GetProjectByKey(ctx, tx, req.Key); err == nil {
			return domain.Project{}, newAlreadyExistsError("key", "a project with key %q already exists", req.Key)
		} else if !errors.Is(err, store.ErrNotFound) {
			return domain.Project{}, fmt.Errorf("service: check existing project: %w", err)
		}

		projectEntityID, _, err := store.InsertEntity(ctx, tx, nil, "project")
		if err != nil {
			return domain.Project{}, fmt.Errorf("service: create project entity: %w", err)
		}
		if err := store.InsertProject(ctx, tx, projectEntityID, req.Key, title, req.Description); err != nil {
			return domain.Project{}, fmt.Errorf("service: create project: %w", err)
		}

		// Mandatory General feature (ADR 0001), created in the same
		// transaction so a project never briefly exists without one.
		featureEntityID, _, err := store.InsertEntity(ctx, tx, &projectEntityID, generalFeatureKind)
		if err != nil {
			return domain.Project{}, fmt.Errorf("service: create general feature entity: %w", err)
		}
		featureSeq, err := store.AllocateReference(ctx, tx, projectEntityID, generalFeatureKind)
		if err != nil {
			return domain.Project{}, fmt.Errorf("service: allocate general feature reference: %w", err)
		}
		if err := store.InsertFeature(ctx, tx, featureEntityID, projectEntityID, featureSeq, generalFeatureTitle); err != nil {
			return domain.Project{}, fmt.Errorf("service: create general feature: %w", err)
		}
		if err := store.SetProjectGeneralFeature(ctx, tx, projectEntityID, featureEntityID); err != nil {
			return domain.Project{}, fmt.Errorf("service: link general feature: %w", err)
		}

		row, err := store.GetProjectByKey(ctx, tx, req.Key)
		if err != nil {
			return domain.Project{}, fmt.Errorf("service: reload created project: %w", err)
		}
		return row.Entity, nil
	})
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
	afterCreatedAt, afterID, err := store.DecodeCursor(cursor)
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
