# 0009: Public reference allocation

## Context

Product spec §5.2 requires each entity kind within a project to get a
sequential human-facing reference (`ABC-123` for tickets, `ABC-F12` for
features, `ABC-D7` for decisions, `ABC-P4` for plans, `ABC-DOC9` for
documents) — five independent sequences per project, not one shared
counter. This is not decided anywhere in plan.md, but Phase 0's
vertical slice (Step 5) cannot create a ticket without it, and
verification gate 4 asserts the exact output (`ABC-1`, `ABC-2`,
`XYZ-1`) to catch a global-instead-of-per-project counter.

## Decision

A `reference_counters` table, not a column on `projects`:

```sql
CREATE TABLE reference_counters (
    project_id INTEGER NOT NULL REFERENCES entities(id),
    kind       TEXT    NOT NULL,  -- 'ticket' | 'feature' | 'decision' | 'plan' | 'document'
    next_seq   INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (project_id, kind)
);
```

A new kind needs a new row, not a schema migration. Allocation is one
statement inside the *same transaction* as the entity insert it names:

```sql
UPDATE reference_counters SET next_seq = next_seq + 1
WHERE project_id = ? AND kind = ?
RETURNING next_seq - 1;
```

(SQLite's `RETURNING` clause, available since 3.35 and present in the
3.51.x this project's driver ships — confirmed as part of Step 5's
implementation, not asserted from the Step 2 spike, which didn't
exercise `RETURNING`.) A project's counter rows are seeded to 1 for
each kind when the project — and its mandatory `General` feature, per
ADR 0001 — is created.

**Concurrency safety relies on SQLite's serialized-writer transaction
model** (ADR 0003), not row-level locking, which SQLite doesn't have:
only one write transaction commits at a time, so two concurrent
`ticket_create` calls cannot observe or increment the same `next_seq`
value.

**No artificial gaps:** because the counter increment lives in the
same transaction as the row it names, a rolled-back create (validation
failure, crash before commit) rolls back the increment too — the
number is never consumed. This is stricter than the "gap-tolerant"
framing used elsewhere in this project for *position* values (§5.6),
which is a distinct, unrelated counter. A gap here would only appear
if a future feature (e.g., pre-reserving a block of references)
deliberately introduces one — none does today.

## Consequences

- `internal/service`'s ticket-create path is: allocate reference →
  insert `entities` row → insert `tickets` row, all in one
  transaction, extending the same transactional discipline ADR 0002
  and ADR 0008 already require for entity creation.
- `internal/domain`'s reference formatter takes `(project key, kind,
  seq)` and produces the wire string; the parser is its inverse. Both
  are pure functions with no I/O, per the `internal/domain` package
  boundary (Step 1).
- Verification gate 4 (Phase 0 plan) is the executable spec for this
  ADR: `ABC-1`, `ABC-2` for two tickets in project `ABC`, `XYZ-1` for
  one in project `XYZ`.
