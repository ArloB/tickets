# 0011: Position allocation and rank-based ordering

## Context

Product spec §5.6 requires manual ordering within a `(project,
priority)` group, and §5.5/§5.6 require the priority queue and issue
register to sort by priority/severity correctly — `critical, high,
medium, low`, not alphabetically. Two related but separate problems:
representing priority/severity as an efficient sort key, and letting a
caller move one record without rewriting every other record's position
on every drag-and-drop.

## Decision

**Rank columns, not `ORDER BY priority`.** `priority` and `severity`
are `TEXT`, so `ORDER BY priority` sorts `critical, high, low, medium`
— wrong. `tickets.priority_rank`, `tickets.severity_rank`, and
`features.priority_rank` are plain `INTEGER NOT NULL` columns (SQLite
disallows a non-`VIRTUAL` generated column via `ALTER TABLE ADD
COLUMN`, which is how these tables gained the columns — see migration
`0002_core_domain.sql`), written by exactly one place,
`internal/store/rank.go`'s `priorityRank`/`severityRank`, on every
insert or update of the source column. This is the same "enum
validation lives in Go, not SQL" convention the schema already uses
elsewhere, applied to ordering instead of validity. A sentinel rank of
4 (sorts last) covers `NULL` severity and any unrecognized value.
`idx_tickets_priority_queue`, `idx_tickets_issue_register`, and
`idx_features_priority_queue` are built on `(project_id, rank...,
position)` to match.

**Gap-spaced positions, not dense integers.** Every `(project,
priority)` group has a `position INTEGER NOT NULL` column. New records
land at the tail with a `PositionGap`-wide gap
(`domain.TailPosition`); moving a record between two existing ones
computes the integer midpoint (`domain.MidpointPosition`) rather than
shifting every intervening row. `domain.PositionGap = 1000` is wide
enough that ordinary reordering finds room via repeated bisection many
times (`log2(1000) ≈ 10`) before a group needs renumbering.
`domain.RenumberPositions` regenerates fresh, evenly spaced positions
for a group from scratch when `MidpointPosition` reports no room left
between two neighbors — the pure arithmetic lives in `internal/domain`
(ADR 0010's I/O-free package boundary) so the renumber-on-exhaustion
path is unit-testable without a database;
`internal/service/positions.go`'s `planPlacement` is what decides,
given a group's current members and a target slot, whether the cheap
path suffices or a renumber is needed.

**A renumber bumps only the record the caller moved.** When
`planPlacement` falls back to a full renumber, every *other* member of
the group gets its `position` rewritten via
`store.SetTicketPositionUnversioned` / `SetFeaturePositionUnversioned`
— no `entities.version` bump, no audit event. Only the record the
caller explicitly asked to move gets the versioned, audited write
(`SetTicketPositionVersioned` / `SetFeaturePositionVersioned`, one
`ticket_reordered`/`feature_reordered` audit event). The alternative —
bumping every renumbered row's version — would mean one drag-and-drop
could invalidate every other open `If-Match` token in the priority
group, which is a worse failure mode than "renumbering is mechanical
bookkeeping, not a change worth its own version." See
`docs/contracts/concurrency.md`'s Phase 1 addendum for the full list
of places Step 4b's mutations don't fit "version increments by exactly
1 per mutation."

**Changing priority moves a record to the tail of its new group.**
`UpdateTicketFields`/`UpdateFeature` compute
`domain.TailPosition(store.TicketGroupMaxPositionByPriority(...))`
whenever the request's priority differs from the row's current one,
rather than leaving the old `position` value in place (which would
sort arbitrarily relative to the new group's existing members — its
old position value has no relationship to that group's positions at
all).

## Consequences

- `TestReorderTicketForcesRenumberAndPreservesOrder`
  (`internal/service/positions_test.go`) drives `MidpointPosition` to
  exhaustion via repeated bisection and asserts both the resulting
  order and that the two untouched neighbors' `entities.version` did
  not move — the regression test for the version-bump asymmetry above.
- `TestPriorityQueueOrdersByRankNotText` /
  `TestIssueRegisterOrdersBySeverityThenPriority` (both in
  `internal/store/tickets_list_test.go`) are the regression guard for
  the rank-column fix — a `TEXT`-sorted `priority` fails them
  immediately (`low` before `medium`).
- §10's pre-migration backup (out of scope for this ADR, named here so
  it isn't silently forgotten) has no bearing on renumbering — a
  renumber is a normal transaction, not a schema migration. Backup
  tooling itself remains unbuilt until Phase 6.
- Features and tickets each get their own near-identical set of store
  functions (`TicketGroupOrderedExcluding` /
  `FeatureGroupOrderedExcluding`, etc.) rather than one generic
  parametrized-by-table-name function — consistent with this
  codebase's existing preference for explicit per-table functions
  (`GetTicketByRef`/`GetFeatureByRef` are already separate) over a
  premature shared abstraction.
