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

// CreateDecisionRequest is CreateDecision's input.
type CreateDecisionRequest struct {
	ProjectKey string
	Title      string
	Context    string
	Decision   string
	Rationale  string
}

// CreateDecision allocates a reference and creates a decision in the
// given project, attributed to actor. Unlike CreateFeature's Phase 1
// decision to skip Idempotency-Key handling (nothing called it over
// the network yet), this is wired in from day one: Phase 3 builds
// decisions with CLI/MCP callers — real network callers that need
// retry-safety — already in mind.
func (s *Service) CreateDecision(ctx context.Context, req CreateDecisionRequest, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (domain.Decision, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Decision{}, newValidationError("title", "title is required")
	}

	var result domain.Reference
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if cached, found, err := checkIdempotency(ctx, tx, idemKey, actorID, fingerprint); err != nil {
			return err
		} else if found {
			ref, perr := domain.Parse(cached)
			if perr != nil {
				return fmt.Errorf("service: parse cached decision ref %q: %w", cached, perr)
			}
			result = ref
			return nil // no writes happened on this path; committing is a no-op
		}

		proj, err := store.GetProjectByKey(ctx, tx, req.ProjectKey)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("project %q not found", req.ProjectKey)
		}
		if err != nil {
			return fmt.Errorf("service: look up project: %w", err)
		}

		decisionEntityID, _, err := store.InsertEntity(ctx, tx, &proj.ID, domain.KindDecision, actorID, now)
		if err != nil {
			return fmt.Errorf("service: create decision entity: %w", err)
		}
		seq, err := store.AllocateReference(ctx, tx, proj.ID, domain.KindDecision)
		if err != nil {
			return fmt.Errorf("service: allocate decision reference: %w", err)
		}
		if err := store.InsertDecision(ctx, tx, decisionEntityID, proj.ID, seq, title, req.Context, req.Decision, req.Rationale, string(domain.DecisionStatusProposed)); err != nil {
			return fmt.Errorf("service: create decision: %w", err)
		}
		if err := rescanMentions(ctx, tx, decisionEntityID, sourceOwnBody, req.ProjectKey, req.Context+"\n"+req.Decision+"\n"+req.Rationale, now); err != nil {
			return err
		}

		ref := domain.Reference{ProjectKey: req.ProjectKey, Kind: domain.KindDecision, Seq: seq}
		refStr, err := domain.Format(ref)
		if err != nil {
			return fmt.Errorf("service: format created decision ref: %w", err)
		}

		changes := auditChanges(map[string]any{"ref": refStr, "title": title})
		if err := store.InsertAuditEvent(ctx, tx, decisionEntityID, actorID, eventDecisionCreated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		if err := recordIdempotency(ctx, tx, idemKey, actorID, fingerprint, refStr, now); err != nil {
			return err
		}
		result = ref
		return nil
	})
	if err != nil {
		return domain.Decision{}, err
	}
	return s.GetDecision(ctx, result)
}

// GetDecision looks up a decision by its parsed reference.
func (s *Service) GetDecision(ctx context.Context, ref domain.Reference) (domain.Decision, error) {
	row, err := store.GetDecisionByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Decision{}, newNotFoundError("decision not found")
	}
	if err != nil {
		return domain.Decision{}, fmt.Errorf("service: get decision: %w", err)
	}
	return row.Entity, nil
}

// DecisionsListResult is ListDecisions' output.
type DecisionsListResult struct {
	Decisions  []domain.Decision
	NextCursor string
}

// ListDecisions returns a cursor-paginated page of a project's
// non-deleted decisions, ordered by creation time (§5.8 has no
// priority/position concept for decisions).
func (s *Service) ListDecisions(ctx context.Context, projectKey string, limit int, cursor string) (DecisionsListResult, error) {
	proj, err := store.GetProjectByKey(ctx, s.store.DB(), projectKey)
	if errors.Is(err, store.ErrNotFound) {
		return DecisionsListResult{}, newNotFoundError("project %q not found", projectKey)
	}
	if err != nil {
		return DecisionsListResult{}, fmt.Errorf("service: look up project: %w", err)
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	afterCreatedAt, afterID, derr := store.DecodeCreatedAtIDCursor(cursor)
	if derr != nil {
		return DecisionsListResult{}, newValidationError("cursor", "invalid cursor")
	}
	page, err := store.ListDecisionsForProjectPage(ctx, s.store.DB(), proj.ID, limit, afterCreatedAt, afterID)
	if err != nil {
		return DecisionsListResult{}, fmt.Errorf("service: list decisions: %w", err)
	}

	out := make([]domain.Decision, len(page.Decisions))
	for i, row := range page.Decisions {
		out[i] = row.Entity
	}
	return DecisionsListResult{Decisions: out, NextCursor: page.NextCursor}, nil
}

// UpdateDecisionRequest is UpdateDecision's input.
type UpdateDecisionRequest struct {
	Ref             domain.Reference
	Title           string
	Context         string
	Decision        string
	Rationale       string
	Status          domain.DecisionStatus
	ExpectedVersion int64
}

// UpdateDecision applies a conditional field update (ADR 0008) — a
// plain overwrite, not a versioned/archived edit: unlike EditComment,
// Phase 3's decisions have no edit-history requirement of their own
// (that, plus supersession-linking, is Phase 5's extension of this
// same table).
func (s *Service) UpdateDecision(ctx context.Context, req UpdateDecisionRequest, actor domain.ActorRef, correlationID string) (domain.Decision, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Decision{}, newValidationError("title", "title is required")
	}
	if !req.Status.Valid() {
		return domain.Decision{}, newValidationError("status", "invalid status %q", req.Status)
	}

	var result domain.Decision
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetDecisionByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("decision not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up decision: %w", err)
		}

		if _, err := store.UpdateDecisionFields(ctx, tx, row.ID, title, req.Context, req.Decision, req.Rationale, string(req.Status), req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update decision: %w", err)
		}
		if err := rescanMentions(ctx, tx, row.ID, sourceOwnBody, row.Entity.ProjectKey, req.Context+"\n"+req.Decision+"\n"+req.Rationale, now); err != nil {
			return err
		}

		changes := auditChanges(map[string]any{"title": title, "status": string(req.Status)})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventDecisionUpdated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetDecisionByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated decision: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Decision{}, err
	}
	return result, nil
}
