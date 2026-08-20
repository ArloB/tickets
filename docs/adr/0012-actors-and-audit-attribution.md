# 0012: Actors and audit attribution

## Context

Product spec §4.1 requires every mutation to be attributed to an
actor (human, agent, or system) even before real authentication
exists — ADR 0004's session/token model is Phase 2 work. Product spec
§5.12 requires an append-only audit trail: every mutation writes an
event carrying who did it, what changed, and a correlation id, in the
same transaction as the mutation it describes.

## Decision

**`actors` is its own table, not an `entities` row.** Unlike
projects/features/tickets, actors have no project, no public
reference, and no soft-delete-by-project semantics — ADR 0002's
`entities` registry is specifically for project *content*. `actors`
carries `kind` (`human`/`agent`/`system`), `name`, an optional
`owner_id` (an agent's owning human, §4.1), and its own `deleted_at`.
The wire form is `kind:name` (`domain.ActorRef.String()`/
`ParseActorRef`), matching `docs/contracts/representations.md`'s
`"creator": "agent:codex-1"` example — `ActorRef` implements
`MarshalJSON`/`UnmarshalJSON` to render as that plain string, not a
nested object.

**Seed two actors, build no creation surface — true through Phase 1.**
Migration `0002_core_domain.sql` seeds `system` (for system-attributed
writes) and `human:local` (every Phase 0/1 mutation's actual
attribution, since there is no session to resolve a real one from).
Through Phase 1, `store.GetActorIDByRef`'s doc was explicit that these
two were the only actors that could exist. **Phase 2 (Step 4) built
real actor creation**: `service.CreateAdminAccount` (via
`tickets setup`) creates the first human account, and
`service.CreateAgent`/`CreateAgentToken` create agent actors and their
bearer tokens (admin-only, `internal/httpapi/admin.go`). `AssignTicket`
can now assign to any real actor a caller creates, not just
`human:local` — the limitation this bullet originally documented no
longer holds.

**A placeholder actor resolver, replaced without touching anything
else.** `internal/httpapi.requestActor(r)` and
`internal/mcpsrv.mcpActor()` both return the seeded `human:local`
actor unconditionally. Every mutation in `internal/service` already
takes an explicit `actor domain.ActorRef` parameter — Phase 2's real
session/token resolution only has to change what these two functions
return, not any of the ~20 service methods that consume the result.

**Phase 2 correction: it wasn't quite "just change what they return."**
`requestActor`/`mcpActor` weren't simply pointed at new logic in
place — they were replaced by a one-line read off a request-scoped
`auth.Principal{Actor, Permission, IsAdmin, AuthMethod}` that
`internal/httpapi`'s authentication middleware (and
`internal/mcpsrv`'s `withCallerActor`) resolves once per request and
stores on the context (`auth.WithPrincipal`/`auth.FromContext`, ADR
0004). A bare `domain.ActorRef` return value can't express "no valid
credentials," "anonymous, no actor at all," or "authenticated but
insufficient permission" without inventing a fake actor row for each
— `Principal` is what actually made that distinction expressible.
Every downstream service method call site is still unchanged, exactly
as predicted; the correction is that the *resolution mechanism*
itself gained a real type, not just a real implementation behind the
same bare-`ActorRef`-returning signature this ADR originally
described.

**The tx helper resolves the actor once, before the mutation body
runs.** `Service.withTx(ctx, actor, correlationID, fn)` resolves
`actor` to its internal id via `store.GetActorIDByRef` immediately
after `BeginTx`, then passes `(tx, actorID, correlationID, now)` into
`fn`. This is what makes audit-event emission structurally uniform:
every mutation's closure already has the actor id and the shared
transaction timestamp in scope, so writing an `audit_events` row is
one more call, not a lookup a future author has to remember to add.

**The audit event goes on the entity the actor took the action on,
not necessarily where the row physically landed.** Two places this
matters: `AddRelationship`/`AddAssociation` canonicalize their storage
(`CanonicalRelationship`/`CanonicalAssociation` — ADR 0014 — can swap
source/target), but the audit event is always recorded against the
caller-supplied source, since that's the entity the actor actually
acted on. A feature-cascade delete (ADR 0013) emits one
`feature_deleted` event on the feature and a separate `ticket_deleted`
event on each affected ticket — the cascade is one transaction, but
each affected record gets its own entry in its own trail.

## Consequences

- Every mutation added in Step 4b (comments, relationships,
  associations, positions, soft-deletion) follows the same shape:
  resolve rows first, write the mutation, write exactly one audit
  event per logically-changed record, all inside `withTx`'s
  transaction. `TestTicketLifecycleAuditTrail`
  (`internal/service/lifecycle_test.go`) is gate 3's regression test —
  it drives one ticket through ten operations and asserts the audit
  trail is exactly the expected ten-event sequence with the right
  actor on each, which is the only test that would catch a missing or
  duplicated emission.
- `idempotency_keys.actor_id` was added in Phase 2 (Step 2, migration
  `0004_identity_and_auth.sql`), widening the primary key to
  `(key, actor_id)` — the gap this bullet originally flagged (adding
  the column without widening the key wouldn't have distinguished two
  actors reusing the same key) is closed; see ADR 0008's updated
  consequences.
- `docs/contracts/enums.md` documents `kind:name` as the actor wire
  form. Phase 2 (Step 13) exposed it over HTTP directly on
  `ticketDetail`'s `creator` and `assignee` fields
  (`internal/httpapi/wire.go`) — the string form is exactly
  `ActorRef.String()`. `domain.Feature`/`domain.Project` also gained a
  `Creator` field in Step 9 for the store/service layers, but neither
  `featureDetail` nor `projectDetail` wires it onto the response yet;
  that's a deliberate deferral (see this file's top doc on the DTO
  layer's "add on purpose" contract), not an oversight — add it there
  when a caller actually needs it.
