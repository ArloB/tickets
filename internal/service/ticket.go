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

// CreateTicketRequest is CreateTicket's input. There is no feature
// field: Phase 0's slice only implements product spec §6.1's "create a
// ticket from a project without manually selecting a feature" path —
// every ticket lands in the project's General feature. Explicit
// feature selection and moving a ticket between features are Phase 1
// work (see the Phase 0 plan's deferral list).
type CreateTicketRequest struct {
	ProjectKey  string
	Type        domain.TicketType
	Title       string
	Description string
	Priority    domain.Priority
	Severity    *domain.Severity
}

// CreateTicket allocates a reference and creates a ticket in its
// project's General feature, in one transaction, attributed to actor
// (ADR 0012) and tagged with correlationID on its audit event.
//
// Like CreateProject, the idempotency cache stores only the created
// ticket's ref, never a snapshot of the response — see idempotency.go.
func (s *Service) CreateTicket(ctx context.Context, req CreateTicketRequest, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (domain.Ticket, error) {
	if !req.Type.Valid() {
		return domain.Ticket{}, newValidationError("type", "invalid ticket type %q", req.Type)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Ticket{}, newValidationError("title", "title is required")
	}
	priority := req.Priority
	if priority == "" {
		priority = domain.PriorityMedium
	}
	if !priority.Valid() {
		return domain.Ticket{}, newValidationError("priority", "invalid priority %q", req.Priority)
	}
	if req.Severity != nil {
		if !req.Severity.Valid() {
			return domain.Ticket{}, newValidationError("severity", "invalid severity %q", *req.Severity)
		}
		if !req.Type.AllowsSeverity() {
			return domain.Ticket{}, newValidationError("severity", "severity only applies to bug/security tickets, not %q", req.Type)
		}
	}

	ref, err := s.createTicketTx(ctx, req, title, priority, actor, correlationID, idemKey, fingerprint)
	if err != nil {
		return domain.Ticket{}, err
	}
	return s.GetTicket(ctx, ref)
}

func (s *Service) createTicketTx(ctx context.Context, req CreateTicketRequest, title string, priority domain.Priority, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (domain.Reference, error) {
	var result domain.Reference
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if cached, found, err := checkIdempotency(ctx, tx, idemKey, fingerprint); err != nil {
			return err
		} else if found {
			ref, perr := domain.Parse(cached)
			if perr != nil {
				return fmt.Errorf("service: cached idempotent ref %q is invalid: %w", cached, perr)
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
		if proj.GeneralFeatureID == 0 {
			return fmt.Errorf("service: project %q has no general feature (data integrity)", req.ProjectKey)
		}

		ticketEntityID, _, err := store.InsertEntity(ctx, tx, &proj.ID, domain.KindTicket, actorID, now)
		if err != nil {
			return fmt.Errorf("service: create ticket entity: %w", err)
		}
		seq, err := store.AllocateReference(ctx, tx, proj.ID, domain.KindTicket)
		if err != nil {
			return fmt.Errorf("service: allocate ticket reference: %w", err)
		}

		var severityStr *string
		if req.Severity != nil {
			v := string(*req.Severity)
			severityStr = &v
		}
		maxPos, err := store.TicketGroupMaxPositionByPriority(ctx, tx, proj.ID, string(priority))
		if err != nil {
			return fmt.Errorf("service: load priority group: %w", err)
		}
		if err := store.InsertTicket(ctx, tx, ticketEntityID, proj.ID, proj.GeneralFeatureID, seq,
			string(req.Type), title, req.Description, string(domain.WorkflowStatusBacklog), string(priority), severityStr, domain.TailPosition(maxPos)); err != nil {
			return fmt.Errorf("service: create ticket: %w", err)
		}
		if err := rescanMentions(ctx, tx, ticketEntityID, sourceOwnBody, req.ProjectKey, req.Description, now); err != nil {
			return err
		}

		ref := domain.Reference{ProjectKey: req.ProjectKey, Kind: domain.KindTicket, Seq: seq}
		refStr, err := domain.Format(ref)
		if err != nil {
			return fmt.Errorf("service: format created ticket ref: %w", err)
		}

		changes := auditChanges(map[string]any{"ref": refStr, "type": string(req.Type), "title": title})
		if err := store.InsertAuditEvent(ctx, tx, ticketEntityID, actorID, eventTicketCreated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		if err := recordIdempotency(ctx, tx, idemKey, fingerprint, refStr, now); err != nil {
			return err
		}
		result = ref
		return nil
	})
	if err != nil {
		return domain.Reference{}, err
	}
	return result, nil
}

// GetTicket looks up a ticket by its parsed reference.
func (s *Service) GetTicket(ctx context.Context, ref domain.Reference) (domain.Ticket, error) {
	row, err := store.GetTicketByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Ticket{}, newNotFoundError("ticket not found")
	}
	if err != nil {
		return domain.Ticket{}, fmt.Errorf("service: get ticket: %w", err)
	}
	return row.Entity, nil
}

// UpdateTicketStatusRequest is UpdateTicketStatus's input.
type UpdateTicketStatusRequest struct {
	Ref             domain.Reference
	NewStatus       domain.WorkflowStatus
	ExpectedVersion int64
}

// UpdateTicketStatus applies a conditional status update (ADR 0008 /
// docs/contracts/concurrency.md). Product spec §5.6 permits transitions
// between any two states — the server records every transition rather
// than validating a transition graph; that's a client/UI concern.
//
// No Idempotency-Key handling here: unlike the two create endpoints,
// a status update is already made duplicate-safe by the version check
// itself (a stale retry fails with version_conflict rather than
// double-applying), so this endpoint relies on If-Match alone.
func (s *Service) UpdateTicketStatus(ctx context.Context, req UpdateTicketStatusRequest, actor domain.ActorRef, correlationID string) (domain.Ticket, error) {
	if !req.NewStatus.Valid() {
		return domain.Ticket{}, newValidationError("status", "invalid status %q", req.NewStatus)
	}

	var result domain.Ticket
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}
		fromStatus := row.Entity.Status

		if _, err := store.UpdateTicketStatus(ctx, tx, row.ID, string(req.NewStatus), req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update ticket status: %w", err)
		}

		changes := auditChanges(map[string]any{"from": string(fromStatus), "to": string(req.NewStatus)})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventTicketStatusChanged, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated ticket: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return result, nil
}

// UpdateTicketFieldsRequest is UpdateTicketFields' input.
type UpdateTicketFieldsRequest struct {
	Ref             domain.Reference
	Type            domain.TicketType
	Title           string
	Description     string
	Priority        domain.Priority
	Severity        *domain.Severity
	ExpectedVersion int64
}

// UpdateTicketFields applies a conditional update to a ticket's
// type/title/description/priority/severity (ADR 0008). Unlike
// UpdateTicketStatus, this does not accept an Idempotency-Key — the
// version check already makes a stale retry safe, matching the
// existing status-update endpoint's reasoning.
func (s *Service) UpdateTicketFields(ctx context.Context, req UpdateTicketFieldsRequest, actor domain.ActorRef, correlationID string) (domain.Ticket, error) {
	if !req.Type.Valid() {
		return domain.Ticket{}, newValidationError("type", "invalid ticket type %q", req.Type)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Ticket{}, newValidationError("title", "title is required")
	}
	if !req.Priority.Valid() {
		return domain.Ticket{}, newValidationError("priority", "invalid priority %q", req.Priority)
	}
	if req.Severity != nil {
		if !req.Severity.Valid() {
			return domain.Ticket{}, newValidationError("severity", "invalid severity %q", *req.Severity)
		}
		if !req.Type.AllowsSeverity() {
			return domain.Ticket{}, newValidationError("severity", "severity only applies to bug/security tickets, not %q", req.Type)
		}
	}

	var result domain.Ticket
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}

		var severityStr *string
		if req.Severity != nil {
			v := string(*req.Severity)
			severityStr = &v
		}
		newPosition := row.Position
		if req.Priority != row.Entity.Priority {
			maxPos, err := store.TicketGroupMaxPositionByPriority(ctx, tx, row.ProjectEntityID, string(req.Priority))
			if err != nil {
				return fmt.Errorf("service: load new priority group: %w", err)
			}
			newPosition = domain.TailPosition(maxPos)
		}
		if _, err := store.UpdateTicketFields(ctx, tx, row.ID, string(req.Type), title, req.Description, string(req.Priority), severityStr, newPosition, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update ticket: %w", err)
		}
		if err := rescanMentions(ctx, tx, row.ID, sourceOwnBody, row.Entity.ProjectKey, req.Description, now); err != nil {
			return err
		}

		changes := auditChanges(map[string]any{"title": title, "type": string(req.Type), "priority": string(req.Priority)})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventTicketUpdated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated ticket: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return result, nil
}

