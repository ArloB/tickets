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

**Phase 0 status — the compact/detail split above is also not built
yet, not just `fields`/`include`.** `domain.Project` and
`domain.Ticket` are single structs; there is no separate compact
struct. `GET /projects` (documented "compact representation" in the
Phase 0 plan) and `GET /projects/{key}` both return the exact same
full record via `api/openapi.yaml`'s one `Project` schema — same for
`Ticket`. This was true even before `api/openapi.yaml` gained
`required`/`additionalProperties: false` on those schemas; the
stricter schema just makes it load-bearing (a handler that started
trimming fields for the list endpoint would now fail contract tests
instead of silently passing). Splitting into real compact/detail
shapes is deferred for the same reason `fields`/`include` are: no
caller (agent context budget, paginated UI list) exists yet to design
the split against. Do this alongside the Phase 3 MCP/CLI work above,
not before.
