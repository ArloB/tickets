package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// parseIfMatch implements docs/contracts/concurrency.md: If-Match must
// be a double-quoted decimal integer ("3"), following ETag syntax
// loosely. A missing header or an unquoted bare integer are both
// validation_failed, not treated as "no version to check."
func parseIfMatch(r *http.Request) (int64, *service.Error) {
	raw := r.Header.Get("If-Match")
	if raw == "" {
		return 0, &service.Error{Code: domain.ErrValidationFailed, Field: "If-Match", Message: "If-Match header is required for this update"}
	}
	if !strings.HasPrefix(raw, `"`) || !strings.HasSuffix(raw, `"`) || len(raw) < 2 {
		return 0, &service.Error{Code: domain.ErrValidationFailed, Field: "If-Match", Message: `If-Match must be a quoted version, e.g. "3"`}
	}
	unquoted := raw[1 : len(raw)-1]
	v, err := strconv.ParseInt(unquoted, 10, 64)
	if err != nil {
		return 0, &service.Error{Code: domain.ErrValidationFailed, Field: "If-Match", Message: `If-Match must be a quoted decimal integer, e.g. "3"`}
	}
	return v, nil
}

// idempotencyKey reads the Idempotency-Key header (docs/contracts/
// concurrency.md). An empty return means the client didn't request
// idempotent-retry semantics for this call.
func idempotencyKey(r *http.Request) string {
	return r.Header.Get("Idempotency-Key")
}
