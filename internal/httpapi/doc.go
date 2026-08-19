// Package httpapi implements the versioned /api/v1 HTTP handlers:
// request/response mapping onto internal/service, the error envelope,
// cursor pagination, sparse-field/expansion handling, idempotency-key
// and If-Match handling, and the liveness/readiness endpoints. See
// api/openapi.yaml for the checked-in contract this package serves.
package httpapi
