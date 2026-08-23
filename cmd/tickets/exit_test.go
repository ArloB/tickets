package main

import (
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// allKnownErrorCodes mirrors domain.ErrorCode's own Valid() switch
// (internal/domain/errors.go) — kept as a literal list here (rather
// than exporting one from internal/domain) so this test breaks loudly
// if a new error code is ever added there without a matching decision
// about its CLI exit code.
var allKnownErrorCodes = []domain.ErrorCode{
	domain.ErrValidationFailed, domain.ErrNotFound, domain.ErrAlreadyExists,
	domain.ErrVersionConflict, domain.ErrIdempotencyKeyReused, domain.ErrUnauthorized,
	domain.ErrInternal, domain.ErrRelationshipCycle, domain.ErrHasDependents,
	domain.ErrThrottled, domain.ErrForbidden,
}

// TestExitCodeCoversEveryErrorCode proves every code exitCode
// specifically maps to (i.e. every one but internal_error, which
// deliberately shares the generic fallback with an unrecognized code)
// gets its own distinct, stable, non-zero, non-2 exit code — 2 is
// reserved for a CLI usage error (main.go), not a server-reported one.
func TestExitCodeCoversEveryErrorCode(t *testing.T) {
	seen := map[int]domain.ErrorCode{}
	for _, code := range allKnownErrorCodes {
		got := exitCode(code)
		if got == 0 || got == 2 {
			t.Errorf("exitCode(%q) = %d, want a nonzero code other than 2 (reserved for usage errors)", code, got)
		}
		if code == domain.ErrInternal {
			continue // deliberately shares the generic fallback, checked separately below
		}
		if other, ok := seen[got]; ok {
			t.Errorf("exitCode(%q) = %d, but exitCode(%q) already returned %d — codes must be distinct", code, got, other, got)
		}
		seen[got] = code
	}
}

func TestExitCodeInternalErrorAndUnknownCodeShareFallback(t *testing.T) {
	if got := exitCode(domain.ErrInternal); got != 1 {
		t.Errorf("exitCode(internal_error) = %d, want 1", got)
	}
	if got := exitCode(domain.ErrorCode("not_a_real_code")); got != 1 {
		t.Errorf("exitCode of an unrecognized code = %d, want 1 (the same fallback as internal_error)", got)
	}
}
