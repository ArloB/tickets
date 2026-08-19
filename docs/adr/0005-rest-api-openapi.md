# 0005: JSON REST /api/v1 with checked-in OpenAPI

## Context

Product spec §9 requires a versioned HTTP API, a consistent error
envelope, cursor pagination, sparse/expanded representations, and a
checked-in OpenAPI document validated in CI. §7.1 requires the CLI and
MCP server to call the same application service rather than
duplicating validation, authorization, or audit logic.

## Decision

- All application endpoints live under `/api/v1`, described by
  `api/openapi.yaml`, linted in `task openapi` (Step 1's Taskfile) and
  eventually enforced in CI.
- `internal/httpapi` is a thin translation layer: HTTP request →
  `internal/service` call → HTTP response. It owns pagination
  cursors, the error envelope, `If-Match`/`Idempotency-Key` header
  handling, and sparse-field/expansion query parsing — nothing
  domain-specific.
- `internal/service` is the single authorization/validation/audit/
  transaction boundary; `internal/mcpsrv` calls the same functions
  `internal/httpapi` does, per ADR 0006.
- JSON field names are the stable public contract — internal Go field
  names may change without notice, but `api/openapi.yaml` schemas are
  the source of truth clients are written against.

## Consequences

- OpenAPI is written *before* the corresponding handler in Step 4/5,
  not generated from it after the fact, so the contract is a design
  decision rather than an accident of implementation.
- Contract tests (product spec §15) validate handler responses against
  the checked-in schema, catching drift automatically.
- Because `internal/service` is the only place business logic lives,
  the MCP tool surface (ADR 0006) can be added or changed later without
  touching authorization or validation code.
