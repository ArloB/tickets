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
- `idempotency_keys` records `(key, actor_id, request fingerprint,
  ref_key)` — `ref_key` being the created record's stable reference,
  not a snapshot of the response (see implementation note below) —
  with a bounded retention window. A mutation replayed with the same
  key, same actor, and matching fingerprint re-fetches and returns the
  current live record without re-executing; a key reused with a
  different fingerprint is a client error, not a silent overwrite.
  **Phase 2 (Step 2/migration `0004_identity_and_auth.sql`) widened
  the primary key from `(key)` to `(key, actor_id)`** and threaded
  `actorID` through `internal/service/idempotency.go`'s
  `checkIdempotency`/`recordIdempotency` — the gap this bullet
  originally flagged (adding `actor_id` to the hash without widening
  the key wouldn't have distinguished two actors reusing the same key)
  is closed. See `docs/contracts/concurrency.md`.
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

## Implementation note (Step 5)

Caching a serialized snapshot of the response (the first design tried)
turned out to be lossy: `domain.Ticket.UUID` is tagged `json:"-"` (ADR
0002 — the wire shape never exposes it), so a snapshot round-tripped
through JSON silently zeroed it on replay, and any field a later phase
adds would have the same problem until someone remembered to update
this cache too. `internal/service` instead caches only the created
record's reference and re-fetches the live row on every replay — see
`docs/contracts/concurrency.md` for the current, accurate shape. Gate:
`internal/service/service_test.go`'s
`TestIdempotentReplayReturnsFullRecordNotASnapshot`. Retention was
unbounded through Phase 0/1; Phase 2 closes that with
`tickets admin purge-idempotency-keys` (`cmd/tickets/admin.go`,
default `--older-than 720h`), wrapping the already-existing
`store.PurgeIdempotencyKeysOlderThan` — an operator-run maintenance
command, not automatic expiry, matching product spec §13's "token
revocation and similar commands" pattern.
