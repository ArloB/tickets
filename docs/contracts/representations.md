# Compact vs. detail representations

Documented now so the shape is frozen before any client (web UI, CLI,
MCP tool) is written against it. **Not implemented in Phase 0** beyond
what Step 5's fixed endpoints need — see Scope note below.

## Two fixed shapes per entity

- **Compact** — what list/search endpoints return by default. Small
  enough that a 20-record page (§11's MCP/CLI default limit) stays
  well under agent context budgets. No Markdown bodies, no comments,
  no relationship lists, no attachment contents (§7.2).
- **Detail** — what `GET .../{ref}` returns by default. Full record:
  Markdown body, relationship summary, comment count, feature/project
  context, timestamps, `version`. Still not comments *content* or
  attachment bytes — those are their own paginated sub-resources.

Example, ticket compact vs. detail:

```jsonc
// compact (list item)
{ "ref": "ABC-123", "title": "Fix the parser", "type": "bug",
  "status": "in_progress", "priority": "high", "severity": "medium",
  "updated_at": "2026-08-19T12:00:00Z", "version": 3 }

// detail (single-record response)
{ "ref": "ABC-123", "title": "Fix the parser", "type": "bug",
  "status": "in_progress", "priority": "high", "severity": "medium",
  "description": "## Repro\n...", "project": "ABC", "feature": "ABC-F1",
  "assignee": null, "creator": "agent:codex-1",
  "created_at": "...", "updated_at": "...", "version": 3,
  "comment_count": 2, "relationship_count": 1 }
```

## `fields` and `include` (contract only — not built in Phase 0)

- `fields=ref,title,status` narrows *either* shape to exactly the
  named top-level fields — for an agent that only needs three columns
  across 500 tickets.
- `include=comments,relationships` expands a detail response with
  sub-resources that are otherwise summarized (a count) rather than
  embedded.
- Both are additive query parameters; omitting them yields the fixed
  compact/detail shape above.

## Scope note

Product spec §7.2 and §9 require this mechanism so agent payloads stay
small, but its actual consumers — the MCP tool surface and CLI
`--fields`/`--include` flags (§7.3) — don't exist until Phase 3.
Building the parameter-parsing and dynamic-projection machinery in
Phase 0 would have no caller to validate it against. `fields` and
`include` parsing is implemented when Phase 2's `internal/httpapi`
work and Phase 3's MCP/CLI work actually need it, against this same
contract.

**Phase 1 status — the compact/detail split is now real for
projects, still not for tickets.** `GET /projects` returns
`internal/httpapi/wire.go`'s `projectCompact` DTO (`key`, `title`,
`status`, `version`, `updated_at` — no `description`, no
`created_at`), a distinct `api/openapi.yaml` schema
(`ProjectCompact`) from `GET /projects/{key}`'s full `Project` detail
shape. `domain.Project`/`domain.Ticket` themselves are still single
structs — the split lives in `wire.go`'s DTOs and mappers
(`toProjectDetail`/`toProjectCompact`/`toTicketDetail`), which also
now exist specifically so a future `domain.Ticket` field (Phase 1
already added `Assignee` and `DeletedAt`) doesn't reach the wire
without a deliberate edit, not just because no caller sets it yet.

Tickets have no list endpoint in Phase 0/1, so there is still no
`ticketCompact` — `ticketDetail` covers every ticket-returning
response. Build the compact shape alongside whichever phase adds a
ticket list/search endpoint, against the JSON shape this doc's
example already specifies. `fields`/`include` parsing itself remains
deferred for the reason above — no MCP/CLI caller exists yet to
validate the dynamic-projection machinery against.
