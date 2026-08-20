# Error envelope

Backed by `internal/httpapi`'s error-response type (Step 5) — the
envelope is an HTTP wire concern, not a pure domain one, so unlike
references/enums it is not `internal/domain` code. `internal/domain`
still defines the machine-readable **codes** below as a typed enum,
since MCP tool errors (ADR 0006) reuse the same codes without going
through HTTP at all.

## Shape

```json
{
  "error": {
    "code": "version_conflict",
    "message": "The ticket was modified by another actor since you last read it.",
    "field": null,
    "correlation_id": "01996a3e-...",
    "current_version": 4
  }
}
```

- `code` — stable, machine-readable, snake_case (catalogue below).
  Clients branch on this, never on `message`.
- `message` — human-readable, safe to display; never contains a
  secret, token, or password (§10, §5.12).
- `field` — present for validation errors naming a single offending
  field; `null` otherwise.
- `correlation_id` — echoes the request's correlation ID (§9's
  client-generated tracing ID) or a server-generated one if the client
  didn't supply one.
- `current_version` — present only for `version_conflict` (§8.4); lets
  a client retry with a fresh `If-Match` without a second round trip.

An earlier draft of this doc also specified a `retry_after` field for
a rate-limiting code, speculatively, before either existed.
`throttled` (429, added below) is what was actually built — for login
attempts specifically, not a general per-route rate limit — and its
envelope carries no `retry_after`; a throttled client just retries
after the throttle window (`internal/auth/throttle.go`) elapses, which
this response doesn't currently name explicitly. `internal/httpapi`'s
`errorBody` (`envelope.go`) is the exhaustive field list: `code`,
`message`, `field`, `correlation_id`, `current_version` — nothing
else, on any code.

## Code catalogue (Phase 0)

| Code | HTTP status | Meaning |
| --- | --- | --- |
| `validation_failed` | 400 | Request body/params failed validation; see `field`. |
| `not_found` | 404 | No entity resolves to the given reference/ID. |
| `already_exists` | 409 | A unique field (e.g. a project's `key`) collides with an existing record; see `field`. |
| `version_conflict` | 409 | `If-Match` didn't match the current `version` (ADR 0008). |
| `idempotency_key_reused` | 409 | Same `Idempotency-Key` replayed with a different request fingerprint. |
| `unauthorized` | 401 | Missing/invalid credentials (bearer token or session). |
| `internal_error` | 500 | Unexpected server-side failure; never leaks internals in `message`. |

Additional codes (`forbidden`, `throttled`, and others tied to
features outside Step 5's slice) are added alongside the feature that
needs them — this table only covers what the vertical slice's three
mutating endpoints can actually return. (`throttled` was called
`rate_limited` in this doc's earlier drafts; see the Phase 2 additions
table below for the name actually built.)

## Code catalogue (Phase 1 additions)

| Code | HTTP status | Meaning |
| --- | --- | --- |
| `relationship_cycle` | 400 | Adding this `blocks`/`parent_of` edge would create a cycle. |
| `has_dependents` | 409 | Soft-deleting this record would orphan non-deleted dependents; retry with `cascade: true` to delete them together. |

Both are reachable over HTTP as of Phase 2: `relationship_cycle` from
`POST /tickets/{ref}/relationships` (a `blocks`/`parent_of` edge that
would close a cycle, ADR 0014), `has_dependents` from
`DELETE /features/{ref}` (a feature with non-deleted tickets, retry
with `?cascade=true`, ADR 0013). `internal/httpapi`'s `statusForCode`
mapped both correctly from the day they were written, well before
either endpoint existed to exercise it.

## Code catalogue (Phase 2 additions)

| Code | HTTP status | Meaning |
| --- | --- | --- |
| `forbidden` | 403 | Authenticated, but the caller's permission level doesn't allow this request (e.g. an anonymous viewer attempting a write, or a non-admin agent hitting an admin route). Distinct from `unauthorized`, which means no valid credentials were presented at all. |
| `throttled` | 429 | Too many failed login attempts for this username/IP within the throttle window (`internal/auth/throttle.go`). |

`forbidden` is decided in `internal/httpapi`'s auth middleware, not
`internal/service` — see ADR 0004/0005's documented exception for why
permission-level checks live in the translation layer for Phase 2.
`throttled` is decided in `internal/service.Authenticate` (via
`internal/auth.TooManyAttempts`), same as every other error code:
`internal/service` stays the sole authorization/validation boundary
for it.

## OpenAPI's `code` field is a bare string, not an enum

`api/openapi.yaml`'s `ErrorEnvelope` schema declares `error.code` as
`type: string`, with no `enum:` constraint against this catalogue. The
Phase 2 plan's Step 5 originally called for `ErrForbidden` to "touch
… the OpenAPI error-code enum," but no such enum exists in the schema
that shipped — enumerating this table's ~13 codes in the schema would
mean every new code (this doc has grown one per phase so far) needs a
synchronized edit to `api/openapi.yaml` on top of `internal/domain`'s
own `ErrorCode` type, for a constraint `openapi3filter`'s
response-schema validation doesn't actually need to catch a
mismatch — every httpapi test already asserts the exact `code` string
a response carries, which is the real enforcement mechanism. This
doc's table is the enum, in effect; keep it in sync with
`internal/domain/errors.go`'s constants, not with `api/openapi.yaml`.

## Consistency across interfaces

MCP tool errors (ADR 0006) and CLI exit-code mappings (§7.3) reuse
this same `code` catalogue rather than inventing their own, so an
agent or script can branch on one vocabulary regardless of which
interface it used.
