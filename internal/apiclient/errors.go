package apiclient

import "github.com/ArloB/tickets/internal/domain"

// Error is apiclient's decoded form of internal/httpapi's error
// envelope (docs/contracts/errors.md). It deliberately mirrors
// internal/service.Error's shape and fields without importing
// internal/service: apiclient is a client package that talks to the
// Tickets HTTP API from the outside, so it has no business depending
// on the server's internal package. Callers that need a
// *service.Error (internal/mcpsrv's HTTPBackend, which already
// imports internal/service to satisfy the Backend interface) convert
// at their own boundary.
type Error struct {
	Code           domain.ErrorCode
	Message        string
	Field          string // "" if not a single-field validation error
	CurrentVersion *int64 // set only for a version-conflict error
}

func (e *Error) Error() string { return e.Message }
