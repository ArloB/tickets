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

**Phase 1 addendum — not every mutation fits "exactly 1 per
mutation" the way it reads above.** Four exceptions, each a
deliberate decision made while building Step 4b, not an oversight:

- A comment's `version` (its own column, `comments.version`) is
  independent of its parent entity's `entities.version`. Adding or
  editing a comment never bumps the ticket/feature it's attached to;
  only the comment's own version changes. `current_version` on a
  `version_conflict` from the comment-edit/delete path is therefore a
  *comment* version, not the parent entity's — harmless while
  comments have no HTTP endpoint, worth remembering once they do.
- `ticket_relationships` and `entity_associations` rows have no
  version column at all and are not version-guarded. A duplicate
  add is `409 already_exists`, which is its own idempotency
  mechanism for an edge that either exists or doesn't.
- Reordering a record bumps `entities.version` only for the record
  the caller explicitly moved. When the gap between neighbors is
  exhausted and the whole priority group is renumbered, every other
  member's `position` is rewritten with **no** version bump and no
  audit event — otherwise one drag-and-drop could invalidate every
  other open `If-Match` token in the group. See ADR 0011.
- Soft-delete and restore both bump `entities.version` like any other
  mutation (deleting or restoring is itself a change a stale `If-Match`
  should catch) — except a feature-cascade's dependent tickets, which
  are soft-deleted without a caller-supplied `ExpectedVersion` (the
  caller has no way to know each dependent's version in advance) and
  still get their version bumped, unconditionally. See ADR 0013.

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
  **Phase 1 note:** `actor_id` is still not part of the fingerprint,
  but the reason changed — Phase 1 gave every mutation a real,
  resolved actor (ADR 0012), so the original "there are no
  authenticated actors yet" reasoning no longer holds. What's actually
  blocking it now: `idempotency_keys.key` is the table's sole PRIMARY
  KEY, so two actors legitimately reusing the same client-chosen key
  would collide at the schema level regardless of what the fingerprint
  hashes — adding `actor_id` to the hash without also widening the key
  to `(key, actor_id)` wouldn't fix anything. That schema change (and
  the fingerprint update alongside it) is deferred to Phase 2's real
  authentication work, which is the same phase that first makes
  "two different actors reusing the same key" a scenario worth
  distinguishing — see the Step 4a commit that made this call.
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
