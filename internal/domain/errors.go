package domain

// ErrorCode is the stable, machine-readable vocabulary clients branch
// on. See docs/contracts/errors.md — internal/httpapi maps these to
// HTTP status codes, and MCP tool errors (ADR 0006) reuse the same
// values without going through HTTP at all.
type ErrorCode string

const (
	ErrValidationFailed     ErrorCode = "validation_failed"
	ErrNotFound             ErrorCode = "not_found"
	ErrAlreadyExists        ErrorCode = "already_exists"
	ErrVersionConflict      ErrorCode = "version_conflict"
	ErrIdempotencyKeyReused ErrorCode = "idempotency_key_reused"
	ErrUnauthorized         ErrorCode = "unauthorized"
	ErrInternal             ErrorCode = "internal_error"

	// ErrRelationshipCycle is Phase 1's addition to the errors.md
	// catalogue (400): adding a blocks or parent_of edge would create a
	// cycle (product spec §5.7). Self-links and a relationship whose
	// endpoints aren't both tickets stay ErrValidationFailed with Field
	// set — this code is specifically for the graph-traversal check.
	ErrRelationshipCycle ErrorCode = "relationship_cycle"

	// ErrHasDependents is Phase 1's other addition (409): soft-deleting
	// a feature that still holds non-deleted tickets, without an
	// explicit cascade, is rejected rather than silently orphaning or
	// bulk-deleting them (see ADR 0013).
	ErrHasDependents ErrorCode = "has_dependents"
)

func (c ErrorCode) Valid() bool {
	switch c {
	case ErrValidationFailed, ErrNotFound, ErrAlreadyExists, ErrVersionConflict,
		ErrIdempotencyKeyReused, ErrUnauthorized, ErrInternal,
		ErrRelationshipCycle, ErrHasDependents:
		return true
	}
	return false
}
