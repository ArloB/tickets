package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/google/uuid"
)

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
// calls (ADR 0012). Phase 1 has no authentication (ADR 0004 lands in
// Phase 2), so every HTTP request is attributed to the single seeded
// 'local' actor (migration 0002_core_domain.sql) — a placeholder, not
// a design decision about who "local" represents long-term. Phase 2
// replaces this with an actor resolved from the request's session or
// bearer token; no other code in this package needs to change when it
// does, since the actor only ever flows through as this one value.
func requestActor(r *http.Request) domain.ActorRef {
	return domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("httpapi: encode response: %v", err)
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

	log.Printf("httpapi: unexpected error [correlation_id=%s]: %v", corrID, err)
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
