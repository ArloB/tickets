# Contracts

Each document here is backed by code in `internal/domain` and unit
tests, so the document and the implementation cannot silently diverge.

- [references.md](references.md) — public reference format per entity
  kind, project key rules, immutability, bare vs. `#`-prefixed parsing.
  Backed by `internal/domain/reference.go`.
- [enums.md](enums.md) — wire values for ticket type, workflow status,
  priority, severity, decision status, relationship types. Frozen here
  and reused by the API, CLI, MCP, and UI. Backed by
  `internal/domain/enums.go`.
- [errors.md](errors.md) — the error envelope and the machine-readable
  error code catalogue. Backed by `internal/httpapi` (Step 5).
- [representations.md](representations.md) — compact vs. detail entity
  shapes; documents the `fields`/`include` query parameters as a
  contract without implementing them yet (no consumer until Phase 3).
- [concurrency.md](concurrency.md) — optimistic-version and
  idempotency-key semantics. Backed by `internal/service` (Step 5).
- [cli.md](cli.md) — `cmd/tickets`' client-mode contract: config file/
  env/flag precedence, token handling, `fields`/`include` wiring,
  exit codes, `--no-color`/`NO_COLOR`, and `admin agent`/`admin token`'s
  local-store-only shape. Backed by `cmd/tickets` (Phase 3).
- [list-filters.md](list-filters.md) — ticket/feature list query-param
  filters (`status`, `type`, `severity`, `priority`, `feature_ref`,
  `assignee`, `creator`, `updated_since`): composition, resolution,
  and the cursor/filter interaction. Backed by `internal/store` and
  `internal/service` (Phase 4).
