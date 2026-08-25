# MVP acceptance criteria coverage

Tracks `plan.md` §16's 17 acceptance criteria — the MVP's actual definition
of done, and the spine of Phase 6's exit criterion ("all MVP acceptance
criteria pass on both target platforms"). Each row names the test(s) that
prove the criterion today, and its status:

- **covered** — an automated test asserts this criterion end-to-end.
- **covered (by review)** — the criterion is about documentation
  content, not code behavior; there is no test suite over prose, so
  the row's notes state what was checked by reading the file (row 17
  is currently the only one of these).
- **implemented, untested** — the behavior exists but nothing exercises it
  as a scenario; unit/integration tests for the underlying pieces may still
  exist without covering the criterion as stated.
- **not implemented** — no code path satisfies this yet.
- **not implemented (partial)** — most of the criterion is implemented
  and tested; a specific, named part of it has no code path at all
  (row 3 is currently the only one of these — see its notes for
  exactly what's missing and what isn't).

This file is a Phase 6 deliverable (`plan.md`'s Phase 6 bullet list) and the
checklist Phase 6 Step 11 closes against — update it as each phase 6 step
lands, don't let it drift the way `api/openapi.yaml`'s stale description did.

At Step 11's close-out, `task web:e2e` was run in full (not just cited
from earlier steps) — 31/31 specs pass. Across three full runs in this
session, three individual failures occurred and none reproduced: two
were `409 already_exists` collisions on `helpers.ts`'s random 3-digit
project-key generator (`E2E818`, `E2E679`) under parallel workers — a
pre-existing, narrow (900-value) key space, now widened to reduce the
odds; one was a `smoke.spec.ts` timeout under system load whose root
cause was not further diagnosed. All three passed cleanly when
re-run in isolation, and none is traceable to a Phase 6 change.

