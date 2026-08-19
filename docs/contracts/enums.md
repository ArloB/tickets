# Enum wire values

Backed by `internal/domain/enums.go` and `enums_test.go`. Values below
are the frozen wire strings — reused verbatim by the JSON API, the
CLI's `--type`/`--status`/etc. flags, MCP tool schemas, and the web UI.
Renaming a value here is a breaking API change, not a refactor.

| Enum | Values (in spec order) | Spec ref |
| --- | --- | --- |
| `project_status` | `active`, `archived` | §5.3 |
| `ticket_type` | `task`, `bug`, `security`, `chore` | §5.5 |
| `workflow_status` | `backlog`, `ready`, `in_progress`, `blocked`, `review`, `done`, `cancelled` | §5.6 |
| `priority` | `critical`, `high`, `medium`, `low` | §5.6 |
| `severity` | `critical`, `high`, `medium`, `low` | §5.5 |
| `decision_status` | `proposed`, `accepted`, `rejected`, `superseded` | §5.8 |
| `relationship_type` | `parent_of`, `child_of`, `blocks`, `blocked_by`, `related_to`, `duplicate_of`, `supersedes`, `superseded_by` | §5.7 |
| `association_type` | `associated_with` | §5.7 (looser, non-directional link for decisions/plans/documents/features/tickets) |

Notes:

- `workflow_status` is shared by tickets **and** features (§5.4: "same
  initial workflow as tickets"). One Go type, one enum, one set of
  transition-validation rules — not two parallel copies.
- `severity` only applies to `ticket_type` `bug` and `security` (§5.5);
  `internal/domain` validation rejects a severity on `task`/`chore`.
- `relationship_type` values are stored as directed pairs
  (`parent_of`/`child_of`, `blocks`/`blocked_by`,
  `supersedes`/`superseded_by`) except `related_to`, which is genuinely
  its own inverse (symmetric — `A related_to B` and `B related_to A`
  are the same fact, so `internal/domain.CanonicalRelationship`
  canonicalizes it to one stored row by UUID order). **`duplicate_of`
  is not its own inverse** — an earlier version of this table implied
  it was, which is wrong: "A duplicate_of B" means B is canonical, not
  the reverse, so treating it as symmetric would let both directions be
  true simultaneously, a contradiction. §5.7 defines no `duplicated_by`
  counterpart for it, so a stored `duplicate_of` edge has no inverse
  view at all — `RelationshipType.Inverse()` returns `ok = false` for
  it, and `CanonicalRelationship` stores it exactly as given rather
  than reordering it. `internal/domain`'s cycle/inverse validation
  (product spec §5.7) is the code this row backs.

## Scope note

§5.6 permits transitions between *any* two workflow states — the
server does not reject "illegal" transitions; it just records every
one in the audit trail (§5.12), and warning-on-unusual-transition is a
client/UI concern, not server validation. So Step 5's status-update
endpoint only needs to accept any valid `workflow_status` value, not
implement a transition graph. Relationship **cycle** detection (§5.7)
*is* server-side validation, but ticket relationships are outside
Step 5's slice entirely and land in Phase 1. This document fixes only
the value sets both areas operate over.
