// Package service implements application services: the single
// authorization, validation, transaction, audit, and idempotency
// boundary shared by internal/httpapi and internal/mcpsrv. Neither
// caller duplicates this logic (product spec §7.1, §8.4).
package service