| # | Criterion (§16) | Status | Test(s) | Notes |
| --- | --- | --- | --- | --- |
| 1 | A single executable starts a fresh server and embedded web UI on Linux and Windows. | **covered** | Phase 6 Step 8: `cmd/tickets/coldstart_test.go` (`TestColdStartFreshDataDirWithUnusualPath`, `TestUpgradeOverExistingDataDirWithUnusualPath` — clean install, and a restart/reopen over an existing data directory, both with a path containing a space and a non-ASCII character per §15, timed against §11's 2s startup target; `internal/store.TestPreMigrationSnapshotIsUsableForRecovery` is the corresponding schema-upgrade-over-existing-data path); `internal/store.TestOpenUnusualPath` and `internal/blobstore.TestOpenUnusualPath` at the store/blobstore layers. `task ci`'s full gate (fmt, vet, lint, `go test ./...`, openapi lint, web:lint, web:test, web:build, `go build`) run and passed cleanly on **both** platforms for real during this step — Linux natively (this environment) and Windows natively (via the Windows Go/Task toolchain reachable from this WSL session at `/mnt/c`, against a copy of the tree on an actual NTFS path, not just WSL's UNC mount of the Linux filesystem — see the note below). | The Windows run caught and fixed a real bug: three backup/restore tests kept a live store handle open across `Restore`'s file swap, which POSIX rename silently tolerates but Windows refuses ("Access is denied") — fixed by closing the store first in each test, matching the documented "server must **not** be running" precondition real CLI usage already follows (a fresh `tickets admin restore` process never holds a prior store handle). See row 16. |
| 2 | First-run setup creates an admin; anonymous users can read when enabled but every write is rejected without authentication. | **covered** | `cmd/tickets/setup_test.go`; `web/e2e/smoke.spec.ts` ("anonymous read-only mode: can browse but has no mutating controls, and the server rejects a forged mutation"); `internal/httpapi/auth_test.go` (`TestEveryMutatingRouteRequiresAtLeastEditor`, `TestAnonymousReadCoversStep10Through14Routes`, `TestAnonymousReadCoversPhase4And5Routes` — new in Phase 6 Step 7, extends coverage to decisions/plans/documents/links/backlinks/activity/search/attachment bytes, and locks in that notifications/subscription status stay identity-gated). | `docs/security-model.md` (Phase 6 Step 7) is the threat-model writeup this and rows 8/9 draw from. |
| 3 | Humans can create and edit all in-scope record types in the web UI. | **not implemented (partial)** | `smoke.spec.ts`'s golden path creates a project, feature, and ticket through the UI; `boards-and-bulk.spec.ts` edits ticket/feature status through the board UI; `content-item-representations.spec.ts` creates and edits a plan and a document through the UI; `conflict-resolution.spec.ts` edits a ticket through the UI; `decisions.spec.ts` (new, Phase 6 Step 11) creates and edits a decision through the UI, closing the gap an earlier pass at this row found (no e2e spec had exercised the decision form at all — `search.spec.ts` only ever created one via a raw API POST). | **Real product gap found during this row's Phase 6 Step 11 audit, not just a test gap**: project **edit and archive are not implemented anywhere** — no `PATCH`/update route in `api/openapi.yaml`, no service method, no UI form — despite product spec §6.1 explicitly listing "Create, edit, archive, browse, and search projects" as a requirement. A project's title/description are fixed at creation with no way to change them, and there is no archive concept at all (soft-deletion exists for tickets/features/content items, not projects). Confirmed by grepping `internal/httpapi/server.go`'s route table and `internal/service/project.go` — nothing partial was missed. **Decision, deliberately not to build this now** (user confirmed when asked): release hardening is the wrong phase to add a new cross-layer feature (service + HTTP + OpenAPI + CLI + MCP + web UI, plus an optimistic-concurrency design decision) this late; tracked here as the known gap for a future phase rather than silently left undocumented. |
| 4 | Creating a project-level ticket transparently uses its `General` feature, and the ticket can later move features. | **covered** | ADR 0001's gate tests; `internal/service/ticket.go`'s move-feature path and its tests. | |
| 5 | Priorities and manual positions produce the same deterministic order in API, CLI, MCP, and UI. | **covered** | `internal/store/tickets_list_test.go` (`TestPriorityQueueOrdersByRankNotText`), `internal/store/positions_test.go` cover per-layer ordering; `cmd/tickets/exit_criterion_phase6_ordering_test.go` (`TestExitCriterionPhase6SameOrderAcrossLayers`, Phase 6 Step 11) is the cross-layer check this row was missing — three same-priority tickets, one moved with a manual position reorder, read back through the HTTP API, CLI `--json`, and a real MCP client's `tickets_list` tool call, asserting all three return the identical order. | UI is not independently re-verified: it renders `GET /projects/{key}/tickets` rows as returned (confirmed by grep — no client-side sort of a ticket list anywhere under `web/src`), so HTTP-order correctness is UI-order correctness by construction, not a fourth thing needing its own test. |
| 6 | The issue register separates bug/security work and orders it by severity. | **covered** | `internal/store/tickets_list_test.go` (`TestIssueRegisterOrdersBySeverityThenPriority`). | |
| 7 | `#ABC-123` references create links and backlinks without being mistaken for dependencies; explicit dependencies support multiple tickets and reject cycles. | **covered** | `internal/domain/scan_test.go`, `internal/service/mentions_test.go` (verification gate 7); `internal/service/relationship_test.go` (`TestAddRelationshipDetectsBlocksCycle`, `TestAddRelationshipDetectsParentOfCycle`). | |
| 8 | Markdown, uploaded files, paths, and URLs can be attached or represented as specified, with the correct kind of version history. | **covered** | `internal/service/attachment_test.go`, `internal/service/content_item_representations_test.go`, `internal/service/content_item_test.go` (version/diff tests), `web/e2e/attachments.spec.ts`, `web/e2e/content-item-representations.spec.ts`; security-focused backfill in Phase 6 Step 7: `TestAttachmentFilenameCannotInjectResponseHeaders`, `TestAttachmentPathReferenceNeverRead` and its content-item counterpart, `web/src/components/Markdown.test.tsx`'s XSS payload table, `web/e2e/csp.spec.ts`. | |
| 9 | Two different agent tokens create separately attributed audit events, can be revoked independently, and never appear in logs or exports. | **covered** | `internal/httpapi/exit_criterion_test.go`; `internal/service/agent_test.go` (`TestAgentLifecycleEmitsActorAuditTrail`, `TestAgentTokenAuditEventNeverCarriesTokenValue`, both new in Phase 6 Step 1); `internal/httpapi/security_test.go` (`TestBearerTokenNeverAppearsInLogOutput`); `internal/backup.TestExportNeverContainsSecrets` (Phase 6 Step 4, extended in Step 7 with independent session/CSRF-token/agent-token-hash sentinels, not just a password hash); `internal/mcpsrv.TestToolsOverRealStreamableHTTPRejectsRevokedToken` (Phase 6 Step 7 — revocation proven at the MCP transport layer too, not just HTTP). | |
| 10 | Codex and Claude Code can use MCP for the representative ticket workflow; the same workflow is possible through CLI JSON. | **implemented, untested (live two-host check)** | Protocol-level proof: `cmd/tickets/exit_criterion_phase3_test.go`'s `InProcessBackend_over_real_MCP_client` subtest drives the workflow through a genuine `mcp.Client` over real Streamable HTTP against the current tool surface (`project_brief` included). CLI JSON parity gap closed in Phase 6 Step 11: `tickets ticket create` (`cmd/tickets/ticket.go`'s `runTicketCreate`, tested by `TestTicketCreateJSON`/`TestTicketCreateRequiresTypeAndTitle`) — found missing during Step 9 documentation (every other principal entity already had a CLI `create`, MCP already had `ticket_create`), now added so CLI JSON can do the full workflow end to end, not just everything after ticket creation. | Deliberately **not** marked covered: §16's actual ask is that Codex *and* Claude Code, as live agents, can perform the workflow starting from `project_brief` — judging real agent behavior against the current tool descriptions (`project_brief` is new, `ticket_comment`'s description was rewritten in Step 5), which no Go test can substitute for. Not performed this step; see the note below this table for exactly what remains and how to run it. |
| 11 | The web UI receives live change hints and shows assignment/mention notifications. | **covered** | `web/e2e/sse.spec.ts`, `web/e2e/notifications.spec.ts`. | |
| 12 | Full-text search returns compact, relevant results across all promised content types. | **covered** | `internal/service/search_test.go` (including `TestSearchFindsCommentsOnNonTicketEntities`, Phase 6 Step 2), `web/e2e/search.spec.ts`. | Re-verified after Step 2: a comment on a project and a comment on a plan are both indexed and findable, not just ticket comments. |
| 13 | A stale concurrent edit receives a conflict and neither version is silently lost. | **covered** | `internal/service/concurrency_test.go` (verification gate 9); `web/e2e/conflict-resolution.spec.ts`. | |
| 14 | A repeated idempotent mutation does not create duplicate tickets or comments. | **covered** | `internal/service/service_test.go` (`TestTicketIdempotentCreateReplay`, `TestIdempotentReplayReturnsFullRecordNotASnapshot`); `internal/httpapi/comments_test.go` (`TestIdempotentCommentReplayOverHTTP`); `internal/httpapi/server_test.go` (`TestIdempotentCreateReplayOverHTTP`). | |
| 15 | Performance targets are measured against the reference dataset and material regressions are documented or corrected. | **covered** | `internal/store/bench_test.go`, `internal/service/bench_test.go`, `internal/httpapi/bench_test.go`, plus the pre-existing `internal/service/concurrency_test.go` (13 benchmarks total under `task bench`: indexed detail/first-page list at store and HTTP layers, selective and pathological FTS search, activity feed, an ordinary mutation, concurrent readers and concurrent writers, attachment streaming); `docs/benchmarks.md` records results from an actual `task bench` run against the full `fixtures.Full` reference dataset (10,000 decisions/plans/documents added to the generator in Phase 6 Step 6). | One target is deliberately left unmet and documented rather than optimized: a full-text search query matching nearly the entire corpus (bm25 ranking cost scales with match-set size, not page size) — the representative selective-query case beats the target by ~9x. See `docs/benchmarks.md`'s "left unmet" section. |
| 16 | Backup and restore preserve records, attachments, references, versions, audit history, and checksums; portable export/import is validated separately. | **covered** | `internal/backup` (Phase 6 Step 4): `TestBackupThenRestoreReproducesState`, `TestRestoreRefusesCorruptedChecksumAndLeavesDataDirUntouched`, `TestRestoreRemovesStaleWAL`, `TestRestoreRefusesWhileServerRunningUnlessForced` (backup/restore); `TestExportThenImportRoundTrip` (records, attachment bytes, references, and comment history preserved through export→import), `TestImportRefusesWithoutAttachmentsDirWhenBlobsAreReferenced`, `TestImportDetectsInvalidReference`, `TestImportDetectsCorruptedSeedActor`, `TestImportRefusesNonEmptyTarget` (export/import validated separately from backup/restore, as the criterion asks). Phase 6 Step 8's recovery drills: `TestOnlineBackupDuringConcurrentWrites` (online backup taken while a separate goroutine keeps writing, `PRAGMA integrity_check` on the result), `internal/store.TestPreMigrationSnapshotIsUsableForRecovery` (the Step 3 pre-migration snapshot copied to a fresh directory and opened, not just inspected in place), `cmd/tickets.TestAdminSearchReindex` (the FTS rebuild drill at the actual CLI layer, not just the pre-existing service-layer `TestSearchRebuildIndexRepopulatesFromScratch`). | Phase 6 Step 11's cross-entity exit-criterion drill (`cmd/tickets/exit_criterion_phase6_test.go`, `TestExitCriterionPhase6BackupRestoreDrill`) is the end-to-end confirmation this row's "records, attachments, references, versions, audit history, and checksums" wording asks for in one run: seeds a ticket (with a version-producing status change, and a description carrying a "#ABC-D1"-style derived mention so "references" means the actual backlink graph criterion 7 is about, not just the external link tested alongside it), a decision (with a version-producing update), an attachment, an external link, and a comment (an audit event); backs up; mutates the live data directory in a way that must not survive; restores; runs the same integrity check `tickets admin integrity` performs for the checksum assertion; and compares every one of those against its pre-backup state. All of Step 8's new/modified tests were also run and passed on real Windows and real Linux, not just Linux (see row 1) — and this drill itself was re-verified on real Windows too (Phase 6 Step 11): the Step 8 finding was specifically about a backup/restore test keeping a store handle open across `Restore`'s file swap, so this new backup/restore test was a real candidate to repeat that bug, and `go test -count=1 ./cmd/tickets/...` on a native NTFS-path copy confirmed it does not. |
| 17 | The release documentation covers secure LAN sharing and clearly warns about anonymous reads, bearer tokens without TLS, path references, and lack of malware scanning. | **covered (by review)** | Phase 6 Step 9: `README.md`, `docs/install.md`, `docs/admin.md`, `docs/cli.md`, `docs/api.md`, `docs/troubleshooting.md` (new); `docs/backup-recovery.md` (pre-existing) and `docs/security-model.md` (Phase 6 Step 7) already carried the full threat-model detail this criterion asks for. | This criterion is documentation content, not code behavior, so "covered" here means the required warnings are present and reviewed, not asserted by an automated test — there is no test suite over prose. Anonymous reads: `README.md`'s quickstart note, `docs/admin.md`'s config table, `docs/security-model.md`'s "Anonymous access" section. Bearer tokens without TLS: `docs/install.md`'s "Platform notes" TLS callout, `docs/troubleshooting.md`'s "Networking and security warnings" section, `docs/security-model.md`'s auth table. Path references: `docs/cli.md`'s plan/document representation note, `docs/security-model.md`'s "Path and URL references" section. No malware scanning: `docs/security-model.md`'s "Out of scope" section. |

