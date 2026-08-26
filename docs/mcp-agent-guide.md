# MCP agent guide

Tickets exposes the same application behavior through Streamable HTTP and a
stdio bridge. The server's `initialize.instructions` field is the concise,
authoritative cross-tool contract; this guide adds connection details and a
tool index.

## Connect

Use either transport:

- Streamable HTTP at `/mcp` on a running `tickets server`. Every request needs
  an agent token in `Authorization: Bearer <token>`.
- stdio through `tickets mcp --url <api-url> --token <bearer-token>`. The
  bridge calls the Tickets HTTP API and never opens the database directly.

Example client configuration:

```json
{
  "mcpServers": {
    "tickets": {
      "command": "tickets",
      "args": [
        "mcp",
        "--url",
        "http://127.0.0.1:8080/api/v1",
        "--token",
        "<bearer-token>"
      ]
    }
  }
}
```

Create a token with `tickets admin agent create` followed by
`tickets admin token create`. A token is displayed once and cannot be recovered.

### Default project

`tickets mcp --project ABC` or `TICKETS_PROJECT=ABC` supplies omitted
`project_key` values in the stdio bridge. This is a client-side convenience,
not an authorization boundary. Direct `/mcp` connections have no default and
must send `project_key`. `project_create` and `project_update` always require an
explicit key.

## Core contract

Projects use a bare key such as `ABC`. Other references are `ABC-123` for a
ticket, `ABC-F1` for a feature, `ABC-D1` for a decision, `ABC-P1` for a plan,
and `ABC-DOC1` for a document.

Markdown mentions create backlinks. `ABC-123` and `#ABC-123` are recognized;
`#123` also identifies a ticket in a project-scoped comment. A backlink is not
a dependency or other typed relationship.

List and search tools return compact results. Read the matching entity with
`project_get`, `ticket_get`, `feature_get`, `record_get`, or `comment_get`
before relying on full content. Use `project_brief` when you need broad project
context; use a specific get tool when you already know the target.

Every input named `expected_version` uses optimistic concurrency. Send the
latest value returned by a read or write. A stale value returns
`version_conflict` with `current_version`.

`project_update`, `ticket_update`, and `feature_update` are partial. Status,
ticket assignment, and ticket feature movement are separate operation groups;
do not combine them with content changes. `record_update` replaces every field
applicable to that record, so read it first and resend unchanged values.

`ticket_create` requires exactly one of `feature` or `general:true`. There is no
implicit General selection.

Create tools and `comment_create` accept an optional `idempotency_key`. Reuse a
key only to retry identical input. Reusing it for different input returns
`idempotency_key_reused`.

## Tools

### Projects and work

| Tool | Purpose |
| --- | --- |
| `project_brief` | Get project details plus capped, compact views of current work, features, activity, accepted decisions, and plans. |
| `project_get` | Get full project details. |
| `projects_list` | Page through compact projects; set `include_archived:true` to include archived projects. |
| `project_create` | Create a project and its General feature. |
| `project_update` | Update title/description or, in a separate call, active/archived status. |
| `tickets_list` | Page through compact tickets using AND-composed workflow, type, severity, priority, feature, assignee, creator, and update-time filters. |
| `ticket_get` | Get full ticket details; `include_deleted:true` also retrieves a deleted ticket for restoration. |
| `ticket_create` | Create a backlog ticket in an explicit feature or in General. |
| `ticket_update` | Update one group: status, content fields, assignee, or feature. Empty severity or assignee clears it. |
| `ticket_reorder` | Reposition a ticket within its current priority group. |
| `ticket_delete` / `ticket_restore` | Soft-delete or restore a ticket. |

### Features

| Tool | Purpose |
| --- | --- |
| `features_list` | Page through compact, non-deleted features using AND-composed status, priority, creator, and update-time filters. |
| `feature_get` | Get full feature details; `include_deleted:true` also retrieves a deleted feature for restoration. |
| `feature_create` | Create a backlog feature. |
| `feature_update` | Update content fields or, in a separate call, workflow status. |
| `feature_reorder` | Reposition a feature within its current priority group. |
| `feature_delete` / `feature_restore` | Soft-delete or restore a feature. Cascading deletion also deletes its tickets; restoration does not restore them automatically. |