// AssignTicketRequest is AssignTicket's input. Assignee nil clears the
// assignment.
type AssignTicketRequest struct {
	Ref             domain.Reference
	Assignee        *domain.ActorRef
	ExpectedVersion int64
}

// AssignTicket sets or clears a ticket's assignee. There is no actor-
// creation surface yet (store.GetActorIDByRef's doc explains why), so
// in practice the assignee must already exist as a seeded actor —
// unassigning is always available regardless.
func (s *Service) AssignTicket(ctx context.Context, req AssignTicketRequest, actor domain.ActorRef, correlationID string) (domain.Ticket, error) {
	var result domain.Ticket
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}

		var assigneeID *int64
		changesVal := "unassigned"
		if req.Assignee != nil {
			id, err := store.GetActorIDByRef(ctx, tx, req.Assignee.Kind, req.Assignee.Name)
			if errors.Is(err, store.ErrNotFound) {
				return newValidationError("assignee", "actor %s not found", req.Assignee)
			}
			if err != nil {
				return fmt.Errorf("service: resolve assignee: %w", err)
			}
			assigneeID = &id
			changesVal = req.Assignee.String()
		}

		if _, err := store.AssignTicket(ctx, tx, row.ID, assigneeID, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: assign ticket: %w", err)
		}

		changes := auditChanges(map[string]any{"assignee": changesVal})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventTicketAssigned, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated ticket: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return result, nil
}

