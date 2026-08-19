# Architecture decision records

Numbered `NNNN-title.md`, each short: context, decision, consequences.
Written after the Phase 0 spikes (`docs/spikes/`) so decisions that
depend on spike results cite the spike report as evidence rather than
assumption.

| ADR | Subject |
| --- | --- |
| [0001](0001-hierarchy-and-general-feature.md) | `Project → Feature → Ticket` hierarchy and the mandatory `General` feature |
| [0002](0002-entities-registry.md) | Shared `entities` registry + the integer-rowid / UUID split |
| [0003](0003-sqlite-wal-fts5.md) | SQLite, WAL, FTS5, driver choice — cites the SQLite spike |
| [0004](0004-identity-and-auth.md) | Human sessions vs. agent bearer tokens vs. anonymous read |
| [0005](0005-rest-api-openapi.md) | JSON REST `/api/v1` with checked-in OpenAPI |
| [0006](0006-mcp-transports.md) | MCP transports and the HTTP-backed stdio bridge — cites the MCP spike |
| [0007](0007-attachment-boundary.md) | Attachment boundary and path-reference safety |
| [0008](0008-concurrency-idempotency.md) | Optimistic concurrency and idempotency-key semantics |
| [0009](0009-reference-allocation.md) | Public reference allocation (per-project monotonic counters) |
| [0010](0010-repo-layout-and-toolchain.md) | Go version, module path, repo layout, embedded web assets |
