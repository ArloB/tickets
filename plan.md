# Tickets: product requirements and implementation plan

Status: proposed MVP specification  
Target users: an individual developer first, with small trusted-team use supported without changing the core architecture

## 1. Product summary

Tickets is a lightweight, self-hosted planning and issue-tracking system designed equally for humans and coding agents. Humans use a full read/write web UI; agents use a compact Model Context Protocol (MCP) interface or a scriptable CLI. All clients operate on the same central server and see the same projects, features, tickets, decisions, plans, documentation, files, comments, and history.

The intended core workflow is:

1. A human creates a project and describes its goals in the web UI.
2. The human or an agent breaks the project into features and tickets.
3. A human can point an agent at a stable reference such as `ABC-123`.
4. The agent reads only the context it needs, performs the work, and records progress or decisions.
5. Humans follow and edit the same work through the backlog, board, issue register, priority queue, and activity views.

This is not merely an agent activity viewer. The web UI and agent interfaces are peers over one API, with the same capabilities subject to authentication.

## 2. Product goals

### 2.1 Goals

- Keep project planning understandable when several humans and agents contribute.
- Make it fast for a human to create, organize, edit, and review work.
- Give agents stable, explicit, low-token interfaces instead of requiring them to interpret an entire UI or large document.
- Preserve who changed what and why through attribution, comments, decisions, and an immutable audit trail.
- Run as a small local executable on Linux and Windows with little administration.
- Support a personal installation while retaining a simple path to a trusted team sharing one server.
- Make all important data exportable and recoverable.

### 2.2 Success criteria

The MVP is successful when:

- A new user can start the server, create the first account, create a project, and add a feature and ticket without reading deployment documentation.
- A user can give an authenticated agent a ticket reference and the agent can retrieve it, inspect related context, update it, and comment through MCP.
- The same changes appear promptly in an open web UI and are attributed to the agent.
- A project containing at least tens of thousands of records remains responsive on an ordinary developer workstation.
- The installation can be backed up, restored, and exported without proprietary tooling.

## 3. Scope

### 3.1 MVP scope

- Projects, features, tickets, decisions, plans, and documents.
- Bug and security issue tickets, including an issue register ordered by severity.
- Comments, `@mentions`, cross-references, typed relationships, and attachments.
- Fixed initial workflows and priorities with manual ordering.
- Human and agent identities with attribution.
- Anonymous read-only access when enabled, password-authenticated human editing, and token-authenticated agent editing.
- Immutable audit history and visible content version history.
- Full-text search.
- In-app notifications and real-time web updates.
- A versioned HTTP API, MCP server, and non-interactive CLI.
- A read/write web UI, including a Markdown editor and preview.
- A single-server Linux and Windows release using SQLite and local attachment storage.
- JSON export/import and consistent backups.

### 3.2 Explicit non-goals for the MVP

- Peer-to-peer or distributed database synchronization.
- General offline editing or automatic conflict resolution.
- Multi-tenant SaaS isolation, public registration, or enterprise role-based access control.
- Custom workflows, custom fields, or per-project priority schemes.
- Email, push, Slack, or other external notifications.
- Dedicated GitHub, GitLab, source-control, calendar, or chat integrations. These can be represented as ordinary external links.
- Native mobile or desktop applications.
- Time tracking, sprints, estimates, billing, or analytics dashboards.
- Hard deletion through ordinary user workflows. Records are archived or soft-deleted.
- Deleting or archiving a project's last (`General`) feature — see ADR 0001.
- Docker as the primary distribution method. It may be added after the executable release is stable.

## 4. Users, identity, and access

### 4.1 Actor model

Every mutation is attributed to a persistent actor:

- **Human:** signs in with a username and password and uses a session cookie.
- **Agent:** has a name, optional description, owning human, and one or more revocable API tokens.
- **System:** records migrations, imports, automated maintenance, and other server-owned events.

An agent token is necessary because attribution cannot be trustworthy if all agents share an anonymous write identity. Tokens are displayed once when created and only a secure hash is stored. An authenticated human creates and revokes agent identities and their tokens in the web UI or CLI.

### 4.2 Initial permissions

The content permission model deliberately has only two levels:

- **Viewer:** may read non-deleted content and download permitted uploaded attachments. Anonymous requests have this level when anonymous reading is enabled.
- **Editor:** may create and change project content. Authenticated humans and agent tokens have this level.

The first human account also has an operational `admin` flag for managing accounts, agent tokens, server settings, imports, and restores. This does not introduce per-project content permissions in the MVP.

Anonymous read access is configurable and enabled for loopback-only personal use by default. The server must prominently warn before anonymous access is exposed on a non-loopback address. Installations containing sensitive material should disable anonymous access.

## 5. Information model

### 5.1 Hierarchy

The canonical hierarchy is:

