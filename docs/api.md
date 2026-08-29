# REST API guide

Prose walkthrough of `/api/v1`. [`api/openapi.yaml`](../api/openapi.yaml)
is the actual contract (request/response schemas, every parameter) —
this document orients you in it. `docs/contracts/` holds the specific
wire rules referenced throughout (reference format, enums, error
envelope, compact/detail shapes, concurrency).

## Base URL and authentication

All routes are under `/api/v1` on a running `tickets server`. Two
credential types (ADR 0004):

- **Session cookie** — for humans. `POST /auth/login` with
  `{"username", "password"}` sets an `HttpOnly` session cookie and
  returns a CSRF token. Every mutating request (`POST`/`PATCH`/`PUT`/
  `DELETE`) must echo that token in the `X-CSRF-Token` header — the
  cookie alone is never enough to mutate state, so a cookie stolen via
  XSS still can't write anything. `GET /auth/me` returns the caller's
  resolved principal; `POST /auth/logout` ends the session.
- **Bearer token** — for agents. `Authorization: Bearer <token>`,
  issued via `tickets admin token create` (see
  [`docs/admin.md`](admin.md)). No CSRF token needed — bearer auth
  isn't cookie-based, so it isn't vulnerable to the same cross-site
  request pattern.
- **Anonymous** — if the server has `anonymous_read` enabled (product
  spec §4.2), every `GET` that's normally Viewer-accessible works with
  no credentials at all. Every mutating route still requires at least
  Editor regardless. See [`docs/security-model.md`](security-model.md).

A non-2xx response always uses the error envelope documented in
[`docs/contracts/errors.md`](contracts/errors.md):

```json
{ "error": { "code": "version_conflict", "message": "...", "correlation_id": "..." } }
```

Branch on `error.code`, never on `message` — codes are stable,
messages are for humans.

## Route map

| Area | Routes |
| --- | --- |
| Projects | `POST/GET /projects`, `GET /projects/{key}`, `GET /projects/{key}/brief`, `GET /projects/{key}/tickets`, `GET /projects/{key}/activity` |
| Features | `POST/GET /projects/{key}/features`, `GET/PATCH/DELETE /features/{ref}`, `POST /features/{ref}/status`, `POST /features/{ref}/reorder`, `POST /features/{ref}/restore` |
| Tickets | `POST/GET /projects/{key}/tickets`, `GET/PATCH/PUT/DELETE /tickets/{ref}`, `POST /tickets/{ref}/assign`, `POST /tickets/{ref}/move`, `POST /tickets/{ref}/reorder`, `POST /tickets/{ref}/restore` |
| Comments | `POST/GET /{tickets,features,decisions,plans,documents}/{ref}/comments`, `POST /projects/{key}/comments`, `GET/PATCH/DELETE /comments/{id}`, `GET /comments/{id}/history` |
| Relationships & associations | `POST/GET /tickets/{ref}/relationships`, `DELETE /tickets/{ref}/relationships/{type}/{target}`, `POST/GET /{kind}/{ref}/associations`, `DELETE /{kind}/{ref}/associations/{target}` (tickets/features/decisions/plans/documents) |
| Links & backlinks | `POST/GET /{kind}/{ref}/links`, `DELETE /{kind}/{ref}/links/{id}`, `GET /{kind}/{ref}/backlinks` |
| Attachments | `POST/GET /{kind}/{ref}/attachments`, `GET /comments/{id}/attachments`, `GET/PUT/DELETE /attachments/{id}`, `GET /attachments/{id}/download`, `GET /attachments/{id}/versions`, `GET /attachments/{id}/versions/{version}/download` |
| Decisions | `POST/GET /projects/{key}/decisions`, `GET/PATCH /decisions/{ref}`, `GET /decisions/{ref}/versions`, `GET /decisions/{ref}/diff` |
| Plans & documents | `POST/GET /projects/{key}/{plans,documents}`, `GET/PATCH /{plans,documents}/{ref}`, `GET .../download`, `GET .../versions`, `GET .../versions/{version}/download`, `GET .../diff` |
| Search | `GET /search` |
| Reference resolution | `GET /refs/resolve?refs=ABC-1,ABC-F2` — batch existence check behind rendering references in prose as hyperlinks (ADR 0025) |
| Subscriptions & notifications | `POST/DELETE/GET /{kind}/{ref}/subscribe` (POST subscribes, DELETE unsubscribes, GET reads subscription status), `GET /notifications`, `POST /notifications/read` |
| Live updates | `GET /events` (Server-Sent Events) |
| Setup & identity | `POST /setup`, `POST /auth/login`, `POST /auth/logout`, `GET /auth/me` |
| Agent/token admin | `POST/GET /agents`, `POST/GET /agents/{name}/tokens`, `DELETE /agents/{name}/tokens/{id}` |

