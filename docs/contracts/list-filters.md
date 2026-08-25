# List-filter contract: ticket and feature filter query parameters

Backed by `internal/store` (`TicketFilters`/`FeatureFilters`,
`internal/store/tickets_list.go` / `features.go`) and
`internal/service` (`TicketListFilters`/`FeatureListFilters`,
`internal/service/ticket_list.go` / `feature.go`). Added in Phase 4 to
give the web UI's backlog and board views server-side filtering at the
product spec §11 reference scale (100,000 tickets) — client-side
filtering over an unfiltered page doesn't hold up at that size.

## Supported filters

`GET /projects/{key}/tickets`:

| Parameter | Matches | Enum-constrained in OpenAPI |
| --- | --- | --- |
| `status` | `WorkflowStatus` | yes |
| `type` | `TicketType` | yes |
| `severity` | `Severity` | yes |
| `priority` | `Priority` | yes |
| `feature_ref` | a feature reference, e.g. `ABC-F1` | no |
| `assignee` | an actor reference wire form, e.g. `human:alice` | no |
| `creator` | an actor reference wire form | no |
| `updated_since` | an RFC3339 timestamp | no (`format: date-time`) |

`GET /projects/{key}/features`: `status`, `priority`, `creator`,
`updated_since` only — a feature has no type, severity, assignee, or
containing feature (product spec §5.4).

## MCP surface parity

The `tickets_list` MCP tool exposes the same eight ticket filters
above (Phase 7 — `internal/mcpsrv/tools.go`'s `ticketsListInput`).
Before Phase 7 the tool took only `project_key`/`view`/`limit`/
`cursor`, so "find my assigned work" (§16 criterion 10's first
representative-workflow step) had no path over MCP even though the
HTTP endpoint always supported an `assignee` filter — see
`docs/mvp-acceptance.md` row 10. The CLI's `ticket list` still has no
filter flags at all; that gap wasn't in this pass's scope.

## Composition

- Every present filter is AND-composed with every other present filter
  and with whichever `?view=` selected the base ordering (tickets
  only). There is no OR, and there is no way to express one from the
  query string.
- `feature_ref`, `assignee`, and `creator` are resolved server-side
  (`internal/service`) to internal ids before reaching
  `internal/store` — a reference or actor that doesn't resolve is
  `400 validation_failed` on that field, never a silently-empty page.
  `feature_ref` must belong to the same project as the path's `{key}`;
  a well-formed reference from a different project is also
  `validation_failed`, not `not_found` — the caller supplied
  syntactically valid input for the wrong project, a client-fixable
  mistake distinct from "no such feature."
- `updated_since` must parse as RFC3339; it is reformatted into
  `store.TimeLayout` (fixed-width, UTC) before being compared against
  `entities.updated_at`, since that column is stored and ordered in
  `TimeLayout`, not RFC3339 — comparing the two formats directly as
  strings would silently misorder whenever their digit counts differ
  (`store.TimeLayout`'s own doc comment).
- `type`/`severity` filters remain meaningful even under
  `?view=issue_register`, whose fixed `type IN ('bug','security')`
  predicate already narrows to two types: `?type=bug` narrows further,
  to one.

## Filters and cursors

Filters do not encode into `?cursor=`. A client paginating a filtered
listing must resupply the identical filter parameters on every page
request — the cursor alone only carries the ordering position
(priority queue's 4-part `(rank, position, created_at, id)`, issue
register's 5-part equivalent, or the feature list's 3-part
`(rank, position, id)`; see `docs/contracts/representations.md`), never
which filters produced it.

This is a deliberate simplicity choice, not an oversight: it mirrors
how `?view=` itself already behaves (nothing rejects a priority-queue
cursor replayed with a *different* `?view=` value's filters layered on
— only a cursor from the *other view's shape entirely* is rejected, by
`store.DecodeCursor`'s component-count check). A client that changes
its filters mid-pagination and reuses an old cursor gets a page
computed against the new filters starting from the old cursor's
position — not an error, and not necessarily the page a human would
expect, but not silent data corruption either. Encoding a filter
fingerprint into the cursor (rejecting a mismatched replay the way a
wrong-shape cursor is already rejected) is a reasonable future
tightening if this proves confusing in practice; it was not built
speculatively here.

`GET /search` (Phase 5 Step 6, ADR 0018) is the one exception to the
`(created_at, id)`/rank-tuple cursor shape this doc otherwise
describes: bm25 relevance rank has no stable seekable key, so its
cursor is a capped, base64-encoded literal offset instead
(`store.EncodeSearchOffsetCursor`/`DecodeSearchOffsetCursor`), capped
at 500 results. Its `project`/`kind`/`status` filters follow the same
"resupply on every page, not encoded into the cursor" rule as
everything else on this page.

## Index coverage

`idx_tickets_priority_queue` and `idx_tickets_issue_register` cover
`(project_id, priority_rank[, severity_rank], position)` only — a
selective filter (e.g. `assignee`) on top of either index still scans
in ordering order and discards non-matching rows, rather than seeking
directly to matching ones.

**Verified at the product spec §11 reference scale (Phase 7):**
`internal/store/bench_test.go`'s `BenchmarkPriorityQueueFilteredByAssignee`
measures exactly this — a selective `assignee` filter (matching 1 of
4,000 tickets) over the 100,000-ticket reference dataset — at 1.37 ms
p95, comfortably inside §11's 100 ms target (~73x headroom) though
visibly slower than the unfiltered priority queue (about 7x), the scan-
and-discard cost this section describes. See `docs/benchmarks.md` for
the full run. No covering index was added, per this section's own
instruction: add one only where a benchmark actually shows it's
needed, not speculatively — and this one doesn't show that yet.
