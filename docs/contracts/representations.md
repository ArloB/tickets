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
Phase 0 would have no caller to validate it against. Step 5's three
endpoints return the fixed shapes above, unconditionally; `fields` and
`include` parsing is implemented when Phase 2's `internal/httpapi`
work and Phase 3's MCP/CLI work actually need it, against this same
contract.
