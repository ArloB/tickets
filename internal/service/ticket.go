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
// project's General feature, in one transaction.
//
// Like CreateProject, the idempotency cache stores only the created
// ticket's ref, never a snapshot of the response — see idempotency.go.
func (s *Service) CreateTicket(ctx context.Context, req CreateTicketRequest, idemKey, fingerprint string) (domain.Ticket, error) {
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

	ref, err := s.createTicketTx(ctx, req, title, priority, idemKey, fingerprint)
	if err != nil {
		return domain.Ticket{}, err
	}
	return s.GetTicket(ctx, ref)
}

func (s *Service) createTicketTx(ctx context.Context, req CreateTicketRequest, title string, priority domain.Priority, idemKey, fingerprint string) (domain.Reference, error) {
	var result domain.Reference
	err := s.withTx(ctx, func(tx *sql.Tx, now string) error {
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

		ticketEntityID, _, err := store.InsertEntity(ctx, tx, &proj.ID, domain.KindTicket, now)
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
		if err := store.InsertTicket(ctx, tx, ticketEntityID, proj.ID, proj.GeneralFeatureID, seq,
			string(req.Type), title, req.Description, string(domain.WorkflowStatusBacklog), string(priority), severityStr); err != nil {
			return fmt.Errorf("service: create ticket: %w", err)
		}

		ref := domain.Reference{ProjectKey: req.ProjectKey, Kind: domain.KindTicket, Seq: seq}
		refStr, err := domain.Format(ref)
		if err != nil {
			return fmt.Errorf("service: format created ticket ref: %w", err)
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
func (s *Service) UpdateTicketStatus(ctx context.Context, req UpdateTicketStatusRequest) (domain.Ticket, error) {
	if !req.NewStatus.Valid() {
		return domain.Ticket{}, newValidationError("status", "invalid status %q", req.NewStatus)
	}

	var result domain.Ticket
	err := s.withTx(ctx, func(tx *sql.Tx, now string) error {
		row, err := store.GetTicketByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("ticket not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up ticket: %w", err)
		}

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