```text
Project
└── Feature
    └── Ticket
```

Every ticket belongs to exactly one feature. Each new project automatically receives a `General` feature. Creating a ticket “directly in a project” places it in `General`, so small tasks remain convenient without creating a nullable or ambiguous hierarchy. A ticket can later be moved to a different feature in the same project.

Cross-feature and cross-project context is represented through references and typed relationships rather than multiple ownership. Moving a feature or ticket between projects is not supported initially; it can be copied or recreated with a `supersedes` relationship.

### 5.2 Stable identities and references

Every entity has an opaque canonical ID, preferably UUIDv7, and a human-facing immutable reference. A project has a short uppercase key chosen at creation, such as `ABC`.

Suggested reference forms are:

| Entity | Example |
| --- | --- |
| Ticket | `ABC-123` |
| Feature | `ABC-F12` |
| Decision | `ABC-D7` |
| Plan | `ABC-P4` |
| Document | `ABC-DOC9` |

Markdown fields recognize references written plainly or prefixed with `#`, for example `#ABC-123`. Inside project-scoped comments, `#123` may resolve to that project's ticket. The rendered web UI makes valid references navigable.

Mentioning an entity creates a derived `mentions` edge for backlinks and discovery, but does not imply scheduling or dependency semantics. Explicit relationships remain separate.

### 5.3 Project

A project is the highest-level container and long-term goal. It contains:

- Key, title, Markdown description, and lifecycle state (`active` or `archived`).
- Creator and creation/update timestamps.
- Features, tickets, decisions, plans, documents, comments, links, and attachments.
- A generated `General` feature.
- An activity stream and project-scoped search.

### 5.4 Feature

A feature is a short- or medium-term outcome containing tickets. It has:

- Title and Markdown description.
- Workflow status.
- Priority and numeric position within that priority.
- Creator, timestamps, comments, links, and attachments.
- Its tickets and progress summary.

Features use the same initial workflow as tickets so boards and aggregate progress remain understandable. Feature progress is calculated from its ticket states; it does not automatically change feature status.

### 5.5 Ticket

A ticket is the base unit of actionable work. It has:

- Type: `task`, `bug`, `security`, or `chore`.
- Title and Markdown description.
- Required project and feature.
- Status, priority, and numeric position within that priority.
- Creator and optional human or agent assignee.
- Optional severity for `bug` and `security` tickets: `critical`, `high`, `medium`, or `low`.
- Comments, explicit relationships, derived mentions, external links, and attachments.
- Creation/update timestamps, version number, and audit history.

Bug and security tickets form the issue register rather than using a separate issue entity. The issue view sorts by severity, then priority, manual position, and age. Security tickets do not receive special secrecy in the MVP; deployment access rules apply to the whole server.

### 5.6 Workflow and priority

The fixed initial workflow is:

```text
backlog -> ready -> in_progress -> blocked -> review -> done
                                                   \-> cancelled
```

The UI permits deliberate transitions between any states because personal workflows are often non-linear, but records every transition. Clients should warn, rather than fail, when reopening completed work or skipping expected stages.

Priority values, highest first, are:

1. `critical`
2. `high`
3. `medium`
4. `low`

Each feature or ticket also has an integer `position`. Default positions are allocated with gaps so most drag-and-drop changes update a single record; the server renumbers a priority group transactionally when needed. The priority queue sorts by priority, position, then creation time.

### 5.7 Ticket relationships

Tickets support directed and undirected typed relationships:

- `parent_of` / `child_of`
- `blocks` / `blocked_by`
- `related_to`
- `duplicate_of`
- `supersedes` / `superseded_by`

The service validates inverse relationships and prevents self-links. Cycles in `blocks` and `parent_of` relationships are rejected. A ticket may have multiple dependencies. Relationships are visible from both ends and are distinct from Markdown references.

Decisions, plans, documents, features, and tickets can also have looser `associated_with` links where a dependency relationship would not make sense.

### 5.8 Decisions

A decision is a first-class project record containing:

- Title, Markdown context, decision, rationale, and consequences.
- Status: `proposed`, `accepted`, `rejected`, or `superseded`.
- Creator, timestamps, comments, links, attachments, and associated entities.
- An optional link to the decision that supersedes it.

Accepted decisions remain editable only by creating a new version, and every version remains visible.

### 5.9 Plans and documents

Plans and documents are first-class project records, not ticket types.

- A **plan** describes intended work and may be associated with a whole project, one or more features, or tickets.
- A **document** stores supporting information and may have the same associations.

Each can be represented as:

- Versioned Markdown stored in the database.
- A versioned uploaded file managed by the server.
- A referenced server-side filesystem path.
- An external URL.