// MoveTicketFeatureRequest is MoveTicketFeature's input.
type MoveTicketFeatureRequest struct {
	Ref             domain.Reference
	NewFeatureRef   domain.Reference
	ExpectedVersion int64
}

// MoveTicketFeature moves a ticket to a different feature in the same
// project. ADR 0001 forbids a cross-project move — the new feature's
// project must match the ticket's, checked here since it needs both
// rows loaded, not at the domain layer.
func (s *Service) MoveTicketFeature(ctx context.Context, req MoveTicketFeatureRequest, actor domain.ActorRef, correlationID string) (domain.Ticket, error) {
	var result domain.Ticket
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}
		feature, err := store.GetFeatureByRef(ctx, tx, req.NewFeatureRef)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("feature not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up feature: %w", err)
		}
		if feature.ProjectEntityID != row.ProjectEntityID {
			return newValidationError("feature", "feature %s is not in the same project as ticket %s", feature.Entity.Ref, row.Entity.Ref)
		}

		if _, err := store.MoveTicketFeature(ctx, tx, row.ID, feature.ID, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: move ticket: %w", err)
		}

		changes := auditChanges(map[string]any{"feature": feature.Entity.Ref})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventTicketMoved, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated ticket: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return result, nil
}

// ReorderTicketRequest is ReorderTicket's input. AfterRef nil means
// "move to the head of the ticket's own (project, priority) group";
// otherwise the ticket is placed immediately after AfterRef, which
// must be in the same group. Reordering never changes priority — see
// UpdateTicketFields for the "changing priority moves to the tail of
// the new group" rule (ADR 0011).
type ReorderTicketRequest struct {
	Ref             domain.Reference
	AfterRef        *domain.Reference
	ExpectedVersion int64
}

