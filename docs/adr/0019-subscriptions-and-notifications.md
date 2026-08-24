# 0019: Subscriptions, notifications, and `@actor` mentions

## Context

Product spec §6.4 requires in-app notifications for assignment,
`@mentions`, replies/comments on subscribed work, and meaningful
changes to subscribed tickets/features/decisions — "creating or
commenting on an entity subscribes the actor by default; users can
unsubscribe." ADR 0015 built the entity-to-entity mention scanner
(`domain.ScanReferences`) but explicitly deferred the `@actor`
mention table to this step, "alongside the notifications that
actually consume it." SSE delivery of change hints (§6.4's second
paragraph) is Phase 5 Step 8, out of scope here — this step is the
notification *model*, not its real-time transport.

## Decision

- **`actor_mentions` is its own table, not a `derived_mentions`
  extension.** `derived_mentions.target_entity_id` FKs to
  `entities(id)` (ADR 0002); an actor has no row there. Same
  delete-and-reinsert-per-write discipline and `source_comment_id=0`
  sentinel as `derived_mentions`, extended into the *same*
  `rescanMentions` function (`internal/service/mentions.go`) rather
  than a parallel `rescanActorMentions` called from every one of that
  function's own eleven call sites a second time — one function, two
  scans, one transaction.
- **`@kind:name` explicit mentions only — no bare `@name` shorthand.**
  `idx_actors_kind_name` is unique on `(kind, name)`, not `name`
  alone: a human and an agent can share a name, so a bare-name form
  would be ambiguous about which actor was meant.
  `domain.ScanActorMentions` reuses `ActorRef.String`'s own wire
  format (`internal/domain/actor.go`) as the mention syntax, the same
  way `ScanReferences` reuses `Reference`'s own formatted form.
- **`subscriptions` is a live on/off table, not soft-deleted.**
  `(entity_id, actor_id)` primary key; subscribing is `INSERT OR
  IGNORE` (idempotent — a second create-or-comment by the same actor
  must not error), unsubscribing is a plain `DELETE`. No audit trail
  for a subscribe/unsubscribe toggle — it isn't an event worth one.
- **`notifications` is an append-only delivered-event log, not a live
  index.** Unlike `derived_mentions`/`actor_mentions` (which drop and
  rebuild on every relevant write) or `subscriptions` (a live flag), a
  notification is a historical fact: "actor X was told about event Y
  at time T." `read_at` is the only column an ordinary operation ever
  updates after insert — mirroring `audit_events`' immutability
  (§5.12) rather than the mention tables' cleanup-on-write pattern.
  Concretely: **a notification survives its source comment's
  soft-delete.** `DeleteComment` deletes its own `derived_mentions`/
  `actor_mentions` rows outright (no restore path for a comment), but
  a notification already delivered stays in the recipient's history —
  an already-seen "X commented" notification does not retroactively
  vanish because the comment was later deleted. Deliberately the
  opposite precedent from the mention tables, chosen because a
  notification's job is "tell me this happened," not "show me the
  currently-true state of the world" (that's what `derived_mentions`
  and the activity feed are for).
- **Notifications are emitted explicitly at each mutation site, not
  derived from `audit_events`.** `activityEventTypes`
  (`internal/service/activity.go`) already shows what maintaining an
  event-type-to-notification-category mapping costs, and the mapping
  wouldn't even be clean here: assignment maps onto one audit event
  type, but "meaningful change to subscribed work" does not — status
  changes and field updates are two different audit event types that
  both count, while reorder/move/assign do not, and a reused-event-
  type derivation would either miss cases or need its own parallel
  allowlist anyway. `notify`/`notifySubscribers`
  (`internal/service/notifications.go`) are called directly from
  `CreateTicket`/`CreateFeature`/`CreateDecision`/`CreateContentItem`
  (subscribe), `AssignTicket` (assigned), `AddComment` (commented +
  subscribe), `UpdateTicketStatus`/`UpdateTicketFields`/
  `UpdateFeatureStatus`/`UpdateFeature`/`UpdateDecision` (changed),
  and `rescanMentions` itself (mentioned).
- **No self-notification, structurally.** `notify` skips whenever
  `recipientActorID == triggeredBy`, unconditionally — an actor never
  learns about their own action through the notification inbox. This
  is why `AddComment` loads the subscriber list *before* auto-
  subscribing the commenter: documents the intent, though `notify`'s
  own check makes it correct either order.
- **Content items (plans/documents) are excluded from "changed"
  notifications.** §6.4 names "subscribed tickets, features, or
  decisions" for the changed category, not plans/documents — creating
  one still auto-subscribes its creator (consistent "creating
  subscribes" behavior), but `UpdateContentItem` does not call
  `notifySubscribers`. A comment edit (`EditComment`, as opposed to a
  new comment via `AddComment`) is likewise not a notification
  trigger — §6.4 lists "replies to or comments on," not "edits to,"
  and re-notifying on every typo fix would be spam.
- **Assignment does not also fire a "changed" notification.**
  `AssignTicket` emits exactly one `assigned` notification, to the new
  assignee only; it does not additionally fan a `changed` notification
  out to the ticket's other subscribers. Two notifications for one
  action was judged worse than one, even though assignment is
  arguably also a "meaningful change."
- **`GET/POST/DELETE .../subscribe` requires Editor permission on
  every method, including the read.** An anonymous viewer has no
  actor identity to attach a subscription (or a notification) to —
  `requestActor(r)` for an anonymous viewer resolves to no real
  actor, and there is no "subscribed: false, this is fine" state
  worth exposing to someone the concept doesn't apply to. This is the
  one read-only route family in this codebase gated above
  `routeViewer`, alongside the equivalent choice for `GET
  /notifications`.

## Consequences

- Reorder/move/assign-to-self mutations do not call `notify`/
  `notifySubscribers` — none of them are "meaningful changes to
  subscribed work" in §6.4's sense, and assignment is covered by its
  own `assigned` category instead.
- `notifications_list`/`notifications_mark_read` are the two MCP
  tools this step adds, matching the names `docs/mcp-agent-guide.md`
  already promised from Phase 3. There is no `subscribe`/`unsubscribe`
  MCP tool — the risk table's tool-surface-growth concern (ADR 0017)
  applies here too, and subscribing to something an actor didn't
  create or comment on is a rarer action than reading/clearing an
  inbox; it's reachable over HTTP and the CLI (`tickets subscribe`/
  `tickets unsubscribe <ref>`) instead.
- SSE change-hint delivery (§6.4's "the web client receives change
  hints through Server-Sent Events") is Phase 5 Step 8's work, not
  this one's — the web inbox (`web/src/routes/Notifications.tsx`)
  polls/refetches on navigation only, same as the activity feed did
  before Step 8 exists.
