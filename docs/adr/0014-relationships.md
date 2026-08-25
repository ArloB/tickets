# 0014: Relationships — storage, canonicalization, cycle detection, and the duplicate_of correction

## Context

Product spec §5.7 defines eight relationship-type wire values across
four logical edge kinds (`parent_of`/`child_of`, `blocks`/`blocked_by`,
`related_to`, `duplicate_of`, `supersedes`/`superseded_by`), plus a
looser `associated_with` link for decisions/plans/documents/features/
tickets. It requires cycle rejection for `blocks`/`parent_of` but is
silent on exactly how a caller-supplied "view" type like `child_of`
should be stored, and an earlier draft of `docs/contracts/enums.md`
called `duplicate_of` "its own inverse" — which would let "A
duplicate_of B" and "B duplicate_of A" both be true simultaneously, a
contradiction, not a relationship.

## Decision

**One row per logical edge, canonicalized at write time — never a
pair kept in sync.** `ticket_relationships`'s primary key is
`(source_id, target_id, type)`. `domain.CanonicalRelationship` maps
any of the eight input types to the single stored triple:
`child_of`/`blocked_by`/`superseded_by` flip to their partner type
with endpoints swapped (`"A child_of B"` stores as `parent_of` with
`source=B, target=A`); `related_to` is genuinely symmetric and
canonicalizes by UUID string ordering, since there's no inherent
direction to prefer between two equal partners;
`parent_of`/`blocks`/`supersedes`/`duplicate_of` are already their own
canonical form and store exactly as given. Reading from the far end
uses `RelationshipType.Inverse()` (`ListRelationshipsForEntity`) rather
than a second stored row.

**`duplicate_of` has no inverse and is corrected accordingly.** §5.7
defines no `duplicated_by` counterpart the way it does for
`parent_of`/`blocks`/`supersedes`. Taking the spec at face value:
`duplicate_of` is directional (`"A duplicate_of B"` means B is
canonical), stored once, and visible **only** from the source end —
`RelationshipType.Inverse()` returns `(_, false)` for it, and
`ListRelationshipsForEntity` skips a stored `duplicate_of` row
entirely when listing from the *target*'s perspective rather than
inventing a synthetic label. The earlier "its own inverse" claim in
`docs/contracts/enums.md` was wrong and has been corrected there.

**Cycle detection is a recursive CTE, scoped to `blocks` and
`parent_of`, walked after canonicalization.**
`store.RelationshipWouldCycle(relType, newSourceID, newTargetID)`
checks whether `newTargetID` can already reach `newSourceID` by
following existing `relType` edges — if so, storing
`newSourceID -> newTargetID` would close a cycle back through the
existing path. Critically, this walk runs against the *canonical*
type and ids, not the caller's input — a cycle expressed by mixing
`parent_of` and `child_of` input (e.g. `A parent_of B`, then later `C
child_of B`, then `A child_of C`) is only caught because both
canonicalize into the same stored type before the walk begins.
`related_to`, `duplicate_of`, and `supersedes` have no cycle concept:
`related_to`/`duplicate_of` aren't a dependency graph, and
`supersedes` is a version-history pointer, not one either.

**Cross-project relationships are allowed.** Product spec §5.1 routes
cross-project context through relationships specifically, so
`AddRelationship` validates both endpoints are tickets
(`domain.ValidateRelationship`) but never compares their project keys
— unlike `MoveTicketFeature`, which ADR 0001 does restrict to the same
project.

**The audit event lands on the caller-supplied source, not the
canonical one.** See ADR 0012's attribution decision — canonicalizing
`child_of` into a `parent_of` row with swapped endpoints must not
change which entity's audit trail records the action.

**Duplicate edges are `already_exists`, not a version conflict.**
`ticket_relationships` rows have no `version` column — an edge either
exists or it doesn't, checked with `store.RelationshipExists` before
insert (the same pre-check convention `CreateProject`'s key-collision
check already uses, rather than parsing a UNIQUE-constraint error out
of the driver).

## Consequences

- `TestAddRelationshipDetectsParentOfCycle`
  (`internal/service/relationship_test.go`) is the regression test for
  the canonicalize-before-walk requirement specifically — it expresses
  the same graph through mixed `parent_of`/`child_of` input and
  confirms the cycle is still caught.
- `ListRelationshipsForEntity` joins the far endpoint against
  `entities` and filters `deleted_at IS NULL` — a relationship pointing
  at a soft-deleted ticket (ADR 0013) vanishes from the list rather
  than making `GetTicketRefByEntityID` fail the whole call, and
  reappears automatically on restore. This was a real bug found and
  fixed before any caller could hit it: the query originally had no
  such filter.
- `entity_associations` (the looser, symmetric `associated_with` link)
  follows the same canonicalization shape via
  `domain.CanonicalAssociation`, but with no type column (§5.7's
  `association_type` has exactly one MVP value) and no cycle concept —
  `internal/service/association.go` mirrors this file's pattern rather
  than sharing code with it, since the two differ enough (cycle
  detection, multiple types, direction) that a shared abstraction would
  cost more than the duplication it removes.
- **Stale as of Phase 5, corrected in the Phase 6 Step 1 audit:**
  `resolveAssociationEndpoint` returned `validation_failed` (not a 500)
  for a syntactically valid reference to a `decision`/`plan`/`document`
  when this ADR was written — `domain.ValidAssociationKind` allowed
  those kinds, but Phase 1 had no tables for them. Phase 5's content-item
  tables closed that gap; `resolveAssociationEndpoint` now resolves all
  five `ValidAssociationKind` kinds (`internal/service/association.go`),
  and this paragraph's `validation_failed` behavior no longer occurs for
  those three kinds. It still applies to any kind outside the valid set
  (a project or comment reference), which is the intended, permanent
  behavior.
