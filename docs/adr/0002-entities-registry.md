# 0002: Shared entities registry, and the integer-rowid / UUID split

## Context

Product spec §8.3 calls for a shared `entities` registry (canonical
ID, project ID, entity kind, public reference, timestamps, soft-delete
state) so that comments, attachments, associations, notifications, and
audit events can reference one entity identity without unvalidated
polymorphic strings. §5.2 separately requires every entity's canonical
ID to be an opaque UUID, preferably UUIDv7.

The SQLite spike (`docs/spikes/sqlite/REPORT.md`, assertion 5) proved
that external-content FTS5 — the shape §6.3 needs so the search index
tracks source rows via triggers — requires `content_rowid` to name an
`INTEGER PRIMARY KEY` column, i.e. a SQLite rowid. A UUID (stored as
TEXT or BLOB) cannot serve as that rowid. Deciding this now avoids a
schema migration when Phase 5 adds search, since every table that will
ever need an FTS5-indexed sibling has to be built on the same key from
the start.

## Decision

`entities` carries two identity columns, with different visibility:

- `id INTEGER PRIMARY KEY` — an internal-only auto-increment surrogate
  key (the SQLite rowid). Used for: FTS5 `content_rowid` joins in
  Phase 5, and as the foreign key target from every other table
  (`comments.entity_id`, `attachments.entity_id`, `audit_events.entity_id`,
  etc.) for index and join efficiency. **Never serialized in any API,
  CLI, or MCP response.**
- `uuid BLOB(16) NOT NULL UNIQUE` — the canonical UUIDv7 identity from
  §5.2. This is what `internal/domain` parses/formats, what the public
  reference (`ABC-123`) resolves to, and the only entity identifier
  that ever appears outside the database.

Concrete per-kind tables (`projects`, `features`, `tickets`,
`decisions`, `content_items`, …) hold kind-specific fields and share
`entities.id` as their own primary key (a 1:1 extension pattern, not a
foreign key column), so a `tickets` row's identity *is*
`entities.id`.

## Consequences

- Every future FTS5-indexed table (tickets, decisions, plans,
  documents, comments — see §6.3) can use `content_rowid` against its
  own table's `id`, which is the shared `entities.id`, without a
  redesign in Phase 5.
- Application code must never leak `entities.id` — `internal/service`
  and `internal/httpapi` translate to/from `uuid` (and the formatted
  public reference) at every boundary. A lint/test in Phase 1 should
  assert no JSON response schema contains an `id` field typed as a
  bare integer.
- UUIDv7 remains sortable and safe to generate client-side later if
  needed, independent of the internal integer surrogate.
