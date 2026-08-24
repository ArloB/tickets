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
	ProjectKey   string
	Title        string
	Context      string
	Decision     string
	Rationale    string
	Consequences string
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
		if err := store.InsertDecision(ctx, tx, decisionEntityID, proj.ID, seq, title, req.Context, req.Decision, req.Rationale, req.Consequences, string(domain.DecisionStatusProposed)); err != nil {
			return fmt.Errorf("service: create decision: %w", err)
		}
		if err := rescanMentions(ctx, tx, decisionEntityID, sourceOwnBody, req.ProjectKey, req.Context+"\n"+req.Decision+"\n"+req.Rationale+"\n"+req.Consequences, now, actorID); err != nil {
			return err
		}

		ref := domain.Reference{ProjectKey: req.ProjectKey, Kind: domain.KindDecision, Seq: seq}
		refStr, err := domain.Format(ref)
		if err != nil {
			return fmt.Errorf("service: format created decision ref: %w", err)
		}
		if err := indexDecisionSearchDoc(ctx, tx, decisionEntityID, proj.ID, domain.Decision{
			Ref: refStr, Status: domain.DecisionStatusProposed, Title: title,
			Context: req.Context, Decision: req.Decision, Rationale: req.Rationale, Consequences: req.Consequences,
		}); err != nil {
			return err
		}
		if err := subscribe(ctx, tx, decisionEntityID, actorID, now); err != nil {
			return err
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

// resolveDecisionEntity fills in a store.DecisionRow's SupersededBy
// public reference from its raw SupersededByID, since a DecisionRow
// straight off the store layer isn't wire-safe on its own (the store
// layer never joins across to format a ref itself — see DecisionRow's
// doc). Used by every single-decision read path (GetDecision,
// CreateDecision/UpdateDecision's return value); ListDecisions
// deliberately does *not* call this per-row, since decisionCompact
// never exposes superseded_by and resolving it for every list row
// would be an unnecessary extra query per row for a field the response
// throws away.
func resolveDecisionEntity(ctx context.Context, q store.Querier, row store.DecisionRow) (domain.Decision, error) {
	d := row.Entity
	if row.SupersededByID != nil {
		ref, err := store.GetDecisionRefByEntityIDAnyDeletion(ctx, q, *row.SupersededByID)
		if err != nil {
			return domain.Decision{}, fmt.Errorf("service: resolve decision superseded_by: %w", err)
		}
		formatted, err := domain.Format(ref)
		if err != nil {
			return domain.Decision{}, fmt.Errorf("service: format decision superseded_by ref: %w", err)
		}
		d.SupersededBy = &formatted
	}
	return d, nil
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
	return resolveDecisionEntity(ctx, s.store.DB(), row)
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

// UpdateDecisionRequest is UpdateDecision's input. SupersededBy is a
// decision reference string ("" clears any existing link) — this is a
// full-representation update, so an omitted field is indistinguishable
// from a deliberate clear, matching every other field here.
type UpdateDecisionRequest struct {
	Ref             domain.Reference
	Title           string
	Context         string
	Decision        string
	Rationale       string
	Consequences    string
	Status          domain.DecisionStatus
	SupersededBy    string
	ExpectedVersion int64
}

// UpdateDecision applies a conditional field update (ADR 0008),
// archiving the pre-update row into decision_versions first (§5.8:
// "accepted decisions remain editable only by creating a new version,
// and every version remains visible") — the same
// snapshot-then-overwrite order EditComment uses for comment_versions.
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
		if row.Entity.Version != req.ExpectedVersion {
			return newVersionConflictError(row.Entity.Version)
		}

		var supersededByID *int64
		if req.SupersededBy != "" {
			target, terr := domain.Parse(req.SupersededBy)
			if terr != nil || target.Kind != domain.KindDecision {
				return newValidationError("superseded_by", "invalid decision reference %q", req.SupersededBy)
			}
			if target.ProjectKey != row.Entity.ProjectKey {
				return newValidationError("superseded_by", "decision %q does not belong to project %q", req.SupersededBy, row.Entity.ProjectKey)
			}
			if target.Seq == req.Ref.Seq {
				return newValidationError("superseded_by", "a decision cannot supersede itself")
			}
			targetRow, gerr := store.GetDecisionByRef(ctx, tx, target)
			if errors.Is(gerr, store.ErrNotFound) {
				return newValidationError("superseded_by", "decision %q not found", req.SupersededBy)
			}
			if gerr != nil {
				return fmt.Errorf("service: look up superseded_by decision: %w", gerr)
			}
			supersededByID = &targetRow.ID
		}

		if err := store.InsertDecisionVersion(ctx, tx, row.ID, row.Entity.Version,
			row.Entity.Title, row.Entity.Context, row.Entity.Decision, row.Entity.Rationale, row.Entity.Consequences, string(row.Entity.Status),
			actorID, now); err != nil {
			return fmt.Errorf("service: archive decision version: %w", err)
		}

		if _, err := store.UpdateDecisionFields(ctx, tx, row.ID, title, req.Context, req.Decision, req.Rationale, req.Consequences, string(req.Status), supersededByID, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update decision: %w", err)
		}
		if err := rescanMentions(ctx, tx, row.ID, sourceOwnBody, row.Entity.ProjectKey, req.Context+"\n"+req.Decision+"\n"+req.Rationale+"\n"+req.Consequences, now, actorID); err != nil {
			return err
		}

		changes := auditChanges(map[string]any{"title": title, "status": string(req.Status)})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventDecisionUpdated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		if err := notifySubscribers(ctx, tx, row.ID, notificationKindChanged, sourceOwnBody, actorID, now); err != nil {
			return err
		}

		updated, err := store.GetDecisionByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated decision: %w", err)
		}
		result, err = resolveDecisionEntity(ctx, tx, updated)
		if err != nil {
			return err
		}
		if err := indexDecisionSearchDoc(ctx, tx, row.ID, row.ProjectEntityID, result); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domain.Decision{}, err
	}
	return result, nil
}

