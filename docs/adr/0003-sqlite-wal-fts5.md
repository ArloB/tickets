# 0003: SQLite, WAL, FTS5, and driver choice

## Context

Product spec §8.2 requires small native Linux/Windows executables and
picks SQLite in WAL mode with FTS5 as the storage engine, but flags a
pre-implementation spike to confirm the pure-Go driver actually
supports what's needed. That spike ran and is fully documented in
`docs/spikes/sqlite/REPORT.md`.

## Decision

Use `modernc.org/sqlite` (pure Go, CGO-free, currently v1.56.0,
SQLite 3.51.x) as the sole SQLite driver, accessed through
`database/sql`. No fallback driver is needed — all 7 spike assertions
passed, including FTS5, external-content tables with sync triggers
inside a transaction, WAL concurrency, and `CGO_ENABLED=0`
cross-compilation to both target platforms from one machine.

`internal/store` opens the database with:

- `PRAGMA journal_mode=WAL`
- `PRAGMA foreign_keys=ON`
- `busy_timeout` set **via the DSN** (`?_pragma=busy_timeout(5000)`),
  not as a separate `PRAGMA` call after `sql.Open`. The spike found
  this matters: `database/sql` pools connections, and a pragma applied
  as a follow-up statement can land on a different pooled connection
  than the one a later statement uses. The DSN form applies the pragma
  to every connection the pool opens.
- `_txlock=immediate`, also via the DSN — see ADR 0009's implementation
  note. The driver's default (`BEGIN DEFERRED`) takes no write lock
  until a transaction's first write, so two concurrent read-then-write
  transactions can both pass their reads and then race on the write;
  `_txlock=immediate` makes every read-write transaction take the lock
  up front so concurrent writers serialize through `busy_timeout`
  instead of failing with `SQLITE_BUSY`. Found by a targeted
  concurrency test in Step 5, not by the Step 2 spike (whose own
  concurrency assertion used a single pooled connection and autocommit
  writes, which doesn't exercise this failure mode).

## Consequences

- No CGO toolchain is required anywhere in the build or CI pipeline,
  on either platform — confirmed empirically, not assumed.
- `internal/store`'s `sql.Open` call is the one place the DSN pragma
  string is constructed; any other pragma that must hold for every
  pooled connection (not just the one active at open time) goes
  through the same DSN mechanism, not a post-open `Exec`.
- FTS5 schema in Phase 5 follows the external-content + trigger
  pattern proven in the spike, joined via `entities.id` per ADR 0002.
