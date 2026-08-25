# MVP acceptance criteria coverage

Tracks `plan.md` §16's 17 acceptance criteria — the MVP's actual definition
of done, and the spine of Phase 6's exit criterion ("all MVP acceptance
criteria pass on both target platforms"). Each row names the test(s) that
prove the criterion today, and its status:

- **covered** — an automated test asserts this criterion end-to-end.
- **implemented, untested** — the behavior exists but nothing exercises it
  as a scenario; unit/integration tests for the underlying pieces may still
  exist without covering the criterion as stated.
- **not implemented** — no code path satisfies this yet.

This file is a Phase 6 deliverable (`plan.md`'s Phase 6 bullet list) and the
checklist Phase 6 Step 11 closes against — update it as each phase 6 step
lands, don't let it drift the way `api/openapi.yaml`'s stale description did.

| # | Criterion (§16) | Status | Test(s) | Notes |
| --- | --- | --- | --- | --- |
| 1 | A single executable starts a fresh server and embedded web UI on Linux and Windows. | **implemented, untested** | `task ci` (Windows), `task ci:linux` (WSL) build and run the full suite on both; no automated "cold start, fresh data dir, both platforms" scenario test exists. | Phase 6 Step 8 (platform testing) closes this — including non-ASCII/space paths per §15. |
| 2 | First-run setup creates an admin; anonymous users can read when enabled but every write is rejected without authentication. | **covered** | `cmd/tickets/setup_test.go`; `web/e2e/smoke.spec.ts` ("anonymous read-only mode: can browse but has no mutating controls, and the server rejects a forged mutation"); `internal/httpapi/auth_test.go`. | |
| 3 | Humans can create and edit all in-scope record types in the web UI. | **implemented, untested** | Per-type e2e coverage exists (`boards-and-bulk.spec.ts`, `content-item-representations.spec.ts`, decision/plan/document routes) but no single scenario walks every record type end-to-end. | Comments now exist on all six principal kinds (Phase 6 Step 2, service+HTTP+MCP+CLI+web all covered by unit/integration tests) — the remaining gap is purely "no single e2e scenario walks every record type," not a missing capability. |
| 4 | Creating a project-level ticket transparently uses its `General` feature, and the ticket can later move features. | **covered** | ADR 0001's gate tests; `internal/service/ticket.go`'s move-feature path and its tests. | |
| 5 | Priorities and manual positions produce the same deterministic order in API, CLI, MCP, and UI. | **implemented, untested** | `internal/store/tickets_list_test.go` (`TestPriorityQueueOrdersByRankNotText`), `internal/store/positions_test.go`, `cmd/tickets/exit_criterion_phase3_test.go` cover ordering per-layer; no test asserts API/CLI/MCP/UI return the *same* order for the *same* data in one run. | Worth a small cross-layer regression test in Phase 6 Step 8 or 11. |
| 6 | The issue register separates bug/security work and orders it by severity. | **covered** | `internal/store/tickets_list_test.go` (`TestIssueRegisterOrdersBySeverityThenPriority`). | |
| 7 | `#ABC-123` references create links and backlinks without being mistaken for dependencies; explicit dependencies support multiple tickets and reject cycles. | **covered** | `internal/domain/scan_test.go`, `internal/service/mentions_test.go` (verification gate 7); `internal/service/relationship_test.go` (`TestAddRelationshipDetectsBlocksCycle`, `TestAddRelationshipDetectsParentOfCycle`). | |
| 8 | Markdown, uploaded files, paths, and URLs can be attached or represented as specified, with the correct kind of version history. | **covered** | `internal/service/attachment_test.go`, `internal/service/content_item_representations_test.go`, `internal/service/content_item_test.go` (version/diff tests), `web/e2e/attachments.spec.ts`, `web/e2e/content-item-representations.spec.ts`. | |
| 9 | Two different agent tokens create separately attributed audit events, can be revoked independently, and never appear in logs or exports. | **covered** | `internal/httpapi/exit_criterion_test.go`; `internal/service/agent_test.go` (`TestAgentLifecycleEmitsActorAuditTrail`, `TestAgentTokenAuditEventNeverCarriesTokenValue`, both new in Phase 6 Step 1); `internal/httpapi/security_test.go` (`TestBearerTokenNeverAppearsInLogOutput`); `internal/backup.TestExportNeverContainsSecrets` (Phase 6 Step 4 — `agent_tokens`/`human_accounts` are never selected by `Export` at all, not merely redacted after the fact). | |
| 10 | Codex and Claude Code can use MCP for the representative ticket workflow; the same workflow is possible through CLI JSON. | **implemented, untested (this phase)** | `cmd/tickets/exit_criterion_phase3_test.go` proved this against Phase 3's tool surface. | Must be re-run manually against both hosts after Phase 6 Step 5 (`project_brief` added, `ticket_comment` description rewritten) — Phase 6 Step 11. |
| 11 | The web UI receives live change hints and shows assignment/mention notifications. | **covered** | `web/e2e/sse.spec.ts`, `web/e2e/notifications.spec.ts`. | |
| 12 | Full-text search returns compact, relevant results across all promised content types. | **covered** | `internal/service/search_test.go` (including `TestSearchFindsCommentsOnNonTicketEntities`, Phase 6 Step 2), `web/e2e/search.spec.ts`. | Re-verified after Step 2: a comment on a project and a comment on a plan are both indexed and findable, not just ticket comments. |
| 13 | A stale concurrent edit receives a conflict and neither version is silently lost. | **covered** | `internal/service/concurrency_test.go` (verification gate 9); `web/e2e/conflict-resolution.spec.ts`. | |
| 14 | A repeated idempotent mutation does not create duplicate tickets or comments. | **covered** | `internal/service/service_test.go` (`TestTicketIdempotentCreateReplay`, `TestIdempotentReplayReturnsFullRecordNotASnapshot`); `internal/httpapi/comments_test.go` (`TestIdempotentCommentReplayOverHTTP`); `internal/httpapi/server_test.go` (`TestIdempotentCreateReplayOverHTTP`). | |
| 15 | Performance targets are measured against the reference dataset and material regressions are documented or corrected. | **not implemented** | `task bench` covers 5 operations only (ticket get, project list, priority queue, issue register, ticket create); no FTS, HTTP-layer, activity-feed, concurrent-access, or attachment-streaming benchmark; no recorded results file. | Phase 6 Step 6. |
| 16 | Backup and restore preserve records, attachments, references, versions, audit history, and checksums; portable export/import is validated separately. | **covered** | `internal/backup` (Phase 6 Step 4): `TestBackupThenRestoreReproducesState`, `TestRestoreRefusesCorruptedChecksumAndLeavesDataDirUntouched`, `TestRestoreRemovesStaleWAL`, `TestRestoreRefusesWhileServerRunningUnlessForced` (backup/restore); `TestExportThenImportRoundTrip` (records, attachment bytes, references, and comment history preserved through export→import), `TestImportRefusesWithoutAttachmentsDirWhenBlobsAreReferenced`, `TestImportDetectsInvalidReference`, `TestImportDetectsCorruptedSeedActor`, `TestImportRefusesNonEmptyTarget` (export/import validated separately from backup/restore, as the criterion asks). | Step 11's drill is still the end-to-end, cross-platform confirmation of this — these are the unit/integration proofs it closes against. |
| 17 | The release documentation covers secure LAN sharing and clearly warns about anonymous reads, bearer tokens without TLS, path references, and lack of malware scanning. | **not implemented** | No user-facing docs exist beyond `docs/mcp-agent-guide.md` and the ADR/contract set. | Phase 6 Step 9. |

## Summary

- **Covered:** 11 / 17 (2, 4, 6, 7, 8, 9, 11, 12, 13, 14, 16)
- **Implemented, untested as a scenario:** 4 / 17 (1, 3, 5, 10)
- **Not implemented:** 2 / 17 (15, 17)

Every "not implemented" or "untested" row above has an owning Phase 6 step.
This file should read all-**covered** by the end of Phase 6 Step 11.

## Accepted for the MVP, reviewed and not changed (Phase 6 Step 1 audit)

Each of these was found during the Phase 6 deferred-items sweep, already
carries its own reasoning at the cited location, and is deliberately staying
as-is — recorded here so a future reader doesn't have to re-derive that the
gap was seen and accepted, not missed:

- **SSE has no `Last-Event-ID` replay.** `docs/adr/0020-sse-change-hints.md`'s
  Consequences: `EventSource`'s auto-reconnect plus each page's own
  refetch-on-mount is sufficient for §16 criterion 11 as written.
- **List cursors don't encode a filter fingerprint** — changing filters
  mid-pagination silently reuses cursor position.
  `docs/contracts/list-filters.md:74-78`.
- **Login-throttle `X-Forwarded-For` trust is unconfigured** — real work for
  a team/shared deployment behind a reverse proxy, not decided here.
  `internal/httpapi/auth_middleware.go:98-105`.
- **API clients are hand-written, not OpenAPI-codegenerated**, on both the
  Go and web sides. `internal/apiclient/client.go:4-11`,
  `web/src/api/client.ts:1-8`: revisit only if drift becomes an observed
  pain point.
- **`entity_associations` rejects any entity kind outside `domain.ValidAssociationKind`'s set** (tickets, features, decisions, plans, documents — not projects or comments), by design, per §5.7. Verified during this audit: `internal/service/association.go`'s `resolveAssociationEndpoint` already resolves all five kinds; ADR 0014's Consequences note describing decision/plan/document as unsupported was accurate for Phase 1 only and is now stale — Phase 5's content-item tables closed that gap without an ADR update. No code change needed; noting the stale ADR text here rather than leaving it to mislead a future reader.
