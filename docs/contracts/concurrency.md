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
  *comment* version, not the parent entity's — this matters now that
  comments have a real HTTP endpoint (`PATCH /comments/{id}`,
  `DELETE /comments/{id}`, Phase 2 Step 11): a client tracking a
  ticket's `If-Match` and a comment's `If-Match` must keep them as two
  separate values, never assume one covers the other.
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

**Phase 4 addendum — a fifth exception.** External links
(`external_links`, product spec §5.11's "named external links" half —
see `internal/service/link.go`) have no version column and are not
version-guarded, the same shape as `ticket_relationships`/
`entity_associations` above: add and delete only, no in-place edit. A
link's title/URL cannot be changed once created — delete and re-add to
change either. This was a deliberate choice over giving links their
own independent version column (the way comments have): a link is
meant to be a lightweight annotation, not a first-class versioned
record like a decision, and a third independent version-token type
would directly complicate a client's conflict-handling logic, which
already has to keep a ticket's version and a comment's version as two
separate values per the exception above.

**Phase 5 addendum — attachments version themselves independently,
like comments.** An attachment's `current_version`
(`attachments.current_version`) is its own conditional-update token,
independent of its owning entity's `entities.version` — uploading a
new version of a file attached to a ticket never bumps the ticket's
own version. `PUT /attachments/{id}` (replace) and
`DELETE /attachments/{id}` both require `If-Match: "<current_version>"`
against that column, and a `version_conflict` from either carries the
attachment's `current_version`, not the parent entity's — a third
independent version-token type, alongside a ticket/feature/decision/
content-item's `entities.version` and a comment's `comments.version`.
Unlike decisions/content_items (which snapshot the *pre-update* row
into `_versions` before overwriting it), `attachment_versions` holds
every version including the current one — version 1 is archived
immediately at creation, not deferred until a first edit — since a
binary/path attachment version has nothing worth line-diffing against;
the version list itself, not a diff, is the history (§5.11).

- Any mutating request may carry `Idempotency-Key: <opaque client string>`.
- The server stores `(key, request_fingerprint, ref_key, created_at)`,
  where `ref_key` is whatever string the created record's own service
  method uses to re-fetch it — never a snapshot of the response. For a
  project or ticket that's the public reference (a project key or
  ticket ref); a comment has no public reference of its own, so
  `AddComment` (Phase 3) stores its integer id as a decimal string
  instead. Either way, a cache hit re-fetches the live record by that
  value, so fields that don't round-trip through JSON (e.g. the
  internal UUID) and any field a later phase adds both survive a
  replay correctly, instead of reverting to whatever a stale snapshot
  happened to contain.
- Retention was unbounded through Phase 0/1. Phase 2 adds
  `tickets admin purge-idempotency-keys` (`cmd/tickets/admin.go`,
  default `--older-than 720h`) — an operator-run maintenance command
  (product spec §13's "token revocation and similar commands"
  pattern), not automatic expiry. Nothing purges keys on its own; an
  operator (or a cron job wrapping this command) has to run it.
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
  **Phase 2 status: `actor_id` scoping is implemented, not part of the
  fingerprint hash itself.** Migration `0004_identity_and_auth.sql`
  widened `idempotency_keys`' primary key from `(key)` to
  `(key, actor_id)` — existing Phase 0/1 rows all predated real actor
  attribution and were dropped outright rather than backfilled (the
  table is a bounded-retention cache by ADR 0008's own design, nothing
  in it was worth preserving across the schema change).
  `internal/service/idempotency.go`'s `checkIdempotency`/
  `recordIdempotency` now take an `actorID` parameter and look up
  `WHERE key = ? AND actor_id = ?`, so two different actors reusing
  the same client-chosen key get two independent records rather than
  colliding. `Fingerprint(method, path, body)` itself is deliberately
  unchanged — actor identity is the *lookup key*, not an input to the
  content hash; a request's fingerprint should only ever reflect what
  the request actually says, not who sent it.
- Reads never require an idempotency key and may be retried freely by
  the client (§8.4); only writes consult this mechanism.
- **Phase 3 status: the fingerprint scheme differs by transport, and
  that's a real caveat, not just an implementation detail.** The HTTP
  API (and everything behind it — `apiclient`, the CLI, and MCP's
  `HTTPBackend`) computes `Fingerprint(method, path, body)` from the
  actual HTTP request, as described above. MCP's `InProcessBackend`
  (the HTTP-mounted MCP endpoint, which calls `internal/service`
  directly and never constructs an HTTP request) instead computes
  `Fingerprint(toolName, "", json(input))` — see `mcpFingerprint` in
  `internal/mcpsrv/inprocess.go`. The two schemes are deliberately
  incompatible: the same logical call (e.g. creating the same decision
  with the same key) produces a *different* fingerprint depending on
  which backend served it. This is harmless in practice today because
  the fingerprint lookup is additionally scoped by `actor_id`, and the
  two backends are only ever reachable through different transports
  with their own token/session — a single agent identity doesn't call
  both for the same logical retry. But if a future caller ever expects
  a key replayed against `InProcessBackend` to match one recorded via
  the HTTP API (or vice versa), it won't — that would need a shared
  canonical fingerprint input, not two independent schemes.

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
