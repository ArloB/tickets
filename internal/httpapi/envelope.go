package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/google/uuid"
)

// logger is this package's own operational logger (encode failures,
// unexpected/internal errors) - distinct from anything the service
// layer logs. It defaults to slog.Default() so tests and any caller
// that never touches SetLogger still get usable output; cmd/tickets
// installs the real process logger (internal/logging.New's result) at
// startup.
var logger = slog.Default()

// SetLogger installs the logger internal/httpapi uses for its own
// operational logging.
func SetLogger(l *slog.Logger) {
	logger = l
}

// errorEnvelope is the wire shape from docs/contracts/errors.md.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Field          string `json:"field,omitempty"`
	CorrelationID  string `json:"correlation_id"`
	CurrentVersion *int64 `json:"current_version,omitempty"`
}

func statusForCode(code domain.ErrorCode) int {
	switch code {
	case domain.ErrValidationFailed:
		return http.StatusBadRequest
	case domain.ErrNotFound:
		return http.StatusNotFound
	case domain.ErrAlreadyExists, domain.ErrVersionConflict, domain.ErrIdempotencyKeyReused, domain.ErrHasDependents:
		return http.StatusConflict
	case domain.ErrUnauthorized:
		return http.StatusUnauthorized
	case domain.ErrRelationshipCycle:
		return http.StatusBadRequest
	case domain.ErrThrottled:
		return http.StatusTooManyRequests
	case domain.ErrForbidden:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

// correlationID returns the client-supplied X-Correlation-Id (product
// spec §9) or generates one.
func correlationID(r *http.Request) string {
	if v := r.Header.Get("X-Correlation-Id"); v != "" {
		return v
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "unknown"
	}
	return id.String()
}

// requestActor is every mutating handler's actor for internal/service
// calls (ADR 0012). It reads whatever auth.WithPrincipal attached to
// the request context — the authenticate middleware (auth_middleware.go)
// resolves a real Principal from a session cookie or bearer token
// before any handler runs, so this is a context read that cannot fail,
// not a lookup. Every handler call site written in Phase 0/1 is
// unchanged by this: the actor only ever flowed through as this one
// function's return value.
func requestActor(r *http.Request) domain.ActorRef {
	return auth.FromContext(r.Context()).Actor
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("httpapi: encode response", "error", err)
	}
}

// writeError maps a *service.Error to the HTTP error envelope. Any
// other error is never shown to the client — it becomes a generic
// internal_error, logged server-side with the correlation ID so it can
// be found (docs/contracts/errors.md: message never leaks internals).
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	corrID := correlationID(r)

	var svcErr *service.Error
	if errors.As(err, &svcErr) {
		writeJSON(w, statusForCode(svcErr.Code), errorEnvelope{Error: errorBody{
			Code:           string(svcErr.Code),
			Message:        svcErr.Message,
			Field:          svcErr.Field,
			CorrelationID:  corrID,
			CurrentVersion: svcErr.CurrentVersion,
		}})
		return
	}

	logger.Error("httpapi: unexpected error", "correlation_id", corrID, "error", err)
	writeJSON(w, http.StatusInternalServerError, errorEnvelope{Error: errorBody{
		Code:          string(domain.ErrInternal),
		Message:       "internal server error",
		CorrelationID: corrID,
	}})
}

// decodeJSON reads and decodes a request body, returning a
// *service.Error (validation_failed) on malformed JSON rather than a
// raw decode error.
func decodeJSON(body []byte, v any) *service.Error {
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return &service.Error{Code: domain.ErrValidationFailed, Message: "request body is not valid JSON: " + err.Error()}
	}
	return nil
}
