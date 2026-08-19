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

**Seed two actors, build no creation surface.** Migration
`0002_core_domain.sql` seeds `system` (for system-attributed writes —
none exist yet, but the row exists so the type is exercised) and
`human:local` (every Phase 1 mutation's actual attribution, since
there is no session to resolve a real one from). `store.GetActorIDByRef`'s
doc is explicit: these two are the only actors that can exist until
ADR 0004's authentication lands and gives Phase 2 a reason to build
real actor creation. `AssignTicket` can therefore only assign to an
actor that already exists — in practice, `human:local` — which is a
real, documented limitation, not a bug.

**A placeholder actor resolver, replaced without touching anything
else.** `internal/httpapi.requestActor(r)` and
`internal/mcpsrv.mcpActor()` both return the seeded `human:local`
actor unconditionally. Every mutation in `internal/service` already
takes an explicit `actor domain.ActorRef` parameter — Phase 2's real
session/token resolution only has to change what these two functions
return, not any of the ~20 service methods that consume the result.

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
- `idempotency_keys.actor_id` is explicitly *not* added despite actors
  now existing — see ADR 0008's updated consequences and
  `docs/contracts/concurrency.md`'s Phase 1 note for why (the primary
  key would need to widen too, which is Phase 2 work).
- `docs/contracts/enums.md` documents `kind:name` as the actor wire
  form; no schema change was needed since Phase 1 exposes no new HTTP
  endpoints (Step 5 keeps `internal/httpapi` below the API line for
  everything Step 4b added).