// ListDecisionVersions returns a decision's archived prior states,
// oldest first (§5.8). Does not include the live state — a caller
// wanting "every version" combines this with GetDecision's current
// result, mirroring how comment history works.
func (s *Service) ListDecisionVersions(ctx context.Context, ref domain.Reference) ([]domain.DecisionVersion, error) {
	row, err := store.GetDecisionByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("decision not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: look up decision: %w", err)
	}
	versions, err := store.ListDecisionVersions(ctx, s.store.DB(), row.ID)
	if err != nil {
		return nil, fmt.Errorf("service: list decision versions: %w", err)
	}
	return versions, nil
}

// DecisionFieldDiff is one text field's line-level diff between two
// versions.
type DecisionFieldDiff struct {
	Title        []domain.DiffLine
	Context      []domain.DiffLine
	Decision     []domain.DiffLine
	Rationale    []domain.DiffLine
	Consequences []domain.DiffLine
}

// DecisionDiff is GetDecisionDiff's output: a per-field line diff
// between two named versions, plus the simple before/after status
// values (a status transition isn't meaningfully "line-diffed").
type DecisionDiff struct {
	FromVersion int64
	ToVersion   int64
	Fields      DecisionFieldDiff
	StatusFrom  domain.DecisionStatus
	StatusTo    domain.DecisionStatus
}

// decisionVersionState is the common shape GetDecisionDiff needs from
// either an archived decision_versions row or the live decisions row —
// letting it treat "diff against the current state" and "diff between
// two archived versions" as the same code path.
type decisionVersionState struct {
	Title, Context, Decision, Rationale, Consequences string
	Status                                            domain.DecisionStatus
}

// GetDecisionDiff computes a line-level diff (§5.9: "the UI computes a
// line-level diff between versions") between decision version numbers
// from and to. Either may name the live version (row.Entity.Version) or
// any archived decision_versions row; a version number outside
// [1, live version] is not_found, since it never existed.
func (s *Service) GetDecisionDiff(ctx context.Context, ref domain.Reference, from, to int64) (DecisionDiff, error) {
	row, err := store.GetDecisionByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return DecisionDiff{}, newNotFoundError("decision not found")
	}
	if err != nil {
		return DecisionDiff{}, fmt.Errorf("service: look up decision: %w", err)
	}

	fromState, err := s.decisionStateAtVersion(ctx, row, from)
	if err != nil {
		return DecisionDiff{}, err
	}
	toState, err := s.decisionStateAtVersion(ctx, row, to)
	if err != nil {
		return DecisionDiff{}, err
	}

	return DecisionDiff{
		FromVersion: from,
		ToVersion:   to,
		Fields: DecisionFieldDiff{
			Title:        domain.LineDiff(fromState.Title, toState.Title),
			Context:      domain.LineDiff(fromState.Context, toState.Context),
			Decision:     domain.LineDiff(fromState.Decision, toState.Decision),
			Rationale:    domain.LineDiff(fromState.Rationale, toState.Rationale),
			Consequences: domain.LineDiff(fromState.Consequences, toState.Consequences),
		},
		StatusFrom: fromState.Status,
		StatusTo:   toState.Status,
	}, nil
}

func (s *Service) decisionStateAtVersion(ctx context.Context, row store.DecisionRow, version int64) (decisionVersionState, error) {
	if version == row.Entity.Version {
		return decisionVersionState{
			Title: row.Entity.Title, Context: row.Entity.Context, Decision: row.Entity.Decision,
			Rationale: row.Entity.Rationale, Consequences: row.Entity.Consequences, Status: row.Entity.Status,
		}, nil
	}
	if version < 1 || version > row.Entity.Version {
		return decisionVersionState{}, newValidationError("version", "decision %s has no version %d", row.Entity.Ref, version)
	}
	v, err := store.GetDecisionVersion(ctx, s.store.DB(), row.ID, version)
	if errors.Is(err, store.ErrNotFound) {
		return decisionVersionState{}, newValidationError("version", "decision %s has no version %d", row.Entity.Ref, version)
	}
	if err != nil {
		return decisionVersionState{}, fmt.Errorf("service: get decision version: %w", err)
	}
	return decisionVersionState{
		Title: v.Title, Context: v.Context, Decision: v.Decision,
		Rationale: v.Rationale, Consequences: v.Consequences, Status: v.Status,
	}, nil
}
