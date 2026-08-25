# 0021: Project edit and archive semantics

## Context

Product spec §6.1 requires "Create, edit, archive, browse, and search
projects." Through Phase 6, only create/browse/search existed —
`docs/mvp-acceptance.md` row 3 tracked this as a known gap, deliberately
deferred rather than built during release hardening: "release hardening
is the wrong phase to add a new cross-layer feature (service + HTTP +
OpenAPI + CLI + MCP + web UI, plus an optimistic-concurrency design
decision) this late." That rationale was about timing, not a decision
never to build it. This is Phase 7's headline item, closing the gap.

The schema was already shaped for this in Phase 1 and never wired up:
`projects.status TEXT NOT NULL DEFAULT 'active'` (`0001_initial.sql`),
`entities.version` shared by every entity kind including projects, and
`domain.ProjectStatus` (`active`/`archived`) already on the domain type.
So this ADR is about behavior, not schema: what does "archive" mean,
and does search/list agree with each other about it.

## Decision

**Archive is visibility only.** `Service.SetProjectStatus` flips
`status` between `active` and `archived` (`internal/service/project.go`).
An archived project:

- Drops out of `GET /projects`' default page (`ListProjects`'
  `includeArchived` parameter, default `false`) and the CLI/web
  project lists.
- Stays fully readable via `GET /projects/{key}` — `GetProject`/
  `store.GetProjectByKey` are deliberately status-blind, taking no
  `includeArchived` parameter at all. An archived project that
  couldn't be fetched would make unarchiving unreachable through the
  API, and every ticket detail page under it would break (a ticket's
  `project` field links back to a project read that must still
  succeed).
- Does **not** cascade. Its tickets, features, decisions, plans, and
  documents stay fully readable and writable, exactly as before
  archiving. Archive is not soft-deletion (ADR 0013) — it is a
  lifecycle flag on the project record itself, the same category as a
  ticket's workflow status, not a permission boundary.

**Alternative considered and rejected: reject writes until
unarchived.** This would add a check to every mutating service path
under a project (ticket/feature/decision/plan/document create and
update) plus an unarchive escape hatch on each — a large, diffuse
surface for a lifecycle flag §5.3 describes as a state, not an access
control. The project's own edit (`UpdateProject`) and status move
(`SetProjectStatus`) are already split the same way
`UpdateFeature`/`UpdateFeatureStatus` and `UpdateTicketFields`/
`UpdateTicketStatus` are, so a plain field edit can never clobber a
concurrent archive/unarchive or vice versa — that's the concurrency
protection this feature actually needs; rejecting writes underneath an
archived project would be a second, unrelated feature.

**Search is not filtered by archive status — deliberately
inconsistent with `GET /projects`.** `internal/service/search.go`'s
`searchKinds` includes `"project"`; nothing in the search path checks
`projects.status`. Two reasons, not one:

- Consistency with the point above: an archived project's tickets stay
  searchable (they're unaffected by the project's own status), so
  hiding the project's own row while its tickets remain findable would
  be the actually inconsistent outcome — a search for a project's name
  would surface everything *except* the project itself.
- Performance: filtering search by project status would add a
  `projects.status` join to the FTS hot path. `docs/benchmarks.md`
  already shows full-text search is the slowest thing in the system,
  and its pathological case (a term matching most of the corpus) is
  the one §11 performance target left deliberately unmet — adding a
  join there for a narrow-value cosmetic filter isn't worth the risk.

The asymmetry is intentional: browsing a list is passive triage
("what am I working on"), where hiding archived noise is the point;
searching is explicit intent ("find X"), where hiding a matching
result because of an unrelated lifecycle flag would be surprising.

**Projects are not subscribable, and this feature doesn't change
that.** `UpdateProject` does not call `notifySubscribers` — unlike
`UpdateFeature`, which does. `resolveSubscribableEntity`
(`internal/service/notifications.go`) delegates to
`resolveMentionTarget`, which handles ticket/feature/decision/plan/
document but has no `domain.KindProject` case (ADR 0019 never added
one). Wiring `notifySubscribers` into `UpdateProject` would silently
be a no-op. Making projects subscribable is a separate, undecided
feature that would touch ADR 0019's design — not bundled into this
one. `rescanMentions`/`publishNotified` still run on `UpdateProject`'s
description edit, since `@mentions` and backlinks are unrelated to
subscriptions and already worked on project creation.

**MCP's `projects_list`/`HTTPBackend.ListProjects` hardcode
`includeArchived=false`, with no tool-level override.** An agent has
no obvious reason to browse archived projects during ordinary work —
`project_get`/`search` both stay reachable regardless of status, which
covers "does this specific project still exist" and "find X across
everything." A `include_archived` MCP parameter can be added later if
an actual workflow needs it; it wasn't spec'd as part of this feature
and would be one more field on an already-compact list tool.

**`project_update` is one merged MCP tool, not two.** Mirroring
`ticket_update`'s `Status` + field pointers pattern
(`internal/mcpsrv/ticket_update.go`): a caller can set `title`/
`description`, `status`, or both in one call. Internally this is still
two separate service calls when both are set — status via
`SetProjectStatus` first, then fields via `UpdateProject` against the
version the status call returned — matching the HTTP surface's own
two-endpoint split (`PATCH /projects/{key}` vs.
`POST /projects/{key}/status`) rather than adding a combined service
method. The tool-level merge is purely an ergonomics choice for the
caller; nothing server-side treats "update a project" as a single
atomic operation across both fields.

## Consequences

- No migration. `projects.status` and `entities.version` already
  existed; this ADR only wires up code paths against them.
- `internal/store/search.go`'s `RebuildSearchIndex` gained a projects
  pass and, incidentally, a real bug fix: its comment-indexing query
  used to `JOIN comments c ... JOIN tickets t ON t.id = c.entity_id`,
  which silently dropped comments on any non-ticket entity
  (project/feature/decision/plan/document — ref-agnostic comments
  landed in Phase 6 Step 2, but this rebuild query was never updated
  to match) on every rebuild. Generalizing the projects loop's
  `owners` map — already used by the attachments/links passes below it
  — to also serve the comments pass fixed this for every commentable
  kind at once, not just projects.
- `ActivityEventType` gained `project_updated`, `project_archived`,
  `project_unarchived` in both `internal/service/audit.go` and
  `api/openapi.yaml`'s enum — `internal/service.TestActivityEventTypesMatchOpenAPIEnum`
  (Phase 6 Step 11) enforces the two stay in lockstep, so this was a
  required pair of edits, not optional.
- CLI `project update`/`project archive`/`project unarchive` need
  `--if-version`, and the only human-mode source for a project's
  current version is `project list` (`project brief` doesn't print
  it, and there is no `project get` subcommand). For an already-
  archived project specifically, that means `project list
  --include-archived` — the default list excludes it. The commands'
  own `--if-version` help text says this explicitly, since "from a
  prior project get/list" would be misleading (there is no `get`).
