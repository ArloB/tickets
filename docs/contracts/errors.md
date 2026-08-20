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
    "current_version": 4,
    "retry_after": null
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
- `retry_after` — present only for `rate_limited`; seconds.

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

Additional codes (`forbidden`, `rate_limited`, and others tied to
features outside Step 5's slice) are added alongside the feature that
needs them — this table only covers what the vertical slice's three
mutating endpoints can actually return.

## Code catalogue (Phase 1 additions)

| Code | HTTP status | Meaning |
| --- | --- | --- |
| `relationship_cycle` | 400 | Adding this `blocks`/`parent_of` edge would create a cycle. |
| `has_dependents` | 409 | Soft-deleting this record would orphan non-deleted dependents; retry with `cascade: true` to delete them together. |

No endpoint returns either code yet — Phase 1 stays below the API
line (service-level tests only) — but `internal/httpapi`'s
`statusForCode` maps both so the mapping isn't invented under
schedule pressure once Phase 2 exposes them.

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

## Consistency across interfaces

MCP tool errors (ADR 0006) and CLI exit-code mappings (§7.3) reuse
this same `code` catalogue rather than inventing their own, so an
agent or script can branch on one vocabulary regardless of which
interface it used.
