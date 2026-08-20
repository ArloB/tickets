package httpapi

import (
	"net/http"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestStatusForCodeCoversEveryKnownErrorCode guards against exactly
// the failure mode that let domain.ErrThrottled silently fall through
// to a generic 500 for a while: statusForCode's switch lives in a
// different file from wherever a new domain.ErrorCode gets introduced,
// so nothing else forces the two to stay in sync. Every code except
// domain.ErrInternal itself must map to something other than the
// switch's 500 default.
func TestStatusForCodeCoversEveryKnownErrorCode(t *testing.T) {
	codes := []domain.ErrorCode{
		domain.ErrValidationFailed,
		domain.ErrNotFound,
		domain.ErrAlreadyExists,
		domain.ErrVersionConflict,
		domain.ErrIdempotencyKeyReused,
		domain.ErrUnauthorized,
		domain.ErrRelationshipCycle,
		domain.ErrHasDependents,
		domain.ErrThrottled,
		domain.ErrForbidden,
	}
	for _, code := range codes {
		if got := statusForCode(code); got == http.StatusInternalServerError {
			t.Errorf("statusForCode(%q) = 500 (the default case) — this code needs its own mapping", code)
		}
	}
}

func TestStatusForCodeSpecificMappings(t *testing.T) {
	cases := []struct {
		code domain.ErrorCode
		want int
	}{
		{domain.ErrValidationFailed, http.StatusBadRequest},
		{domain.ErrNotFound, http.StatusNotFound},
		{domain.ErrAlreadyExists, http.StatusConflict},
		{domain.ErrVersionConflict, http.StatusConflict},
		{domain.ErrIdempotencyKeyReused, http.StatusConflict},
		{domain.ErrUnauthorized, http.StatusUnauthorized},
		{domain.ErrRelationshipCycle, http.StatusBadRequest},
		{domain.ErrHasDependents, http.StatusConflict},
		{domain.ErrThrottled, http.StatusTooManyRequests},
		{domain.ErrForbidden, http.StatusForbidden},
		{domain.ErrInternal, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := statusForCode(tc.code); got != tc.want {
			t.Errorf("statusForCode(%q) = %d, want %d", tc.code, got, tc.want)
		}
	}
}
