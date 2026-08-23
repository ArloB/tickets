package main

import "github.com/ArloB/tickets/internal/domain"

// exitCode maps a domain.ErrorCode (docs/contracts/errors.md) to a
// stable CLI exit code (docs/contracts/cli.md), so a script can branch
// on $? instead of parsing stderr text. 2 stays reserved for a usage
// error (main.go's existing convention: an unknown command or a flag
// parse failure); 1 is every error this table doesn't specifically
// call out, including internal_error itself — there's nothing a
// script can usefully do differently for "the server had an internal
// error" versus any other uncategorized failure.
func exitCode(code domain.ErrorCode) int {
	switch code {
	case domain.ErrValidationFailed:
		return 10
	case domain.ErrNotFound:
		return 11
	case domain.ErrAlreadyExists:
		return 12
	case domain.ErrVersionConflict:
		return 13
	case domain.ErrIdempotencyKeyReused:
		return 14
	case domain.ErrUnauthorized:
		return 15
	case domain.ErrForbidden:
		return 16
	case domain.ErrRelationshipCycle:
		return 17
	case domain.ErrHasDependents:
		return 18
	case domain.ErrThrottled:
		return 19
	default:
		return 1
	}
}
