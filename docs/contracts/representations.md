# Compact vs. detail representations

Documented now so the shape is frozen before any client (web UI, CLI,
MCP tool) is written against it. Fully implemented as of Phase 2 Step
14 — see the status note below for what changed from the original
contract.

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
  "created_at": "...", "updated_at": "...", "version": 3 }
```

As built, a detail response carries no `comment_count`/
`relationship_count` summary fields — those two keys in the original
sketch above are dropped; see the status note below for why.

## `fields` and `include`

- `fields=ref,title,status` narrows *either* shape to exactly the
  named top-level fields — for an agent that only needs three columns
  across 500 tickets. An unknown field name is `validation_failed`,
  not silently dropped, checked against a per-DTO allow-list
  (`internal/httpapi/representation.go`'s `allowedTicketCompactFields`/
  `allowedTicketDetailFields`).
- `include=comments,relationships` adds a `comments`/`relationships`
  array to a detail response. Both keys are absent entirely (not
  present as `null` or a count) unless requested.
- Both are additive query parameters; omitting them yields the fixed
  compact/detail shape above.

## Status

Fully implemented as of Phase 2 Step 14
(`internal/httpapi/representation.go`, `tickets.go`'s `listTickets`/
`getTicket`). One deliberate departure from the original sketch above:
`comment_count`/`relationship_count` summary fields were dropped from
the always-on detail shape. The original idea was that `include=`
would *expand* a count already present into the full array; building
that means every `GET /tickets/{ref}` pays for two `COUNT` queries
whether or not a caller ever asks for `include=`, for a value (a bare
count, with no way to act on it) that turned out to have no confirmed
consumer. `include=` in the shipped version instead adds a field that
is otherwise wholly absent — cheaper, and arguably a cleaner opt-in
than sketching a count no client asked for.

The compact/detail split covers projects, features, and tickets.
`domain.Project`/`domain.Feature`/`domain.Ticket` themselves stay
single structs — the split lives in `internal/httpapi/wire.go`'s DTOs
and mappers (`toProjectDetail`/`toProjectCompact`,
`toFeatureDetail`/`toFeatureCompact`, `toTicketDetail`/
`toTicketCompact`), so a future domain field doesn't reach the wire
without a deliberate edit, not just because no caller sets it yet.

`fields=` on a narrowed response is not validated against
`api/openapi.yaml`'s fixed 200 schema for that operation — OpenAPI 3.0
has no way to express a response shape conditioned on a query
parameter's value, documented on the `fields` parameter itself in the
spec. `include=` stays fully contract-validated, since it only adds
optional properties the schema already declares.
