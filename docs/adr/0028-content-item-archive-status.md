# 0028: Content item archive status

## Context

A migration of an existing project onto Tickets surfaced a real
discoverability failure in `project_brief`, the tool a cold agent calls
first to orient itself (`docs/mcp-agent-guide.md`). The migration
created 39 plans in one pass, in numeric order. `RecentContentItems`
(`internal/store/content_items.go`, the query behind `project_brief`'s
`recent_plans` section) orders by `created_at DESC, id DESC` with a
hard `LIMIT 20` — with no other signal to rank by, the newest-inserted
rows filled the entire visible list, which happened to be the 20 oldest
precursor-design plans, not the 6 current ones the project's 9 open
tickets actually depend on. A cold agent calling `project_brief` saw a
wall of historical documents and no signal about which plans mattered.

The reporting agent's own workaround — a `START HERE` plan, deliberately
created last so recency keeps it on top — treats the symptom, not the
cause, and stops working the moment someone adds a newer plan without
also updating it. The reporting agent explicitly asked for a real lever
here, not a per-project convention.

ADR 0017 considered this and declined: "A content item has no `status`
field: §5.9 names no workflow for plans/documents the way §5.8 names
one for decisions, so none is added speculatively." That was correct
for a workflow status (proposed/accepted/rejected/superseded has no
plan/document analogue) but didn't anticipate a visibility-only
lifecycle flag — which projects already have, for exactly this "stop
showing me the old ones by default" problem (ADR 0021). This ADR adds
that flag to content items and supersedes ADR 0017's "no status field"
consequence.

## Decision

**`content_items.status`, active/archived, mirrors `projects.status`
exactly** — same column shape, same default, same
`domain.ContentItemStatus` type (`ContentItemStatusActive` /
`ContentItemStatusArchived`) as `domain.ProjectStatus`. Migration
0014 (`0014_content_item_status.sql`) adds it as
`TEXT NOT NULL DEFAULT 'active'`, so every existing row is active with
no backfill required.

**Archive only, not pin.** The reporting agent's fix used recency as a
forced signal (`START HERE`); the underlying ask was a way to say "this
is no longer current." Archiving 33 historical records solves that
directly and scales past 20 items, where a single pinned record would
not. A second, separate pinning mechanism was considered and rejected
as solving the same problem twice.

**Archive is visibility only — the same contract ADR 0021 gives
projects, for the same reason.** `Service.SetContentItemStatus`
(`internal/service/content_item.go`) flips `status` between `active`
and `archived`. An archived plan or document:

- Drops out of the default `GET /projects/{key}/plans|documents` page
  (`include_archived=false` is the default, mirroring `GET /projects`'
  own `include_archived`) and `RecentContentItems`
  (`project_brief`'s `recent_plans`/would-be `recent_documents`
  section) — the query that actually fixes the reported bug.
- Stays fully readable via `GET /plans|documents/{ref}` —
  `GetContentItem`/`store.GetContentItemByRef` are status-blind, the
  same reachability guarantee ADR 0021 called out: an archived item
  that couldn't be fetched would make unarchiving unreachable, and
  every backlink pointing at it would break.
- Does **not** cascade and is **not** soft-deletion (ADR 0013) —
  associations, links, backlinks, and comments on an archived item are
  unaffected.
- Is **not** filtered from search, for the same two reasons ADR 0021
  gives for projects: hiding a matching search result because of an
  unrelated lifecycle flag would be the actually-surprising outcome
  (search is explicit intent, unlike a passive list), and adding a
  `content_items.status` join to the FTS hot path isn't worth it for a
  narrow-value cosmetic filter.

**`RecentContentItems` hardcodes `status = 'active'`, not a parameter.**
It already hardcodes non-deleted-only for the same reason
`RecentAcceptedDecisions` hardcodes accepted-only: this is a
fixed-size orientation read, not a general listing, and there is no
caller that legitimately wants archived items mixed into "what's
current." This means **`project_brief.go` needed zero changes** — the
fix lives entirely in the store query that section already called.

