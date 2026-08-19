# Concurrency contract: version + If-Match, idempotency keys

Backed by `internal/service` (Step 5) — the enforcement logic is
inherently tied to the store transaction, so unlike references/enums
this contract's code lives in `internal/service`, not
`internal/domain`. This document is what Step 5's contract tests
verify against.

## Optimistic version

- Every mutable entity row carries `version INTEGER NOT NULL DEFAULT 1`
  (ADR 0002/0008), incremented by exactly 1 per successful mutation,
  in the same transaction as the mutation.
- Detail responses always include `version`.
- Update requests **must** send `If-Match: "<version>"` — a
  double-quoted decimal integer, following ETag header syntax loosely
  (this project does not implement full RFC 7232: no weak validators,
  no `*`, no multi-value lists — just one quoted integer).
- A mismatch returns `409 version_conflict` with `current_version` set
  to the row's actual current value (see `errors.md`). Neither the
  server's nor the client's version of the data is silently discarded
  — the client decides whether to retry.
- A missing `If-Match` on an update request is itself a
  `400 validation_failed`, not an unconditional overwrite.

## Idempotency keys

- Any mutating request may carry `Idempotency-Key: <opaque client string>`.
- The server stores `(key, request_fingerprint, ref_key, created_at)`,
  where `ref_key` is the created record's stable reference (a project
  key or ticket ref) — never a snapshot of the response. A cache hit
  re-fetches the live record by that reference, so fields that don't
  round-trip through JSON (e.g. the internal UUID) and any field a
  later phase adds both survive a replay correctly, instead of
  reverting to whatever a stale snapshot happened to contain.
- **Phase 0 status:** retention is unbounded — nothing purges old
  `idempotency_keys` rows yet. ADR 0008 calls for a bounded retention
  window; that's an administrative maintenance concern (product spec
  §13's "token revocation and similar commands" pattern) implemented
  alongside Phase 2's admin operations, not before.
- `request_fingerprint` = SHA-256 over `method || "\n" || path || "\n"
  || canonical_json_body`, where "canonical" means: parse the request
  body as JSON, then re-marshal with map keys sorted — so two clients
  sending semantically identical JSON with different key order produce
  the same fingerprint, not a spurious `idempotency_key_reused`.
  Replaying the same key with a matching fingerprint returns the
  original stored result without re-executing the mutation. Replaying
  the same key with a *different* fingerprint is
  `409 idempotency_key_reused` — a client bug, not a silent overwrite
  of unrelated data.
  **Phase 0 note:** `actor_id` is not yet part of the fingerprint —
  there are no authenticated actors until ADR 0004 lands in Phase 2.
  It joins the hash then; until it does, two different unauthenticated
  callers reusing the same key are (correctly, for a single-user
  personal install) treated as the same logical request.
- Reads never require an idempotency key and may be retried freely by
  the client (§8.4); only writes consult this mechanism.

## What Step 5 proves end-to-end

Per the Phase 0 plan's verification section:

1. A stale `If-Match` on `PATCH /tickets/{ref}` returns `409` carrying
   `current_version`.
2. Replaying `POST /projects/{key}/tickets` with the same
   `Idempotency-Key` and identical body returns the same ticket,
   not a duplicate.

Everything else in this document (fingerprint hashing, retention
window sizing, `idempotency_key_reused` on a mismatched replay) is
exercised by unit/integration tests in `internal/service`, not
necessarily by the hand-run curl sequence in the Phase 0 plan's
verification section.