For stored Markdown, each edit saves a full snapshot and the UI computes a line-level diff between versions. Full snapshots simplify recovery and are acceptable at the intended scale. For uploaded files, each replacement creates an immutable file version recording uploader, time, size, media type, and checksum; binary line diffs are not attempted. For path and URL records, changes to the referenced value and metadata are versioned.

### 5.10 Comments and activity

Projects, features, tickets, decisions, plans, and documents can receive Markdown comments. Comments support entity references and `@actor` mentions.

- Comment edits create versions and remain visible in the audit trail.
- Comment deletion is a soft-delete with a visible tombstone.
- Field changes are audit events, not synthetic comments.
- The activity feed combines comments with selected audit events and can be filtered by actor, entity type, and event type.

### 5.11 Links and attachments

Any principal entity or comment can have named external links and file attachments.

Attachments can be:

- Uploaded into server-managed storage.
- A server-side filesystem path reference.
- An external URL.

Uploaded content is stored outside SQLite in a managed data directory, named by content hash, and referenced from the database. Identical content may be deduplicated. The default upload limit is 25 MiB per version and is configurable.

A path attachment is a reference only. The server must not automatically serve or read arbitrary paths through the web API. This avoids turning a path reference into unintended filesystem access. A future explicit import action may copy an allowed path into managed storage.

### 5.12 History and deletion

Every create, update, state transition, reorder, relationship change, assignment, token operation, import, and archive action emits an append-only audit event containing:

- Actor, timestamp, request/correlation ID, entity, event type, and changed fields.
- Safe before/after values or a structured patch where appropriate.
- No password, session secret, token value, or sensitive request header.

User-facing records are archived or soft-deleted. Audit records and referenced content versions are not changed by ordinary application operations. Permanent purge is an explicit administrative maintenance operation outside the initial web UI.

## 6. Functional requirements

### 6.1 Project and work management

Users must be able to:

- Create, edit, archive, browse, and search projects. **Status as of Phase 6 Step 11: create/browse/search are implemented; edit and archive are not** — no update route, service method, or UI form exists for a project's own fields. See `docs/mvp-acceptance.md` row 3.
- Create, edit, prioritize, reorder, and change the state of features and tickets.
- Create a ticket from a project without manually selecting a feature; the server uses `General`.
- Move a ticket between features within its project.
- Assign tickets to persistent human or agent actors.
- Add and remove relationships, references, links, and attachments.
- View backlinks generated from Markdown references.
- Filter by status, type, severity, priority, feature, assignee, creator, and update time.

### 6.2 Knowledge and decision management

Users must be able to:

- Create and version decisions, plans, and documents.
- Associate them with projects, features, and tickets.
- Compare Markdown versions line by line and inspect uploaded-file metadata history.
- Browse proposed, accepted, rejected, and superseded decisions.
- Link a superseding decision without destroying the historical record.

### 6.3 Search

SQLite FTS5 provides full-text search over:

- Titles and Markdown bodies.
- Ticket descriptions and comments.
- Decision fields.
- Stored Markdown plans and documents.
- Attachment names and link metadata, but not arbitrary binary file contents in the MVP.

Search supports project scope, entity type, status, and other structured filters. Results return compact snippets and stable references, with cursor-based pagination. Search index maintenance occurs transactionally with source changes where practical and can be rebuilt by an administrative command.

### 6.4 Notifications and live updates

The MVP provides in-app notifications for:

- Assignment to a ticket.
- `@mentions` in comments or Markdown bodies.
- Replies to or comments on subscribed work.
- Meaningful changes to subscribed tickets, features, or decisions.

Creating or commenting on an entity subscribes the actor by default; users can unsubscribe. Notifications have read/unread state. External delivery and complex notification preferences are deferred.

The web client receives change hints through Server-Sent Events (SSE). It refetches affected records through the normal API, keeping the real-time protocol simple and making the HTTP API the source of truth.

### 6.5 Web UI

The web UI is responsive and read/write. Initial views are:

- First-run setup and sign-in.
- Project list and project overview.
- Backlog/list view with filters and bulk selection.
- Ticket and feature kanban boards.
- Priority queue with manual reordering.
- Issue register/board ordered by severity.
- Ticket and feature detail views.
- Decision register and detail/version views.
- Plan and document library, editor, preview, and version comparison.
- Project activity feed.
- Notification inbox.
- Global and project-scoped search.
- Actor and agent-token administration.

Markdown input uses a simple split edit/preview experience, supports keyboard shortcuts, and sanitizes rendered HTML. Entity references and mentions autocomplete where useful. Pages should favor information density, keyboard navigation, and fast list interactions over decorative UI.

Saved filters, labels, and fully customizable boards are deferred unless implementation proves trivial after the core views are complete.

## 7. Agent and automation interfaces

### 7.1 Interface decision

MCP is the primary agent integration because it is an open protocol supported by both Codex and Claude-family tooling, among other clients. A thin CLI remains part of the MVP because it is universally scriptable, easy to test, and useful when an agent host cannot connect to MCP.

