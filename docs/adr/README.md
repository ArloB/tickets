# Architecture decision records

Numbered `NNNN-title.md`, each short: context, decision, consequences.
Written after the Phase 0 spikes (`docs/spikes/`) so decisions that
depend on spike results cite the spike report as evidence rather than
assumption.

Planned for Phase 0 (see `plan.md` §14 and the Phase 0 implementation
plan):

| ADR | Subject |
| --- | --- |
| 0001 | `Project → Feature → Ticket` hierarchy and the mandatory `General` feature |
| 0002 | Shared `entities` registry + concrete per-kind tables |
| 0003 | SQLite, WAL, FTS5, driver choice |
| 0004 | Human sessions vs. agent bearer tokens vs. anonymous read |
| 0005 | JSON REST `/api/v1` with checked-in OpenAPI |
| 0006 | MCP transports and the HTTP-only stdio bridge |
| 0007 | Attachment boundary and path-reference safety |
| 0008 | Optimistic concurrency and idempotency-key semantics |
| 0009 | Public reference allocation (per-project monotonic counters) |
| 0010 | Go version, module path, repo layout, embedded web assets |
