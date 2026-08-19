package service

import (
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
)

// Error carries everything docs/contracts/errors.md's envelope needs.
// internal/httpapi type-asserts for *Error to build the HTTP response;
// internal/mcpsrv does the same to build an MCP tool error, reusing the
// same domain.ErrorCode vocabulary (ADR 0006) without going through
// HTTP at all.
type Error struct {
	Code           domain.ErrorCode
	Message        string
	Field          string // "" if not a single-field validation error
	CurrentVersion *int64 // set only for ErrVersionConflict
}

func (e *Error) Error() string { return e.Message }

func newValidationError(field, format string, args ...any) *Error {
	return &Error{Code: domain.ErrValidationFailed, Field: field, Message: fmt.Sprintf(format, args...)}
}

func newNotFoundError(format string, args ...any) *Error {
	return &Error{Code: domain.ErrNotFound, Message: fmt.Sprintf(format, args...)}
}

func newVersionConflictError(current int64) *Error {
	return &Error{
		Code:           domain.ErrVersionConflict,
		Message:        "the record was modified by another actor since you last read it",
		CurrentVersion: &current,
	}
}

func newIdempotencyReusedError() *Error {
	return &Error{
		Code:    domain.ErrIdempotencyKeyReused,
		Message: "this Idempotency-Key was already used with a different request body",
	}
}

func newAlreadyExistsError(field, format string, args ...any) *Error {
	return &Error{Code: domain.ErrAlreadyExists, Field: field, Message: fmt.Sprintf(format, args...)}
}

func newRelationshipCycleError(relType domain.RelationshipType) *Error {
	return &Error{
		Code:    domain.ErrRelationshipCycle,
		Field:   "type",
		Message: fmt.Sprintf("adding this %s relationship would create a cycle", relType),
	}
}

func newHasDependentsError(format string, args ...any) *Error {
	return &Error{Code: domain.ErrHasDependents, Message: fmt.Sprintf(format, args...)}
}