// ReorderTicket moves a ticket to a new position within its priority
// group (product spec §5.6, ADR 0011): a head/tail append or
// domain.MidpointPosition insertion when there's room, or a full
// group renumber when the gap between the target neighbors is
// exhausted. A renumber writes every other group member's position
// with no version bump and no audit event (store.SetTicketPositionUnversioned)
// — only the ticket the caller actually moved gets a versioned,
// audited write, so a renumber triggered by one drag-and-drop doesn't
// invalidate every other open If-Match in the group.
func (s *Service) ReorderTicket(ctx context.Context, req ReorderTicketRequest, actor domain.ActorRef, correlationID string) (domain.Ticket, error) {
	var result domain.Ticket
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}

		others, err := store.TicketGroupOrderedExcluding(ctx, tx, row.ProjectEntityID, row.PriorityRank, row.ID)
		if err != nil {
			return fmt.Errorf("service: load priority group: %w", err)
		}

		insertIdx := 0
		if req.AfterRef != nil {
			anchor, err := store.GetTicketByRef(ctx, tx, *req.AfterRef)
			if errors.Is(err, store.ErrNotFound) {
				return newNotFoundError("after ticket not found")
			}
			if err != nil {
				return fmt.Errorf("service: look up after ticket: %w", err)
			}
			if anchor.ID == row.ID {
				return newValidationError("after", "cannot reorder a ticket after itself")
			}
			if anchor.ProjectEntityID != row.ProjectEntityID || anchor.PriorityRank != row.PriorityRank {
				return newValidationError("after", "after ticket %s is not in the same priority group", anchor.Entity.Ref)
			}
			idx := -1
			for i, m := range others {
				if m.EntityID == anchor.ID {
					idx = i
					break
				}
			}
			if idx == -1 {
				return fmt.Errorf("service: after ticket %d not found in its own priority group (data integrity)", anchor.ID)
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
				if err := store.SetTicketPositionUnversioned(ctx, tx, id, positions[i]); err != nil {
					return fmt.Errorf("service: renumber priority group: %w", err)
				}
			}
		}

		if _, err := store.SetTicketPositionVersioned(ctx, tx, row.ID, newPos, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: set ticket position: %w", err)
		}

		reorderChanges := auditChanges(map[string]any{"position": newPos})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventTicketReordered, corrID, nil, reorderChanges, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload reordered ticket: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return result, nil
}

// DeleteTicketRequest is DeleteTicket's input.
type DeleteTicketRequest struct {
	Ref             domain.Reference
	ExpectedVersion int64
}

// DeleteTicket soft-deletes a ticket (ADR 0013). Unlike a feature, a
// ticket has no dependents that could block deletion — its comments,
// relationships, and mentions aren't blocking dependencies, they're
// simply filtered out of every read path once the ticket itself is
// gone (the same pattern already applied to relationship/mention
// listings that point at a deleted endpoint).
func (s *Service) DeleteTicket(ctx context.Context, req DeleteTicketRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}

		if _, err := store.SoftDeleteEntity(ctx, tx, row.ID, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: soft-delete ticket: %w", err)
		}

		changes := auditChanges(map[string]any{"ref": row.Entity.Ref})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventTicketDeleted, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
}

// RestoreTicketRequest is RestoreTicket's input.
type RestoreTicketRequest struct {
	Ref             domain.Reference
	ExpectedVersion int64
}

// RestoreTicket clears a ticket's soft-delete, refusing when the
// ticket's feature is itself deleted (ADR 0001 forbids a nullable
// feature_id, so a restored ticket would otherwise point at a deleted
// feature with no escape hatch — restore the feature first). Cascade-
// deleted tickets are not auto-restored when their feature is
// restored; each is restored individually once its parent is live
// again, which this same check then allows.
func (s *Service) RestoreTicket(ctx context.Context, req RestoreTicketRequest, actor domain.ActorRef, correlationID string) (domain.Ticket, error) {
	var result domain.Ticket
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetTicketByRefAnyDeletion(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}
		if row.Entity.DeletedAt == nil {
			return newValidationError("ref", "ticket is not deleted")
		}

		featureRef, err := domain.Parse(row.Entity.FeatureRef)
		if err != nil {
			return fmt.Errorf("service: parse ticket's feature ref: %w", err)
		}
		feature, err := store.GetFeatureByRefAnyDeletion(ctx, tx, featureRef)
		if err != nil {
			return fmt.Errorf("service: look up ticket's feature: %w", err)
		}
		if feature.Entity.DeletedAt != nil {
			return newValidationError("ref", "cannot restore ticket %s: its feature %s is deleted; restore the feature first", row.Entity.Ref, feature.Entity.Ref)
		}

		if _, err := store.RestoreEntity(ctx, tx, row.ID, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: restore ticket: %w", err)
		}

		changes := auditChanges(map[string]any{"ref": row.Entity.Ref})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventTicketRestored, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload restored ticket: %w", err)
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return result, nil
}
