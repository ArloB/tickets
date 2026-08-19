package store

import (
	"context"
	"fmt"
)

// UpdateTicketFields applies a conditional update to a ticket's
// type/title/description/priority/severity (ADR 0008's version-guard
// pattern via bumpEntityVersion). severity may be nil (task/chore
// tickets carry none) — the caller has already enforced
// TicketType.AllowsSeverity() before reaching here, the same rule
// CreateTicket applies. now is the caller's shared transaction
// timestamp (see Now).
func UpdateTicketFields(ctx context.Context, q Querier, entityID int64, ticketType, title, description, priority string, severity *string, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE tickets SET type = ?, title = ?, description = ?, priority = ?, severity = ?, priority_rank = ?, severity_rank = ? WHERE id = ?`,
		ticketType, title, description, priority, severity, priorityRank(priority), severityRank(severity), entityID,
	); err != nil {
		return 0, fmt.Errorf("update ticket fields: %w", err)
	}
	return newVersion, nil
}

// AssignTicket sets or clears (assigneeID == nil) a ticket's assignee.
func AssignTicket(ctx context.Context, q Querier, entityID int64, assigneeID *int64, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE tickets SET assignee_id = ? WHERE id = ?`, assigneeID, entityID); err != nil {
		return 0, fmt.Errorf("assign ticket: %w", err)
	}
	return newVersion, nil
}

// MoveTicketFeature moves a ticket to a different feature. The caller
// (internal/service) validates the new feature belongs to the same
// project as the ticket (ADR 0001 forbids a cross-project move) —
// this function only applies the write.
func MoveTicketFeature(ctx context.Context, q Querier, entityID, newFeatureEntityID int64, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE tickets SET feature_id = ? WHERE id = ?`, newFeatureEntityID, entityID); err != nil {
		return 0, fmt.Errorf("move ticket feature: %w", err)
	}
	return newVersion, nil
}