Both interfaces call the same versioned application service/API. Neither duplicates authorization, validation, audit, or relationship logic.

The server supports:

- Streamable HTTP MCP at a configurable endpoint for clients connecting to the running server.
- A `tickets mcp` STDIO bridge for hosts that prefer launching a local process. The bridge connects to the configured Tickets HTTP API rather than opening the database directly.
- Bearer-token authentication for writes and optional authenticated reads.

Small optional instruction packages for Codex and Claude may document recommended workflows, but they contain no application logic. The MCP server's `instructions` field carries the essential cross-tool guidance.

### 7.2 MCP design

The exact schemas are contract-tested before implementation, but the initial tool surface should remain small and task-oriented:

- `projects_list`, `project_get`, and `project_create` — `project_create` shipped in Phase 3 beyond this table's original list, closing a CLI/MCP parity gap found in live use
- `search` (Phase 5 Step 6 — real full-text search; shipped under this name, not the placeholder `work_search` this table originally used; Phase 3's tools relied on list filters instead)
- `tickets_list`, `ticket_get`, `ticket_create`, and `ticket_update`
- `ticket_comment` and `ticket_link` — `ticket_comment` is ref-agnostic as of Phase 6 Step 2 (any commentable entity, not tickets only)
- `feature_get`, `features_list`, `feature_create`, and `feature_update` — `features_list` shipped in Phase 3 beyond this table's original list
- `ticket_relationships` and `ticket_associations` — read-side companions to `ticket_link`, shipped in Phase 3 beyond this table's original list
- `record_get`, `record_create`, and `record_update` — Phase 3 scoped this to decisions only (a minimal slice: title/context/decision/rationale/status, no versioning/supersession); plans and documents joined in Phase 5
- `notifications_list` and `notifications_mark_read` (Phase 5 Step 7)
- `project_brief` (Phase 6) — a single aggregation read for orientation; see §14's Phase 6 bullet

Tool responses default to compact summaries. List and search calls omit full Markdown bodies, comments, history, and attachment contents unless explicitly requested. Detail calls accept include fields, and all collections are paginated. Writes return the changed entity's stable reference, version, essential fields, and warnings rather than echoing an entire expanded record.

Tools expose actionable validation errors, current versions after conflicts, and stable machine-readable error codes. Tool descriptions explain when a reference is merely contextual and when an explicit relationship is required.

### 7.3 CLI design

The `tickets` executable contains server, administrative, MCP bridge, and client commands. Representative commands are:

```text
tickets server
tickets setup
tickets project list
tickets project create --key ABC --title "Example"
tickets feature create --project ABC --title "First feature"
tickets ticket create --project ABC --title "Fix the parser" --type bug
tickets ticket get ABC-123
tickets ticket update ABC-123 --status in_progress
tickets comment add ABC-123 --body-file -
tickets search "parser failure" --project ABC
tickets agent create --name codex
tickets export --output backup.json
tickets mcp
```

CLI requirements:

- Non-interactive by default; prompts are opt-in and never appear when stdin is not a terminal.
- Human-readable tables by default and stable JSON with `--json`.
- `--fields`, `--include`, `--limit`, and cursor options to control output size.
- Markdown bodies accepted as a flag, from a file, or from stdin.
- Meaningful exit codes and machine-readable error objects.
- Configuration through flags, environment variables, and an OS-appropriate config file, with that precedence documented.
- Tokens read from protected config, an environment variable, or stdin; never accepted in a way that encourages inclusion in shell history.
- No terminal color when output is redirected, plus `--no-color` and `NO_COLOR` support.

### 7.4 Multi-project scoping for agents

A server can host multiple, unrelated projects (a `tickets` project and a separate web-server project, for example). An agent working on one must not read or write the other by accident, and needs some way to know which project it's in without being told every time.

Resolved by ADR 0016: scoped bearer tokens were rejected as the wrong tool — they solve a *context* problem (which project is this agent working in right now) with an *access-control* mechanism, adding a per-project authorization dimension nothing else in this codebase's flat viewer/editor model has, for the common single-user/personal-install case this product optimizes for (§2). Instead, the `tickets mcp` stdio bridge takes a `--project`/`TICKETS_PROJECT` default, filled in client-side whenever an outgoing tool call omits a project key (`internal/mcpsrv/httpbackend.go`'s `HTTPBackend.DefaultProject`, `cmd/tickets/mcp.go`) — pure client-side convenience the server never sees, exactly like a shell's `$PWD` biasing a relative path. Tokens stay server-wide, with no project dimension at all. Implemented as of Phase 3.

