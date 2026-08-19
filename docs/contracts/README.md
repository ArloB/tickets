# Contracts

Each document here is backed by code in `internal/domain` and unit
tests, so the document and the implementation cannot silently diverge.

- **references.md** — public reference format per entity kind, project
  key rules, immutability, bare vs. `#`-prefixed parsing.
- **enums.md** — wire values for ticket type, workflow status,
  priority, severity, decision status, relationship types. Frozen here
  and reused by the API, CLI, MCP, and UI.
- **errors.md** — the error envelope and the machine-readable error
  code catalogue.
- **representations.md** — compact vs. detail entity shapes and the
  `fields`/`include` query parameters.
- **concurrency.md** — optimistic-version and idempotency-key
  semantics.
