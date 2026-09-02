# 0013: Soft-deletion semantics

## Context

Every read path in `internal/store` has filtered `deleted_at IS NULL`
since Phase 0's schema, but nothing wrote `deleted_at` until Step 4b —
it was scaffolding, not load-bearing behavior. Product spec §5.4
requires a feature holding tickets to not silently delete them, and
ADR 0001 already forbids a nullable `tickets.feature_id`, which
creates a real constraint on what restoring a ticket is allowed to do.

## Decision

**One generic conditional update per direction, not one per table.**
`store.SoftDeleteEntity`/`RestoreEntity` operate on `entities.deleted_at`
directly and work for any principal entity — soft-deletion lives
entirely on the `entities` registry row (ADR 0002), never on a
kind-specific table, so nothing needs to know which table a given
`entityID` belongs to. Both are version-guarded the same way every
other conditional update is (`UPDATE ... WHERE id = ? AND version = ?
AND deleted_at IS { NULL | NOT NULL }`), returning `ErrVersionConflict`
on a mismatch. Restoring bumps `version` too — restoring is itself a
change a stale `If-Match` should catch, the same reasoning as any
other mutation.

**A feature blocks deletion by default when it has non-deleted
tickets; `Cascade: true` deletes them together.** `DeleteFeature`
counts non-deleted tickets via `store.ListTicketEntityIDsForFeature`
and returns `409 has_dependents` naming the count unless `Cascade` is
set. With cascade, the feature and every dependent ticket are
soft-deleted in one transaction — each dependent ticket via
`store.SoftDeleteEntityUnconditional`, which skips the
`ExpectedVersion` check entirely (the caller deleting the *feature*
has no way to know each dependent ticket's individual version) but is
still safe: the transaction's write lock (`BEGIN IMMEDIATE`, ADR 0003)
guarantees nothing else touched a dependent between it being counted
and being deleted, moments later in the same transaction. Each
cascade-deleted ticket gets its own `ticket_deleted` audit event
(`cascade_from` naming the feature) in addition to the feature's own
`feature_deleted` event — ADR 0012's "one event per logically-changed
record."

**The General feature can never be deleted**, cascade or not — checked
by comparing the target feature's internal id against
`projects.general_feature_id` before anything else runs. This is
ADR 0001's rule, enforced here rather than only documented there.

**A ticket has no dependents check.** Its comments, relationships, and
derived mentions aren't blocking dependencies — they're filtered out
of every read path once the ticket itself is gone
(`ListRelationshipsForEntity`/`ListAssociatedEntityIDs`/
`ListMentionTargetsFromSource` all join the far endpoint against
`entities` and filter `deleted_at IS NULL`, so an edge pointing at a
deleted ticket simply vanishes from the list rather than erroring —
and reappears automatically on restore, no cleanup step needed).
Comments are the one exception: a comment's own `derived_mentions`
rows (the mentions *it* creates, not mentions *of* it — a comment can
never be a mention target) are deleted outright on comment soft-delete
(`DeleteComment`), not filtered, because comments have no restore path
in the plan — there's nothing to preserve them for.

**Restore refuses when the ticket's feature is itself deleted.** ADR
0001 forbids a nullable `feature_id`, so restoring a cascade-deleted
ticket on its own would point it at a deleted feature with no escape
hatch. `RestoreTicket` resolves the ticket's current feature (via its
own `FeatureRef`, `GetFeatureByRefAnyDeletion`) and returns
`validation_failed` naming the feature if it's still deleted — restore
the feature first. Restoring a feature does **not** auto-restore its
cascade-deleted tickets; each is restored individually once its parent
is live again, which this same check then allows.

**A soft-deleted row is invisible even to the function that would
restore it, unless that function says otherwise.** Every normal
`Get*ByRef` filters `deleted_at IS NULL` — including the one Restore
would naturally reach for. `GetTicketByRefAnyDeletion` /
`GetFeatureByRefAnyDeletion` are the deliberate exception: the same
query, minus the filter, used only by the restore path (and nowhere
else). `domain.Ticket`/`domain.Feature` both gained a `DeletedAt
*time.Time` field (`omitempty`, always nil on every ordinary read) so
that state can be carried back when one of these variants is actually
used.

## Consequences

- **Restore's discoverability gap is closed as of Phase 2 (Step 9).**
  Through Phase 1, `DeleteTicket`/`DeleteFeature` returned only
  `error` — no version, no confirmation the caller could use to
  construct a `RestoreTicket` call, and every normal read hid the
  deleted row, so a caller had no way to learn the version
  `RestoreTicket` needs without already knowing it from before the
  delete. Both now return `(newVersion int64, err error)` —
  `store.SoftDeleteEntity` already computed this value; Step 9 was
  purely plumbing it out. Over HTTP (Step 13), `DELETE
  /tickets/{ref}`/`DELETE /features/{ref}` return a `deleteResponse{
  version int64}` body carrying exactly that value
  (`internal/httpapi/wire.go`), so a caller can construct the
  subsequent `POST .../restore` call's `If-Match` from the delete
  response alone, no second read required. `internal/service/soft_delete_test.go`'s
  tests now assert against the real returned value instead of the
  hand-computed `ticket.Version + 1` this bullet originally described.
- `TestReadPathsSoftDeleteFiltering`
  (`internal/service/soft_delete_readpath_test.go`) is the checklist
  the Phase 1 plan's Risks table asked for: every exported
  `internal/store` read function this phase touched, with an explicit
  `hidesDeleted` intent, so the next one added has to make the same
  decision on purpose. `GetComment`/`ListCommentsForEntity`/
  `ListCommentVersions`/`ListAuditEvents` deliberately do **not**
  filter — a comment's tombstone stays visible (§5.10) and an audit
  trail is append-only (§5.12) regardless of what happens to the
  entity it describes.
- `TestDeleteFeatureCascadeDeletesTicketsToo` caught a real ordering
  bug during development: resolving a cascade-deleted ticket's ref for
  its audit event *after* soft-deleting it returned `not_found`, since
  `GetTicketRefByEntityID` filters deleted rows like every other read.
  Fixed by resolving the ref before the delete, not by adding a
  deleted-aware variant of that function — nothing else needs one.
- **The remaining discoverability gap — knowing a deleted ref exists
  at all, as opposed to already holding one — is closed by the web
  UI, not the API.** `?include_deleted=true` (Step 13, above) only
  helps a caller that already has the ref; nothing server-side lists
  soft-deleted rows. `TicketDetail`/`FeatureDetail`
  (`web/src/routes/`) retry a 404 with `include_deleted=true` before
  giving up, rendering a "this was deleted — Restore" view instead of
  a bare not-found when that's what the 404 actually was. The
  activity feed (`ActivityFeed.tsx`) already links every
  `ticket_deleted`/`feature_deleted`/`*_restored` event's `entity` ref
  to its detail route, so that link — previously a dead end — is now
  the discovery path this ADR's Decision assumed existed. Feature
  delete's cascade choice is surfaced explicitly in the same UI
  (`DeleteFeatureButton`, `FeatureDetail.tsx`) rather than retried
  silently on `has_dependents`, and Delete is never offered on the
  General feature (`isGeneralFeature`, keyed off the structural
  `-F1` ref rather than the renameable title) — both mirroring this
  ADR's Decision section rather than re-deriving the rule.