### Row 10's two-host MCP check — status as of Phase 6 Step 11

Not performed as a live agent session during this step, and that is
recorded honestly here rather than assumed complete. What Step 11
*did* verify, automatically and for real: `cmd/tickets/exit_criterion_phase3_test.go`'s
`InProcessBackend_over_real_MCP_client` subtest drives the workflow
through a genuine `mcp.Client` speaking the real Streamable HTTP
protocol against the live tool surface (`project_brief` included,
since it's now registered) — this proves the protocol and tool
schemas are correct. It does **not** prove an LLM reading the current
tool descriptions actually picks `project_brief` first and completes
the workflow without extra discovery calls, which is what §16
criterion 10 and this row are actually asking for; no Go test can
measure that.

This agent session (Claude Code, running in this repo) has no way to
add a new MCP server connection to itself mid-session — doing so
requires `claude mcp add` plus a session restart, which is disruptive
and wasn't taken unilaterally. To actually close this out:

1. Build and run the server: `task build && ./bin/tickets server` (or
   `tickets.exe` on Windows), then `tickets setup` and seed a project.
2. Issue an agent bearer token: `tickets admin agent create --name
   drill --as <you>` then `tickets admin token create drill --as <you>`.
3. Create a ticket and assign it to that agent (`tickets ticket create
   --project ABC --type task --title "Fix the drill" --priority high`,
   then `tickets ticket assign <ref> --assignee agent:drill --if-version 1`)
   — `tickets_list` has no assignee filter (Phase 3's own finding, see
   `cmd/tickets/exit_criterion_phase3_test.go`'s doc comment), so
   "find assigned work" only actually works if a ticket is already
   assigned to the agent before it looks.
4. Register `tickets mcp --url http://127.0.0.1:8080/api/v1 --token
   <token>` as an MCP server in both a Claude Code session and a Codex
   session (`claude mcp add` / Codex's own MCP config).
5. In each, ask the agent to perform §16's representative workflow
   (find assigned work, read linked context, start ticket, comment,
   create decision, complete ticket) starting from `project_brief`,
   and confirm it completes without the agent needing extra discovery
   calls or getting confused by a tool description.

## Summary

- **Covered:** 14 / 17 (1, 2, 4, 5, 6, 7, 8, 9, 11, 12, 13, 14, 15, 16)
- **Covered (by review):** 1 / 17 (17)
- **Implemented, untested as a scenario:** 1 / 17 (10)
- **Not implemented:** 1 / 17 (3 — partial: everything except project edit/archive is implemented and now e2e-tested)

Two rows stay open, both deliberately rather than by oversight: row 10
asks whether a live LLM agent, not a Go test, picks the right tool
from its description — no code change can close that, only the manual
two-host run described above. Row 3 is a genuine, narrow product gap
found during this step's audit (project edit/archive were never
built, against product spec §6.1), not a test gap — deliberately left
unbuilt this phase per an explicit decision, not silently accepted.
Every other row this file can close by code or by review now does.

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
- **`api/openapi.yaml`'s `ActivityEventType` enum drifted from `internal/service/activity.go`'s `activityEventTypes` allowlist** (missing `attachment_added`/`attachment_replaced`/`attachment_removed`), found incidentally in Phase 6 Step 7 via OpenAPI response-schema validation in a new test, and fixed. The guard this note originally flagged as missing — a test asserting the two sets stay identical — was added at Phase 6 Step 11's close-out: `internal/service.TestActivityEventTypesMatchOpenAPIEnum`. No longer an open item; kept here as the record of when and why it was found.
