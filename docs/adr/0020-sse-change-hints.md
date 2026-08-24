# 0020: SSE change hints

## Context

Product spec §6.4: "The web client receives change hints through
Server-Sent Events (SSE). It refetches affected records through the
normal API, keeping the real-time protocol simple and making the HTTP
API the source of truth." §8.2's stack table picks SSE specifically
over a bidirectional socket protocol as "sufficient for server-to-
browser notifications and simpler." §17's risk table adds the
governing constraint: "Live events become another source of state. |
Treat SSE as invalidation/change hints only; refetch authoritative
state from the API." Phase 5's exit criterion: "two browsers observe
changes and notifications without manual full-page refresh." This is
Phase 5 Step 8 — the delivery transport for the notification/
subscription model ADR 0019 already built, and for board/detail-page
live updates more generally.

## Decision

- **A hint carries only `{kind, ref, project}` or `{kind, actor}` —
  never a changed value.** `service.ChangeHint` (`internal/service/
  events.go`) has exactly two kinds: `entity_changed` (`ref`/`project`
  set) and `notifications_changed` (`actor` set). This isn't just a
  convention the handler follows — `changeHintWire`
  (`internal/httpapi/events.go`) has no field a status, title, or body
  could ever be written into, so §17's "hints only" rule is enforced
  by the wire type's own shape, not by every future call site
  remembering not to add one.
- **Every mutation publishes its hint from the same place it already
  emits an audit event and notifications — the outer method, strictly
  after `s.withTx` returns a nil error.** `Service.broadcast`/
  `publishNotified` are called once per mutation, after the commit
  check, never from inside a `txFunc` closure — a hint for a rolled-
  back transaction must never reach a subscriber. This mirrors ADR
  0019's "explicit emission at each mutation site" choice for the same
  reason: `activityEventTypes`-style derivation from `audit_events`
  would need its own parallel allowlist anyway, and a board's "did the
  position change" question doesn't map cleanly onto one audit event
  type.
- **`notify`/`notifySubscribers`/`rescanMentions` return the recipient
  actor ids they actually notified**, instead of only an error, so the
  calling mutation method can fold them into one post-commit
  `publishNotified` call. This is the same "collect during the
  transaction, act after it commits" shape as the entity hint, applied
  to notifications specifically because their recipient set (unlike an
  entity's ref/project) isn't known until the transaction runs.
- **The hub is in-process, not a shared backend.** `internal/httpapi.
  Hub` is a `map[int64]*hubSubscriber` behind a mutex, matching the
  product's single-server deployment model (§8.1/§11) — a multi-
  instance deployment would need Redis/NATS-style fan-out instead,
  out of scope here. `Hub` implements `service.Broadcaster` and is
  registered via `Service.SetBroadcaster` in `NewHandler`, so every
  mutation has somewhere to publish to before any browser has ever
  connected; a CLI-only process or a test that never calls
  `SetBroadcaster` gets a nil-safe no-op instead (mirrors `blobs`
  being optional on `Service`).
- **A full subscriber channel drops the hint rather than blocking
  `Publish`.** `Hub.Publish` uses a non-blocking `select`/`default`
  send on a small (16-deep) buffered channel per connection. A dropped
  hint is not a correctness gap: §17 already requires the client to
  treat the stream as an invalidation signal and refetch, never trust
  it as an event log, so a missed hint is just as recoverable as one
  the client was never sent — the next hint for the same entity, or
  the connection's own reconnect, catches it up. This also protects
  every mutation's write path from ever blocking on a slow browser.
- **`notifications_changed` is delivered only to the one connection
  whose authenticated actor matches** — `Hub.Publish` compares
  `hint.Actor` against each subscriber's own `actor` field, gating
  strictly at publish time rather than only being conventionally
  scoped. `entity_changed` has no such gate: it goes to every
  subscriber, anonymous viewers included, since a board redraw isn't
  actor-scoped information. This is why `GET /events` is `routeViewer`
  rather than `routeEditor` (unlike ADR 0019's subscribe/notifications
  routes) — an anonymous connection is a legitimate subscriber for
  half the hint space, so there is no "not applicable to you" case to
  gate the whole route on the way there was for a per-actor inbox.
- **`Hub.Close` exists because `net/http`'s own graceful shutdown
  doesn't end a long-running handler by itself.** `srv.Shutdown` stops
  accepting new connections and waits for in-flight ones to finish
  naturally, but an SSE handler blocked on a channel receive never
  finishes on its own. `cmd/tickets/server.go`'s `serve` now takes a
  `closeBroadcaster func()` (`svc.CloseBroadcaster`, a type-asserted
  best-effort call — see `Service.CloseBroadcaster`'s doc) and calls
  it before `srv.Shutdown`, closing every subscriber channel so each
  handler goroutine's `select` unblocks and the connection ends,
  letting graceful shutdown actually complete within
  `shutdownTimeout` (product spec §11) instead of stalling until it
  expires.

## Consequences

- Reorder/move mutations broadcast `entity_changed` even though ADR
  0019 deliberately excludes them from notifications — a live board
  redraw and a notification-worthy "meaningful change" are different
  questions; ADR 0019's exclusion is about the inbox, not about
  whether a second browser needs to see a card move.
- SSE payloads are not schema-validated the way other endpoints'
  JSON responses are (`api/openapi.yaml`'s `/events` path documents
  this directly) — an indefinite `text/event-stream` body has no
  fixed shape for `openapi3filter`'s response-schema validation to
  check, so `httpapi` tests against this route use a raw client, not
  the schema-validated `testServer.do` helper every other route's
  tests use.
- No reconnect/backoff/replay protocol: `EventSource`'s built-in
  auto-reconnect, plus each detail/board page's own refetch-on-mount,
  is sufficient for the exit criterion as written ("two browsers
  observe changes... without manual full-page refresh") — a missed
  hint during a brief disconnect is caught by the next hint or the
  page's own next natural refetch, not by replaying a gap from a
  Last-Event-ID cursor.
