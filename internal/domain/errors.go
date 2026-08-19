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
)

func (c ErrorCode) Valid() bool {
	switch c {
	case ErrValidationFailed, ErrNotFound, ErrAlreadyExists, ErrVersionConflict,
		ErrIdempotencyKeyReused, ErrUnauthorized, ErrInternal:
		return true
	}
	return false
}
