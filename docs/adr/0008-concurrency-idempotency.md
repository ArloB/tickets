# 0008: Optimistic concurrency (version + If-Match) and idempotency keys

## Context

Product spec §8.4 requires mutable records to carry an integer
`version`, update APIs to require the last-read version (via body or
`If-Match`) and return `409` with the current version when stale, and
every mutation to accept an idempotency key so a client can safely
retry after losing a response.

## Decision

- Every entity table row (or its `entities` registry row, per ADR
  0002) carries `version INTEGER NOT NULL DEFAULT 1`, incremented by
  exactly 1 on every successful mutation, inside the same transaction
  as the mutation itself.
- Update endpoints require `If-Match: "<version>"`. A mismatch returns
  `409` with an envelope carrying `current_version` (product spec §9's
  error envelope), so the client can decide whether to retry.
- `idempotency_keys` records `(key, actor, request fingerprint, result)`
  with a bounded retention window. A mutation replayed with the same
  key and matching fingerprint returns the original result without
  re-executing; a key reused with a different fingerprint is a client
  error, not a silent overwrite.
- Reads may retry automatically; writes retry only when an
  `Idempotency-Key` header is present (§8.4).

## Consequences

- `internal/service` owns both mechanisms — `internal/httpapi` and
  `internal/mcpsrv` only translate the `If-Match`/`Idempotency-Key`
  headers or tool-call fields into service-layer calls, per ADR 0005.
- Phase 0's vertical slice (Step 5) proves this end-to-end on the
  ticket-create and ticket-status-update endpoints specifically because
  it is the only way to *demonstrate* the contract, not just assert it.
- The idempotency fingerprint must include enough of the request body
  that two different creates sharing an accidental key collision are
  still distinguishable as a client error rather than silently
  returning the wrong prior result.