**`SetContentItemStatus` never writes a `content_versions` snapshot.**
Unlike `UpdateContentItem`, which archives the pre-update row into
`content_versions` before every field edit (§5.9: "each edit saves a
full snapshot"), `store.UpdateContentItemStatus` only bumps
`entities.version` and flips the column — mirroring
`store.UpdateProjectStatus` exactly. Archiving is a lifecycle move, not
a content edit; a version-history row whose body is byte-identical to
its predecessor would only be noise. This is a deliberate asymmetry
from `record_update`'s own behavior (below).

**MCP: `record_update`'s existing `status` field grows a second,
optional meaning.** `record_update` already carried a decision-only
`status *string`, required (a nil value is `validation_failed`, per a
prior code-review fix — see `requireDecisionUpdateFields`). For a
plan/document, the same field is optional: nil means "leave the
current archive status unchanged." No new tool and no new field — the
jsonschema description states which contract applies per kind, the
same way `body`/`path`/`url`'s descriptions already say which
representation each applies to.

Internally this is `UpdateContentItemInput.Status *string`, threaded
through `InProcessBackend.UpdateContentItem`/`HTTPBackend.UpdateContentItem`
with the same status-then-fields sequencing
`InProcessBackend.UpdateProject`/`HTTPBackend.UpdateProject` use
(ADR 0021): an optional `SetContentItemStatus` call first, then the
field replace, chained against whichever version the prior step left
current. Unlike `UpdateProject`'s optional `Title`/`Description`,
though, a content item's `Title`/`Body`/`Path`/`URL` stay
unconditionally required (ADR 0017's original full-representation
contract) — so the field-replace step is never skipped, only ever
preceded by a status move. A status change made through `record_update`
therefore still bumps version twice and writes one (otherwise-identical)
`content_versions` snapshot, unlike the dedicated path below.

This is also a genuine atomicity gap, not just an inefficiency: the
status move commits in its own transaction, and if the field-replace
call that follows then fails (a concurrent writer's version bump, an
empty title, anything), the archive/unarchive has already taken effect
while the caller sees an error and holds a now-stale
`expected_version`. ADR 0021's `project_update` has the same two-call
shape, but a status-only project call skips the fields step entirely,
so the window only opens when a caller explicitly asks for both; every
content-item status change made through `record_update` opens it. A
caller that wants archive/unarchive to commit atomically — without
touching content, the version history, or risking this window — should
use the dedicated path below instead.

**Dedicated status-only path, mirroring ADR 0021's project split
exactly:** `POST /plans|documents/{ref}/status` (HTTP),
`tickets plan|document archive`/`unarchive` (CLI), and
`Service.SetContentItemStatus` (direct). None of the three ever touch
content or write a snapshot — this is the path that actually gets the
single-bump, no-snapshot behavior described above.

**`records_list` (MCP) and `GET /projects/{key}/plans|documents` both
gain `include_archived`**, defaulting to `false`, mirroring
`projects_list`/`GET /projects`. Without this, an archived item's ref
(needed to unarchive it) would be unreachable through either surface
once it dropped off the default page — the same reachability gap
ADR 0021 flagged for projects.

**Backup export/import round-trips the new column.**
`internal/backup`'s `ContentItemRow` gained `Status`; an envelope
exported before this ADR has no `status` field in its JSON, so import
falls back to `"active"` (the column's own default) rather than
writing an empty string that would silently fail every `status =
'active'` filter above.

## Consequences

- Migration 0014 adds `content_items.status`; no backfill, existing
  rows default to `active`.
- `ActivityEventType` gains `content_item_archived`,
  `content_item_unarchived` in both `internal/service/audit.go` and
  `api/openapi.yaml` — `TestActivityEventTypesMatchOpenAPIEnum`
  enforces the two stay in lockstep, the same gate ADR 0021 added
  events under.
- This change supplies the lever; it does not itself fix any
  already-migrated project. The 33 precursor plans/documents that
  motivated this ADR still have to be archived by whoever migrated
  them, and a `START HERE`-style pointer stays load-bearing until that
  happens.
- Web UI archive/unarchive controls are not part of this change —
  `web/src/api/types.ts`'s `ContentItemDetail`/`ContentItemCompact`
  carry the new `status` field for wire fidelity, but `ContentLibrary.tsx`/
  `ContentItemDetail.tsx` gained no new controls. A separate follow-up,
  if wanted.
- `cmd/tickets/mcp_parity_test.go`'s `routeToolMappings` gained two
  entries mapping the new `/status` routes to `record_update`, the
  same tool `PATCH /plans|documents/{ref}` already maps to.
