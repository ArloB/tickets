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
- `idempotency_keys` records `(key, request fingerprint, ref_key)` —
  `ref_key` being the created record's stable reference, not a
  snapshot of the response (see implementation note below) — with a
  bounded retention window. A mutation replayed with the same key and
  matching fingerprint re-fetches and returns the current live record
  without re-executing; a key reused with a different fingerprint is a
  client error, not a silent overwrite. `actor` joins the fingerprint
  once ADR 0004's actors exist (Phase 2); Phase 0 has none.
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
`TestIdempotentReplayReturnsFullRecordNotASnapshot`. Retention remains
unbounded in Phase 0 (docs/contracts/concurrency.md's Phase 0 status
note) — the bounded window above is not yet implemented.