The more promising direction for team/shared deployments, if a single shared server ever needs real per-project access control, is binding scope to how the MCP server is *launched or configured* (one server process, or one `--project` default in that project's `.mcp.json`, per active project) rather than retrofitting this client-side convenience — see ADR 0016's consequences.

## 8. Technical architecture

### 8.1 Overview

```text
Browser web UI ───────────────────────┐
CLI client ───────────────────────────┼──> Versioned HTTP API ──> Application services
MCP HTTP endpoint / STDIO bridge ─────┘             │                    │
                                                    │ SSE                ├──> SQLite + FTS5
                                                    └────────────────────└──> Managed file store
```

There is one authoritative server and database. No client reads SQLite directly, including the STDIO MCP bridge. This boundary keeps validation, authorization, audit history, search indexing, and future migration behavior consistent.

### 8.2 Proposed stack

| Layer | Choice | Rationale |
| --- | --- | --- |
| Server and CLI | Go | Produces small native Linux and Windows executables, starts quickly, handles concurrency well, and can embed web assets. |
| Database | SQLite in WAL mode with foreign keys and FTS5 | Fits a personal/small-team server, requires no separate service, supports transactional data and fast full-text search. |
| Database access | Explicit SQL, migrations, and generated or thin typed query wrappers | Keeps behavior inspectable and avoids a large ORM abstraction. |
| Web UI | React + TypeScript built with Vite | Mature ecosystem for an interactive board/editor UI; production assets are embedded in the Go executable. |
| API | JSON REST under `/api/v1`, described by checked-in OpenAPI | Easy to debug, script, generate clients for, and evolve deliberately. |
| Live updates | SSE | Sufficient for server-to-browser notifications and simpler than a bidirectional socket protocol. |
| Agent protocol | MCP over Streamable HTTP plus an STDIO bridge | Broad agent-host compatibility without duplicating business logic. |
| File storage | Content-addressed files under the configured data directory | Avoids inflating SQLite and makes integrity checking and backup straightforward. |

Before implementation, a short technical spike must verify the selected pure-Go SQLite driver includes the required FTS5 behavior on both target platforms and that the selected MCP SDK supports both planned transports.

### 8.3 Storage model

The relational design should use a shared `entities` registry containing canonical ID, project ID, entity kind, public reference, timestamps, and soft-delete state. Concrete tables hold fields for projects, features, tickets, decisions, plans, and documents. This lets comments, attachments, associations, notifications, and audit events safely refer to one entity identity without unvalidated polymorphic strings.

Principal tables include:

- `entities`, `projects`, `features`, `tickets`, `decisions`, and `content_items`
- `actors`, `human_accounts`, `agent_tokens`, `sessions`
- `comments`, `comment_versions`, `content_versions`
- `ticket_relationships`, `entity_associations`, `derived_mentions`
- `attachments`, `attachment_versions`, `external_links`
- `audit_events`, `notifications`, `subscriptions`
- `idempotency_keys`, schema migrations, and FTS virtual/index tables

All timestamps are stored in UTC and rendered in the viewer's local timezone. Schema changes are forward migrations embedded in the executable. Startup refuses to run a database created by a newer incompatible server version.

### 8.4 Concurrency and retry safety

Mutable records carry an integer version. Update APIs require the version last read, through the body or `If-Match`, and return `409 conflict` with the current version when stale. This prevents silent overwrites between a browser and an agent.

Every mutation accepts an idempotency key. The server stores a bounded record of the key, actor, request fingerprint, and result so a client can safely retry after losing a response. Read requests may retry automatically; writes retry only when an idempotency key is present.

### 8.5 Client buffering decision

A central server is substantially simpler than distributed synchronization and fits the stated always-online use case. The MVP therefore does not silently queue writes while disconnected. Clients give a clear connection error and retain user-entered browser text locally long enough to avoid accidental loss.

A lightweight durable outbox is feasible later without changing the server architecture because mutations already have idempotency keys and optimistic versions. It would require a small local queue, ordered replay, visible pending/failed states, and an explicit conflict-resolution UI. This is moderate work rather than a trivial retry feature, so it should be added only if real disconnected use appears. It must not evolve into multi-master SQLite synchronization.

## 9. HTTP API requirements

- All application endpoints are versioned under `/api/v1`; MCP has its own advertised endpoint.
- JSON uses stable public names and ISO 8601 UTC timestamps.
- List endpoints use cursor pagination and default to compact representations.
- Sparse field and explicit expansion options prevent accidental large responses.
- Errors use a consistent envelope with code, message, relevant field, correlation ID, and optional retry/conflict information.
- Mutations support idempotency keys and optimistic versions.
- Create/update requests accept a client-generated correlation ID for tracing an agent workflow.
- Upload/download endpoints stream data and enforce size limits without buffering entire files in memory.
- OpenAPI is checked in and validated in CI. CLI and MCP contract tests run against the same service implementation.
- Health endpoints distinguish process liveness from database/storage readiness without exposing sensitive configuration.

## 10. Security and safety

- Bind to `127.0.0.1` by default. Listening on other interfaces requires an explicit configuration change and prints an access warning.
- Use Argon2id with per-password salts for human passwords.
- Store only hashes of agent tokens; show a token only at creation; support optional expiry and immediate revocation.
- Use secure, HTTP-only, same-site session cookies, CSRF protection, login throttling, and session expiry.
- Require TLS when traffic leaves the trusted host, normally through a documented reverse-proxy configuration. Refuse or strongly warn about bearer tokens over unencrypted non-loopback HTTP.
- Sanitize Markdown and attachment filenames, use a restrictive Content Security Policy, and serve uploads with safe content-disposition behavior.
- Never resolve or serve arbitrary path references. Managed uploads cannot escape the configured storage root.
- Validate uploaded sizes and checksums. Malware scanning is outside MVP scope and must be stated in deployment documentation.
- Redact secrets from logs and audit events. Do not place token values in command arguments, URLs, or exported data.
- Use parameterized SQL, enable SQLite foreign keys, and validate relationship cycles transactionally.
- Create a pre-migration backup and fail safely if a migration cannot complete.

## 11. Performance and reliability targets

The reference performance dataset is 25 projects, 100,000 tickets, 500,000 comments, and 10,000 decisions/plans/documents, excluding uploaded-file bytes. On an ordinary current developer workstation with a warm local database:

- Indexed detail and first-page list requests should have p95 server latency below 100 ms.
- Full-text search first-page requests should have p95 server latency below 200 ms.
- Ordinary non-upload mutations should have p95 server latency below 250 ms.
- Server startup should normally complete within two seconds, excluding migrations or integrity recovery.
- MCP and CLI list responses default to at most 20 compact records and never include full bodies or comment history implicitly.

These are engineering targets rather than absolute guarantees across all hardware. Benchmarks must record dataset, hardware, build, and cold/warm state.

Reliability requirements:

- Atomic transactions preserve entity changes, audit events, notifications, and search-index updates together.
- Graceful shutdown stops accepting work, completes bounded in-flight requests, and closes the database cleanly.
- A backup can be created while the server runs using SQLite's supported online-backup mechanism or a consistent server-controlled snapshot.
- Import runs transactionally where size permits and produces a validation report before committing destructive changes.
- The database must not be hosted directly on a network filesystem. Team clients connect to the HTTP server instead.

## 12. Backup, import, and export

Two mechanisms are required:

1. **Operational backup:** a consistent database snapshot plus the managed attachment directory and a manifest of checksums and server/schema versions.
2. **Portable export:** documented, versioned JSON plus attachment files in a directory or archive, containing all non-secret domain data, history, and stable IDs.

Restore verifies manifest checksums before replacing active state and is an explicit administrative operation. Import supports a dry run, reports reference collisions and invalid relationships, and never imports password hashes, sessions, or usable tokens. Exported agent identities require newly issued tokens after restore/import when appropriate.

## 13. Observability and administration

- Structured logs with severity, timestamp, correlation ID, route/tool, actor ID where safe, latency, and result code.
- Human-readable console logging by default and JSON logging by configuration.
- Commands for database integrity checks, search-index rebuild, backup, restore, export, import dry-run, and token revocation.
- A small admin UI for account, agent, token, and server status management.
- No telemetry sent outside the installation in the MVP.

## 14. Implementation roadmap

Each phase should finish with documentation and tests for the behavior it introduces. Prefer usable vertical slices over building every table before any workflow can be exercised.

### Phase 0: contracts and technical spikes

- Record architecture decisions for the hierarchy, shared entity registry, SQLite, authentication, API, MCP transports, and attachment boundaries.
- Define public references, status/priority enums, error envelope, compact/detail representations, and concurrency semantics.
- Draft the OpenAPI document and MCP tool schemas for the smallest project/feature/ticket slice.
- Verify SQLite WAL/FTS5 and MCP Streamable HTTP/STDIO on Linux and Windows.
- Establish repository structure, formatting, linting, tests, and cross-platform CI.

Exit criterion: a minimal server can create and fetch a ticket through an API proof of concept on both platforms, and protocol risks are resolved.

### Phase 1: core domain and persistence

- Implement migrations and typed storage access.
- Implement projects, automatic `General` features, features, tickets, public references, workflow, priority positions, and soft deletion.
- Implement actors, comments, audit events, typed ticket relationships, associations, and derived reference parsing.
- Add service-layer validation, cycle detection, transactions, optimistic versions, and idempotency records.
- Add representative fixture generation and database benchmarks.

Exit criterion: domain and repository integration tests cover the complete core ticket lifecycle, history, relationships, concurrency, and ordering.

### Phase 2: API, identity, and server runtime

- Implement `/api/v1`, pagination, sparse/expanded responses, error envelopes, streaming uploads, and health endpoints.
- Implement first-run setup, human login/session security, anonymous viewer mode, agents, hashed tokens, and admin operations.
- Implement configuration, structured logging, graceful shutdown, embedded migrations, and data-directory validation.
- Generate or implement the shared API client used by CLI and MCP.

Exit criterion: two authenticated actors and one anonymous viewer receive correct permissions and attribution through contract-tested API calls.

### Phase 3: CLI and MCP agent workflow

- Implement the non-interactive CLI conventions and core commands.
- Implement the MCP Streamable HTTP endpoint and STDIO bridge.
- Optimize tool descriptions, compact responses, pagination, and selective expansion for agent accuracy and token use.
- Add optional concise Codex/Claude instruction files and setup documentation.
- Test a complete workflow: find assigned work, read linked context, start ticket, comment, create decision, and complete ticket.

Exit criterion: both Codex and Claude Code can perform the workflow against the same server with separately attributed agent identities; shell scripts can perform it with stable JSON through the CLI.

### Phase 4: core web UI

- Implement setup/sign-in, project overview, backlog, kanban, priority queue, issue register, and detail views.
- Implement ticket/feature create and edit flows, status movement, assignment, relationships, comments, links, and attachments.
- Implement Markdown edit/preview, safe rendering, entity reference linking, filters, keyboard navigation, responsive layout, and error/conflict states.
- Add browser end-to-end and accessibility smoke tests.

Exit criterion: a human can carry out the core workflow entirely in the web UI, including resolving an optimistic concurrency conflict without losing edits.

### Phase 5: knowledge, search, and collaboration

- Implement decisions, plans, documents, version snapshots, diffs, and associations.
- Implement uploaded-file versioning and path/URL reference history.
- Implement FTS5 search, snippets, filters, backlinks, and index rebuild.
- Implement subscriptions, notifications, mentions, SSE change hints, and the activity feed.
- Complete the decision register, plan/document library, search, inbox, and activity UI.

Exit criterion: knowledge records and attachments retain complete visible history; search finds all indexed kinds; two browsers observe changes and notifications without manual full-page refresh.

### Phase 6: release hardening

- Implement a project summary/brief view — a single read assembling what a new agent needs to get oriented fast (upcoming/in-progress tickets, issue register highlights, feature list, recent activity, decisions/plans). No new schema: an aggregation over data the information model (§5) already covers, natural fit in the Phase 3 MCP tool surface (§7.2's `project_get`/`search` neighborhood). Moved here from §18's future options now that its blocker — Phase 5's decision/plan records — is resolved.
- Implement backup, restore, JSON export/import, integrity checks, and documented recovery drills.
- Run performance benchmarks and optimize indexes, expansions, search, and agent payloads.
- Threat-model authentication, uploads, Markdown, path references, anonymous access, and MCP token handling.
- Test clean installation and upgrades on supported Linux and Windows versions.
- Write user, administration, CLI, MCP, API, backup, and troubleshooting documentation.
- Produce versioned native release artifacts and checksums.

Exit criterion: all MVP acceptance criteria pass on both target platforms and a backup/restore drill reproduces the reference installation.

## 15. Test strategy

- **Unit tests:** reference parsing, workflow validation, rank allocation, authorization, Markdown sanitization, relationship inverses/cycles, notification rules, and diff presentation.
- **Database integration tests:** every migration, rollback-on-error behavior, constraints, FTS consistency, audit atomicity, soft deletion, concurrency, and idempotent retry.
- **API contract tests:** OpenAPI conformance, permissions, pagination, sparse fields, errors, uploads, caching headers, and version conflicts.
- **CLI tests:** command parsing, stdin, JSON golden files/schema compatibility, exit codes, token redaction, and non-interactive behavior.
- **MCP tests:** tool schemas, transport behavior, authentication, compact response guarantees, errors, pagination, and representative agent tasks.
- **Web tests:** component tests plus end-to-end creation, editing, boards, search, conflict recovery, notifications, versions, and anonymous read-only behavior.
- **Security tests:** session and CSRF behavior, brute-force throttling, XSS payloads in Markdown, path traversal, upload headers, SQL injection, secret logging, and permission boundaries.
- **Performance tests:** seeded reference dataset, hot and cold queries, concurrent readers/writers, FTS, board loads, activity pagination, and attachment streaming.
- **Recovery tests:** online backup during writes, checksum failure, restore, export/import round trip, interrupted migration, and FTS rebuild.
- **Platform tests:** CI and release smoke tests on Linux and Windows, including paths with spaces and non-ASCII characters.

## 16. MVP acceptance criteria

The MVP is complete only when all of the following are demonstrated:

- A single executable starts a fresh server and embedded web UI on Linux and Windows.
- First-run setup creates an admin; anonymous users can read when enabled but every write is rejected without authentication.
- Humans can create and edit all in-scope record types in the web UI.
- Creating a project-level ticket transparently uses its `General` feature, and the ticket can later move features.
- Priorities and manual positions produce the same deterministic order in API, CLI, MCP, and UI.
- The issue register separates bug/security work and orders it by severity.
- `#ABC-123` references create links and backlinks without being mistaken for dependencies; explicit dependencies support multiple tickets and reject cycles.
- Markdown, uploaded files, paths, and URLs can be attached or represented as specified, with the correct kind of version history.
- Two different agent tokens create separately attributed audit events, can be revoked independently, and never appear in logs or exports.
- Codex and Claude Code can use MCP for the representative ticket workflow; the same workflow is possible through CLI JSON.
- The web UI receives live change hints and shows assignment/mention notifications.
- Full-text search returns compact, relevant results across all promised content types.
- A stale concurrent edit receives a conflict and neither version is silently lost.
- A repeated idempotent mutation does not create duplicate tickets or comments.
- Performance targets are measured against the reference dataset and material regressions are documented or corrected.
- Backup and restore preserve records, attachments, references, versions, audit history, and checksums; portable export/import is validated separately.
- The release documentation covers secure LAN sharing and clearly warns about anonymous reads, bearer tokens without TLS, path references, and lack of malware scanning.

## 17. Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| The scope grows into a full project-management suite. | Hold the MVP boundary: fixed workflows, two content permission levels, no custom fields, no external integrations. |
| Anonymous read access exposes sensitive plans or security tickets. | Default to loopback, make anonymous access configurable, warn on external binds, and document the whole-server permission boundary. |
| Agent tools return too much text and waste context. | Compact defaults, pagination, sparse fields, explicit expansions, search snippets, and payload regression tests. |
| Too many tiny MCP tools confuse agents. | Keep a reviewed task-oriented surface, use consistent schemas, and test representative workflows with multiple hosts. |
| SQLite is misused as a shared file database. | Keep it server-local, use WAL, document that clients must use HTTP, and benchmark realistic concurrency. |
| Audit and version snapshots grow indefinitely. | Keep append-only correctness first, expose storage metrics, deduplicate uploads, and add a future explicit retention/purge policy if needed. |
| Reordering conflicts under concurrent edits. | Allocate rank gaps, update transactionally, use versions, and deterministically renumber a scoped group. |
| Arbitrary path attachments become a file disclosure vector. | Store path text only and never read or serve it through ordinary attachment endpoints. |
| Live events become another source of state. | Treat SSE as invalidation/change hints only; refetch authoritative state from the API. |
| A buffered client creates hidden conflicts or duplicate work. | Exclude durable buffering from MVP and prepare only with idempotency and optimistic concurrency. |

## 18. Future options

Future work should be driven by observed use rather than included speculatively. Plausible additions are:

- An opt-in durable client outbox for short disconnections.
- Docker images and Compose examples.
- Labels, saved filters, custom fields, and configurable workflows.
- Project-level roles and private projects.
- Email or chat notifications.
- GitHub/GitLab and commit/pull-request associations.
- File content indexing and optional malware scanning.
- Due dates, milestones, estimates, and recurring tickets.
- Plugin packaging or hosted remote MCP distribution.
- A PostgreSQL storage option if deployment or concurrency eventually exceeds SQLite's intended role.

None of these should compromise exportability, stable references, attribution, or the single application-service boundary.

## 19. Resolved design decisions

- Optimize for a personal installation, but use persistent actors and a server API so a trusted team works without redesign.
- Use `Project -> Feature -> Ticket`; every project has a `General` feature for small ungrouped work.
- Treat bug/security issues as ticket types; keep decisions, plans, and documents first-class.
- Use explicit relationships for semantics and Markdown references for lightweight context/backlinks.
- Use a central server with no offline synchronization in the MVP.
- Make MCP the primary agent interface and retain a thin CLI for scripts and broad compatibility.
- Use a Go single-binary server/CLI, embedded React web UI, SQLite/FTS5, and managed file storage.
- Use username/password sessions for humans, bearer tokens for agents, and optional anonymous read-only access.
- Keep workflows and priorities fixed initially, with numeric manual ordering inside priority groups.
- Store Markdown as versioned snapshots with computed line diffs; version upload metadata and path/URL changes appropriately.

Product naming, branding, final license, and exact release packaging remain intentionally undecided because they do not block implementation.

## 20. Integration references

- [OpenAI documentation: MCP in Codex](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
- [Anthropic documentation: MCP](https://docs.anthropic.com/en/docs/mcp)
- [Model Context Protocol documentation](https://modelcontextprotocol.io/docs/2026-07-28/getting-started/intro)