`POST /setup` mirrors `tickets setup`'s effect over HTTP, but only
`tickets setup` is the documented, non-interactive path (product spec
§7.3) — see [`docs/install.md`](install.md#first-run-setup).

`{kind}` above means the same route pattern is registered under
`tickets`, `features`, `decisions`, `plans`, and `documents` — all five
are principal entities that can carry comments, associations, links,
and attachments.

## References

Every entity is addressed by a stable, human-readable reference, not
an internal id: a project key (`ABC`), and per-kind tokens built on it
(`ABC-1` ticket, `ABC-F2` feature, `ABC-D3` decision, `ABC-P4` plan,
`ABC-DOC5` document). Full grammar in
[`docs/contracts/references.md`](contracts/references.md). `#ABC-123`
appearing in a Markdown body is scanned automatically and becomes a
link/backlink — no separate call needed.

## Compact vs. detail

List/search endpoints return **compact** records — small enough that a
20-record page stays well within an agent's context budget, no
Markdown bodies or relationship lists. `GET .../{ref}` returns
**detail** — the full record, still not comment content or attachment
bytes (those are their own paginated sub-resources). See
[`docs/contracts/representations.md`](contracts/representations.md)
for the exact field sets.

## Pagination

Most list endpoints default to 20 records, cap at 100, and use an
opaque cursor (`?cursor=...` from the previous response's
`next_cursor`), never an offset. `GET /search` is the one exception —
ADR 0018: it's offset-paginated and capped at 500 total results, since
relevance ranking over a single query doesn't fit the same
`(created_at, id)` cursor every other list endpoint uses. See
[`docs/contracts/list-filters.md`](contracts/list-filters.md) for
per-endpoint filters and sort behavior.

## Concurrency and idempotency

Every mutable record carries a `version`. An update that matters sends
`If-Match: "N"` (a double-quoted decimal version number, not a full
RFC 7232 ETag — see the `IfMatch` parameter in
`api/openapi.yaml`) — a stale version returns `version_conflict`
(HTTP 409, with `error.current_version` telling you the real one)
rather than silently overwriting a concurrent edit. Create endpoints
that support retries accept an `Idempotency-Key` header: replaying the
same key with identical content returns the original result; replaying
it with different content is `idempotency_key_reused`. Full rules in
[`docs/contracts/concurrency.md`](contracts/concurrency.md).

## Uploads

`POST .../attachments` is `multipart/form-data`, capped at
`--max-upload-bytes` (25 MiB by default) per version, enforced before
the body is fully read. Downloads always come back with
`Content-Disposition: attachment` (never `inline`) regardless of the
declared content type — a deliberate control, not an oversight; see
[`docs/security-model.md`](security-model.md#uploads).

## Live updates

`GET /events` is a Server-Sent Events stream of minimal change hints
(`{kind, ref, project}` or `{kind, actor}`) — a client refetches the
affected resource on receipt rather than trusting the event payload as
the new state. No `Last-Event-ID` replay; a client that misses events
during a disconnect should refetch on reconnect. This is what the web
UI uses for live updates; an API client can use it too.

## Trying it

```sh
# Log in, capture the session cookie and CSRF token.
curl -c cookies.txt -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<password>"}'

# Create a ticket, echoing the CSRF token from the login response.
curl -b cookies.txt -X POST http://127.0.0.1:8080/api/v1/projects/ABC/tickets \
  -H 'Content-Type: application/json' -H 'X-CSRF-Token: <token>' \
  -d '{"type":"task","title":"Fix the thing","priority":"high"}'

# Or, as an agent with a bearer token — no CSRF header needed:
curl -H "Authorization: Bearer $TICKETS_API_TOKEN" \
  http://127.0.0.1:8080/api/v1/projects/ABC/tickets
```

For a typed client instead of raw `curl`, use `internal/apiclient`
(Go) or `web/src/api/` (TypeScript) as reference implementations, or
generate one from `api/openapi.yaml` with any OpenAPI codegen tool —
both hand-written clients exist by deliberate choice (see their
package doc comments), not because codegen wouldn't work.