### Comments and connections

| Tool | Purpose |
| --- | --- |
| `comment_create` | Comment on a project, ticket, feature, decision, plan, or document. |
| `comments_list` / `comment_get` | Page through compact comments, then read a full body or tombstone. |
| `comment_update` / `comment_delete` | Replace a comment body or leave a permanent tombstone. |
| `comment_history` | Page through archived prior comment bodies. |
| `relationship_add` / `relationship_remove` | Manage typed ticket-to-ticket relationships. |
| `relationships_list` | Page through a ticket's relationships from that ticket's perspective. |
| `association_add` / `association_remove` | Manage symmetric `associated_with` connections among tickets, features, decisions, plans, and documents. |
| `associations_list` | Page through an entity's associations. |
| `backlinks_list` | Page through live entities and comments whose Markdown mentions a reference. |

`duplicate_of` has no inverse and is listed only from the duplicate ticket.
Other relationship types are `parent_of`, `child_of`, `blocks`, `blocked_by`,
`related_to`, `supersedes`, and `superseded_by`.

### External material

| Tool | Purpose |
| --- | --- |
| `external_link_add` / `external_link_remove` | Manage named HTTP, HTTPS, or mailto bookmarks. |
| `external_links_list` | Page through an entity's external bookmarks. |
| `attachment_get` | Read attachment metadata by ID. |
| `attachments_list` | Page through attachment metadata for one entity or comment. |
| `attachment_versions` | Page through archived attachment metadata. |

MCP does not transfer attachment bytes or create, replace, or delete
attachments. Use the HTTP API or CLI for binary and attachment mutations.

### Decisions, plans, and documents

| Tool | Purpose |
| --- | --- |
| `record_get` | Get a decision, plan, or document; the reference identifies its kind. |
| `record_create` | Create a record. `kind` defaults to `decision`; decisions start as `proposed`. |
| `record_update` | Replace all applicable fields. File-backed records cannot be updated through MCP. |
| `records_list` | Page through one record kind in a project. |
| `record_versions` | Page through archived snapshots, including representation-specific metadata. |
| `record_diff` | Diff decision text, or a plan/document title and Markdown body, between two versions. |

Plans and documents have one immutable representation: `markdown`, `path`,
`url`, or a file created outside MCP. Set only the field selected by that
representation. Decision updates require `context`, `decision`, `rationale`,
`consequences`, and `status`; omit or empty `superseded_by` to clear it.

### Discovery and notifications

| Tool | Purpose |
| --- | --- |
| `search` | Search projects, records, comments, attachment names, and external links by relevance. |
| `project_activity` | Page through project audit events, optionally filtered by actor, entity kind, or event type. |
| `notifications_list` | Page through the caller's notifications, optionally unread only. |
| `notifications_mark_read` | Mark selected notifications, or all unread notifications, read. |
| `subscription_update` | Subscribe or unsubscribe the caller from an entity's change notifications. |

A comment, attachment, or external-link search hit uses `ref` for its owning
entity. Creating an entity subscribes the caller to it; commenting subscribes
the caller to the comment owner. Callers do not receive notifications for their
own changes.

## Representative workflow

1. Call `project_brief`, or `tickets_list` with filters such as
   `assignee:"agent:codex"`, to find work.
2. Call `ticket_get` and follow relevant references with `record_get` or
   `feature_get`.
3. Call `ticket_update` with `status:"in_progress"` and the ticket's current
   version.
4. Use `comment_create` to report progress. Use `association_add` for contextual
   connections or `relationship_add` for ticket dependencies.
5. Use `record_create` when the work produces a durable decision, plan, or
   document, then associate it with the ticket if useful.
6. Call `ticket_update` with the appropriate terminal status and the latest
   ticket version.

## Errors

Tool errors begin with the same stable code used by the HTTP API and CLI, for
example `validation_failed`, `not_found`, or `version_conflict`. Field-specific
errors append `field`; version conflicts append `current_version`. Treat the
message as explanatory text and branch on the code.
