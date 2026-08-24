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
| [0011](0011-position-and-rank-ordering.md) | Position allocation (gap-spaced, renumber-on-exhaustion) and rank-based priority/severity ordering |
| [0012](0012-actors-and-audit-attribution.md) | Actors outside the entity registry, seeded `system`/`local`, uniform audit attribution |
| [0013](0013-soft-deletion-semantics.md) | Soft-deletion: block-by-default, explicit cascade, `General` undeletable, restore-refuses-orphan |
| [0014](0014-relationships.md) | Relationship storage, inverse canonicalization, cycle detection, the `duplicate_of` correction |
| [0015](0015-derived-mentions.md) | Derived mentions: scanner scope, code-fence exclusion, delete-and-reinsert |
| [0016](0016-multi-project-scoping-rejection.md) | Multi-project scoping: rejects scoped bearer tokens, adopts a client-side `--project` default |
| [0017](0017-content-items.md) | Content items: shared plan/document representation model, immutable representation, MCP `record_*` kind discriminator |
| [0018](0018-unified-search-index.md) | Unified search index: synthetic `search_documents` rowid over entities + comments, capped offset pagination, FTS5 query sanitization |
| [0019](0019-subscriptions-and-notifications.md) | Subscriptions, notifications, and `@actor` mentions: append-only notification log, explicit emission at each mutation site, no self-notification |
