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

	// ErrThrottled is Phase 2's addition (429): too many failed login
	// attempts within internal/auth.TooManyAttempts' trailing window.
	ErrThrottled ErrorCode = "throttled"

	// ErrForbidden is Phase 2's other addition (403): the caller is
	// authenticated (or anonymous with reads enabled) but lacks the
	// permission a mutating route requires — reserved separately from
	// ErrUnauthorized (401), which means "no valid credentials at all."
	// Decided in internal/httpapi's auth middleware, not internal/
	// service, since §4.2's flat viewer/editor/admin levels are a
	// property of the request, not of any entity a service method
	// would inspect.
	ErrForbidden ErrorCode = "forbidden"
)

func (c ErrorCode) Valid() bool {
	switch c {
	case ErrValidationFailed, ErrNotFound, ErrAlreadyExists, ErrVersionConflict,
		ErrIdempotencyKeyReused, ErrUnauthorized, ErrInternal,
		ErrRelationshipCycle, ErrHasDependents, ErrThrottled, ErrForbidden:
		return true
	}
	return false
}
