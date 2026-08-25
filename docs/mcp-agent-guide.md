# MCP agent guide

How a coding agent (Codex, Claude Code, or any other MCP client)
connects to Tickets and uses its tools. `docs/contracts/cli.md` is this
document's CLI counterpart — the same underlying `internal/service`
operations, reached a different way (§16: "the same workflow is
possible through CLI JSON").

## Connecting

Two transports (ADR 0006), both serving the identical tool set
registered by `internal/mcpsrv.RegisterTools`:

- **Streamable HTTP**, mounted at `/mcp` on a running `tickets server`
  (alongside the REST API at `/api/v1`). Requires a valid agent bearer
  token on every request, same as the HTTP API
  (`Authorization: Bearer <token>`).
- **stdio**, via `tickets mcp --url <api-url> --token <bearer-token>`.
  This never opens SQLite directly (product spec §8.1) — it's a thin
  bridge that reaches the same server over its HTTP API. This is the
  form a `.mcp.json`/Codex config entry typically uses:

  ```json
  {
    "mcpServers": {
      "tickets": {
        "command": "tickets",
        "args": ["mcp", "--url", "http://127.0.0.1:8080/api/v1", "--token", "<bearer-token>"]
      }
    }
  }
  ```

  `--token` as a flag is fine here specifically because a config file
  invokes it non-interactively — never typed at a shell (contrast
  `docs/contracts/cli.md`'s CLI commands, none of which have a `--token`
  flag for exactly that reason).

Get a bearer token via `tickets admin agent create` +
`tickets admin token create` (`docs/contracts/cli.md`'s `admin
agent`/`admin token` section) — issued once, shown once, never
recoverable afterward.

### `--project` / `TICKETS_PROJECT`

Tickets doesn't scope tokens to a single project (ADR 0016) — an
agent's token works across every project it has permission for. The
bridge's `--project` flag (or `TICKETS_PROJECT` env var) is a purely
client-side convenience: it's filled in automatically whenever a tool
call's `project_key` is omitted, invisible to the server. Set it once
per bridge instance and every tool call can omit `project_key`
entirely, unless the agent is legitimately working across projects in
one session.

## What the server tells you at connect time

The MCP `initialize` response's `instructions` field
(`internal/mcpsrv/server.go`'s `serverInstructions`) is the
authoritative, always-current statement of the cross-tool conventions
below — if this section and that constant ever disagree, the constant
is what a connected client actually receives, so trust it. As of this
writing:

> Tickets is a self-hosted issue tracker. References use the form
> PROJECTKEY-N: a ticket is "ABC-123" (no letter code), a feature is
> "ABC-F1", a decision is "ABC-D1". Writing "#ABC-123" inside a ticket's
> description or a comment body creates a backlink to that entity — it
> does not create a dependency; use ticket_link for that.
>
> List tools (projects_list, tickets_list) return compact rows only —
> no description/context/decision/rationale body text — to keep
> responses small. Call the matching \*\_get tool (ticket_get,
> feature_get, record_get) for an entity's full detail before acting on
> its content.
>
> ticket_link's type is either "associated_with" (a loose reference for
> context, e.g. linking a ticket to the decision that explains it — no
> dependency implied) or one of 8 explicit relationship types
> (parent_of, child_of, blocks, blocked_by, related_to, duplicate_of,
> supersedes, superseded_by), which are ticket-to-ticket only.
>
> ticket_update is a partial update: only the fields you set are
> changed. feature_update and record_update are full-representation
> updates instead, with no merge: their schema marks every text field
> required, so a compliant client will refuse to send a call missing
> one (rather than silently wiping it) — but the value each field
> carries still replaces what is stored, unchanged or not. Call
> feature_get/record_get first and resend every field's current value
> (you need that call's version for expected_version anyway).
> record_update's superseded_by is the one optional field: omit it (or
> send "") to clear an existing supersession link, the same as any
> other omitted field there — it isn't a partial-update exception.
>
> record_\* covers decisions, plans, and documents. record_create's kind
> is "decision" (default), "plan", or "document". Decisions use
> title/context/decision/rationale/consequences/status/superseded_by;
> plans and documents use title/body (Markdown) instead — the other
> kind's fields are simply omitted from that call. record_get/
> record_update infer which kind a reference names from the reference
> itself (ABC-D1 is a decision, ABC-P1 a plan, ABC-DOC1 a document), so
> neither needs a kind argument. ticket_comment is the only comment tool,
> despite its name — ref accepts a ticket, feature, decision, plan, or
> document reference, or a bare project key, so it works on any of those
> six kinds.
>
> ticket_comment and record_create accept an optional idempotency_key:
> reusing the same key with identical arguments returns the original
> result instead of creating a duplicate, and reusing it with different
> content is rejected as idempotency_key_reused — useful when retrying
> after a dropped connection.

## Tool surface (Phase 3, plus later additions noted per row)

| Tool | Purpose |
| --- | --- |
| `project_brief` | **Call this first** when starting work in a project: in-progress/upcoming tickets, issue-register highlights, the feature list with ticket-progress counts, recent activity, and recent accepted decisions and plans, each capped at 20 compact rows. `ticket_get`/`record_get` follow it for detail on any one record (Phase 6 Step 5). |
| `project_get` / `projects_list` | Read a project / list projects, compact rows. |
| `project_create` | Create a project. Always creates a General feature alongside it (ADR 0001). |
| `project_update` | Update a project's title/description and/or archive/unarchive status (ADR 0021) — only fields you set are changed. Archiving is visibility only: the project drops out of default `projects_list`/search results, but its tickets/features/knowledge records stay fully readable and writable. |
| `ticket_get` / `tickets_list` | Read a ticket / list tickets. `tickets_list`'s `view` is `priority_queue` (default) or `issue_register`; `status`/`type`/`severity`/`priority`/`feature_ref`/`assignee`/`creator`/`updated_since` are optional, AND-composed filters (Phase 7) — set `assignee` to your own actor reference (e.g. `agent:codex`) to find your assigned work in one call, rather than paging through every ticket. |
| `ticket_create` | Create a ticket — the one create tool that returns the full `domain.Ticket`, not a compact write result, since an agent that just created something usually needs its full state immediately. |
| `ticket_update` | Partial update — see the instructions text above. |
| `ticket_comment` | Add a Markdown comment to a ticket, feature, decision, plan, document, or project (despite the tool's name) — see the instructions text above. |
| `ticket_link` | Associate or relate two entities — see the instructions text above. |
| `ticket_relationships` | Read back a ticket's explicit relationships (both ends), from that ticket's perspective. |
| `ticket_associations` | Read back a ticket or feature's `associated_with` links. |
| `feature_get` / `features_list` / `feature_create` / `feature_update` | Feature CRUD, plus a compact paginated list. |
| `record_get` / `record_create` / `record_update` | Decision/plan/document CRUD via a `kind` discriminator (see instructions text). |
| `search` | Full-text search over tickets/features/decisions/plans/documents/comments/attachment names/link titles and URLs, ranked by relevance (Phase 5 Step 6, ADR 0018; attachment/link indexing added at Step 9 close-out). Named `search`, not the plan's original working name `work_search`. |
| `notifications_list` / `notifications_mark_read` | Read and clear the calling actor's own notification inbox (Phase 5 Step 7, ADR 0019): assignment, an `@kind:name` mention, a reply/comment, or a change to subscribed work. There is no subscribe/unsubscribe tool — that's HTTP/CLI-only (`tickets subscribe`/`tickets unsubscribe <ref>`), the same reasoning agent/token management is CLI-only. |

`project_create`, `features_list`, `ticket_relationships`, and
`ticket_associations` were added after Phase 3's live dogfood step
found each gap by actually using the tool surface — none were in the
plan's original MCP tool table, so their absence wasn't a defect
against a committed scope, just an ergonomic hole a live session
surfaced. `project_create` also closes the matching CLI gap
(`cmd/tickets/project.go` previously had only `list`).

Not present in Phase 3:

- Full-text search — landed in Phase 5 Step 6 as the `search` tool
  (see the table above), not the plan's placeholder name `work_search`.
- ~~`notifications_list`/`notifications_mark_read`~~ — landed in Phase 5 Step 7 (see the table above).
- Any `agent_*`/`token_*` tool — not coming as an MCP tool at all. See
  `cmd/tickets/admin_agent.go`'s package doc comment: `InProcessBackend`
  calls `*internal/service.Service` directly, bypassing
  `internal/httpapi`'s `requireAdmin` wrapper entirely, so an
  agent-management tool would be unenforced over the HTTP-mounted
  endpoint and simply broken over stdio (no admin session exists
  there). Agent/token management is CLI-only
  (`docs/contracts/cli.md`'s `admin agent`/`admin token`).
- ~~A project-brief view~~ — landed in Phase 6 Step 5 as the
  `project_brief` tool (see the table above).

## Representative workflow

The sequence product spec §16 names as Phase 3's acceptance bar: find
assigned work, read linked context, start the ticket, comment, record
a decision, complete the ticket.

0. `project_brief` to get oriented before anything else — it surfaces
   in-progress/upcoming and issue-register tickets, features, recent
   activity, and recent accepted decisions/plans in one call, often
   enough to skip a separate `tickets_list` call in step 1.
1. `tickets_list` with `view: "issue_register"` or `"priority_queue"`
   to find work (compact rows — ref/title/status/priority/severity
   only).
2. `ticket_get` on the ticket you're taking, to read its full
   description. If the description references `#ABC-D3` or similar,
   `record_get`/`feature_get` that reference for context before
   acting.
3. `ticket_update` with `status: "in_progress"` and
   `expected_version` from step 2's `ticket_get`.
4. `ticket_comment` to narrate progress, or `ticket_link` with
   `type: "associated_with"` to connect the ticket to a decision that
   explains an approach.
5. `record_create` if the work involved a decision worth recording
   (title/context/decision/rationale) — then `ticket_link` it to the
   ticket.
6. `ticket_update` with `status: "done"` (or the appropriate terminal
   status) and the latest `expected_version`.

`ticket_relationships`/`ticket_associations` let you confirm a link
you (or another actor) created actually landed, or discover one you
didn't know about — useful between steps 4 and 6 if you want to verify
before completing the ticket.

Every step after the first uses the version/ref returned by the
previous call — never a value computed or guessed client-side, so a
concurrent edit by another actor surfaces as `version_conflict`
(`docs/contracts/concurrency.md`) rather than being silently
overwritten.

## Error vocabulary

Tool errors are `"<code>: <message>"` strings (`internal/mcpsrv/
tools.go`'s `toolError`), using the same `domain.ErrorCode` catalogue
as the HTTP API (`docs/contracts/errors.md`) and the CLI's exit codes
(`docs/contracts/cli.md`) — one vocabulary across every interface.
