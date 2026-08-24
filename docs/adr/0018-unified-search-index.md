# 0018: Unified search index — synthetic rowid over entities + comments

## Context

Product spec §5.12 calls for one full-text search across tickets,
features, decisions, plans, documents, and comments, ranked by
relevance. ADR 0003 anticipated building this on the external-content
FTS5 pattern the sqlite spike proved (`docs/spikes/sqlite/REPORT.md`
assertion 5), joined via `entities.id` — but that line was written
before comments existed as a searchable source: a comment has no row
in `entities` at all (ADR 0002's registry only covers principal
records — tickets/features/decisions/plans/documents), so `entities.id`
can't be FTS5's `content_rowid` for a schema that needs to index both.

## Decision

- **`search_documents` is a new table, not a view over `entities`.**
  Each row's natural key is `(source_kind, source_id)` —
  `('entity', entities.id)` for a ticket/feature/decision/plan/
  document, `('comment', comments.id)` for a comment — but
  `search_documents.id` is its own `AUTOINCREMENT` surrogate, giving
  FTS5's `content_rowid` a single dense id space to join against
  regardless of which source table a hit came from. This is the
  concrete fix for the "two different id spaces" problem ADR 0003's
  original consequence line didn't anticipate.
- **Denormalized, not resolved by a join at query time.** Each row
  also carries `entity_id` (the *owning* entity — for a comment row,
  its parent ticket's entity id, not the comment's own id), `kind`,
  `project_id`, `ref` (the owning entity's formatted reference), and
  `status`. A search hit is directly renderable from one row: no
  second lookup to resolve a ref or a project, the same reasoning
  `Backlink`'s `ref`+`comment_id` shape already applies to mention
  hits (`internal/service/mentions.go`). The cost is that every write
  path recomputes and re-upserts these columns instead of the database
  deriving them once — accepted the same way `content_items`' own
  denormalized `kind` column was (ADR 0017): SQLite can't express a
  computed uniqueness/lookup constraint across a join cheaply enough
  to prefer over writing the value.
- **`entity_id` (not `source_id`) is what a cascade delete keys off
  of.** Deleting a ticket must also drop its comments' search rows —
  `DELETE FROM search_documents WHERE source_kind='comment' AND
  entity_id = ?` does that in one indexed query, without a join back
  to `comments`. This matters specifically for `DeleteFeature`'s
  `cascade: true` path, which soft-deletes dependent tickets in its
  own loop rather than by calling `DeleteTicket` — a naive
  entity-only delete would leave a cascade-deleted ticket's comments
  searchable after the ticket itself is gone. (This is the opposite
  call from the Phase 5 plan's Step 7 notification design, which
  deliberately does *not* walk that cascade loop — search wants the
  cascade; notifications don't.) A ticket's comments are never
  themselves soft-deleted by a ticket delete (only excluded from the
  index), so `RestoreTicket` re-upserts every live comment's search row
  alongside the ticket's own.
- **External-content FTS5 + sync triggers**, exactly the pattern the
  spike validated: `search_fts` (title, body columns, `tokenize='porter
  unicode61'`) stores no data of its own; `AFTER INSERT/UPDATE/DELETE`
  triggers on `search_documents` keep it in lockstep, in the same
  transaction as every write. `store.RebuildSearchIndex`'s test uses
  FTS5's own `INSERT INTO search_fts(search_fts) VALUES('integrity-check')`
  command after a mixed insert/update/delete sequence — the trigger
  class of bug (a wrong `old.title`/`old.body` in the `AFTER UPDATE`
  trigger's delete-side `INSERT`) corrupts the index silently; it
  keeps answering queries, just with wrong results, so "search finds
  the row" alone would not have caught it.
- **User input is sanitized before it reaches `MATCH`, not passed
  through.** FTS5's `MATCH` argument is a query *language*, not a
  literal — a bare colon, an unbalanced quote, a lone boolean operator,
  or a trailing `*` is a query-time syntax error, not a zero-result
  search. `domain.SanitizeFTSQuery` wraps every whitespace-separated
  term in double quotes (doubling any embedded quote), turning any
  input into a sequence of phrase queries FTS5's default syntax ANDs
  together — syntactically safe for arbitrary user text. An
  empty-after-sanitization query is a `validation_failed` error
  (`field: "q"`), the same as any other required-field check.
- **Pagination is a capped offset, not the `(created_at, id)` tuple
  cursor every other list endpoint uses**
  (`docs/contracts/list-filters.md`). bm25 rank has no stable seekable
  key to compare against in a `WHERE (...) > (...)` clause the way a
  timestamp-and-id tuple does, so `Search` pages by literal `OFFSET`.
  Capped at 500 (`maxSearchOffset`) — product spec §5.12 treats search
  as "find it in the first page or two," not a browsable full listing,
  so paging past the cap is a validation error (`field: "cursor"`)
  rather than an unbounded `O(offset)` scan against the tail of a large
  result set.
- **`tickets admin search-reindex`** (`store.RebuildSearchIndex`)
  clears and rebuilds `search_documents` from scratch inside one
  transaction — atomicity (never a half-rebuilt index visible to
  concurrent readers) is judged worth more than lock duration for an
  offline admin command. It is the documented recovery path for
  anything the incremental `UpsertSearchDocument` call sites miss or
  get wrong.

## Consequences

- Assign/move/reorder ticket and feature mutations do **not** call
  `UpsertSearchDocument` — none of them change indexed text
  (title/description/status), so re-indexing on them would be a
  no-op write on every drag-and-drop.
- **Attachment file names and external link titles/URLs are not
  folded into the index**, incrementally or by
  `RebuildSearchIndex` — deferred out of this step's scope rather than
  implemented halfway. Folding them into `RebuildSearchIndex` only
  (and not the incremental path) was considered and rejected: search
  results would then differ depending on when a project was last
  reindexed, which is worse than not indexing them at all. `tickets
  admin search-reindex` is the eventual home for this if it's ever
  added, not a workaround for its absence today. Tracked for a
  decision at Phase 5 close-out — see `docs/phase5-close-out.md`.
- A content item's `file`/`path`/`url` representations have no
  Markdown body to index (`ContentItemFields.Body` is empty for all
  three) — their identifying value (file name, path, URL) is indexed
  in `body`'s place instead, or a file/path/url plan or document would
  only ever be findable by its title.
- `search_documents.status` is `NULL` for plans, documents, and
  comments (none have a status concept) — a `status` filter therefore
  never matches those kinds, which is correct behavior, not a bug to
  work around with an empty-string sentinel.
