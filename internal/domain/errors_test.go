package domain

import "testing"

func TestErrorCodeValid(t *testing.T) {
	valid := []ErrorCode{
		ErrValidationFailed, ErrNotFound, ErrAlreadyExists, ErrVersionConflict,
		ErrIdempotencyKeyReused, ErrUnauthorized, ErrInternal,
		ErrRelationshipCycle, ErrHasDependents, ErrThrottled, ErrForbidden,
	}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("%s.Valid() = false, want true", c)
		}
	}
	if ErrorCode("bogus").Valid() {
		t.Error(`ErrorCode("bogus").Valid() = true, want false`)
	}
}
