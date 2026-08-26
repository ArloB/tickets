# 0024: MCP/CLI/HTTP API parity sweep, and a regression guard

## Context

ADR 0023 closed one MCP gap (explicit feature selection) found by
actual usage. Looking for others turned up a pattern in the git
history: a route's HTTP wiring and its MCP tool wiring used to happen
in the same change, then several bulk route-wiring commits (ticket/
feature lifecycle routes, comment edit/delete, relationship/
association removal, external links, backlinks) and later Phase 5
work (decision/content-item versioning, the project activity feed)
added HTTP routes with no MCP follow-through and no comment marking
the omission as deliberate. Distinct from the already-documented,
genuinely deliberate exclusions: file attachments (no multipart
transport over MCP), subscribe/unsubscribe (ADR 0019), and agent/token
admin routes (`InProcessBackend` bypasses `requireAdmin`, an
architectural constraint, not scope-trimming).

## Decision

- Closed every undocumented gap found: ticket assign/reorder/delete/
  restore, feature status/reorder/delete/restore, comment list/get/
  update/delete/history, relationship/association removal
  (`ticket_unlink`, mirroring `ticket_link`'s dispatch), external
  links (`link_add`/`links_list`/`link_remove`), backlinks
  (`backlinks_list`), decision/content-item list/versions/diff
  (`records_list`/`record_versions`/`record_diff`, unified across
  kinds the same way `record_get`/`record_create`/`record_update`
  already are), and the project activity feed (`project_activity`).
- `service.CreateTicketRequest`-style permissiveness doesn't apply
  here: these are read/write primitives with no equivalent
  "convenience default" question, so no service-layer design tension
  like ADR 0023's — the work is Backend interface methods on both
  `InProcessBackend` and `HTTPBackend`, plus tool registration.
- Several `apiclient` methods didn't exist yet either (feature status/
  reorder/delete/restore, ticket reorder, backlinks) since the CLI
  never needed them — added there too, since `HTTPBackend` has no
  other path to the HTTP API. No new CLI subcommands were added for
  these; CLI/HTTP-API parity for feature lifecycle management and
  ticket reorder is a separate, pre-existing gap this sweep didn't
  scope in.
- Output types stay compact by convention: every `*_list` tool omits
  full bodies (`comments_list` needed a new `comment_get` for this
  reason — `TestListToolsOmitFullBodies` catches a violation
  structurally, not by review), and `TicketWriteResult` gained an
  `Assignee` field rather than having `ticket_assign` return a full
  `domain.Ticket` — the feature-move operation group on `ticket_update`
  (closing ADR 0023's tracked gap) also returns the compact
  `TicketWriteResult`, so `ticket_create` remains the one documented
  full-detail exception, not a new pattern.
- `cmd/tickets/mcp_parity_test.go` is the regression guard the
  underlying cause calls for: it reads `internal/httpapi.RouteList()`
  (a new exported accessor to the same `routeTable()`
  `route_table_test.go` already reads internally) and a live MCP tool
  session's `ListTools`, and asserts every mutating route has either a
  mapped tool or an explicit, reasoned exemption in a table. Adding a
  route without updating that table now fails a test instead of
  silently reproducing this gap.

## Consequences

- The MCP tool surface roughly doubled (23 → 49 tools with ADR 0023's
  feature-move operation group on `ticket_update` and this sweep
  combined), a larger jump than any prior single change. plan.md
  §7.2's "the initial tool surface should remain small" was about the
  *initial* surface; the precedent
  for growing it by closing real usage gaps was already established
  (`project_create`/`features_list`/`ticket_relationships`/
  `ticket_associations`, per `mcpsrv_test.go`'s
  `TestGapClosingToolsOverRealStreamableHTTP` doc comment) — this
  sweep is the same kind of change at larger scale, not a new one.
- `mcp_parity_test.go`'s mapping table is hand-maintained, not derived
  — adding a route still requires a human (or agent) to add the
  matching table entry. It converts "silently missing" into "test
  fails until someone decides," which is the actual gap this ADR
  closes; it doesn't make the decision automatic.
- Feature lifecycle management (status/reorder/delete/restore) and
  ticket reorder now have MCP tools and `apiclient` methods but still
  no CLI subcommand — flagged here rather than silently left
  inconsistent, for whoever picks up CLI/HTTP-API parity next.
