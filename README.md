# Tickets

A self-hosted issue tracker for humans and coding agents. One binary
serves a REST API, an embedded web UI, and an MCP server (both
Streamable HTTP and stdio) over the same SQLite-backed data directory
— no external database, no separate services, no Docker.

Tickets exists so a coding agent (Claude Code, Codex, or any other MCP
client) and the humans it works with can share one project tracker
through the interface each is best at: agents call MCP tools or the
CLI's `--json` output, humans use the web UI. Projects contain
features, which contain tickets; tickets carry priority/severity,
typed relationships and associations, comments, attachments, and
`#REF-123`-style cross-references that resolve automatically. Decisions
and plans/documents get full version history. Everything is full-text
searchable, and the web UI updates live over SSE.

Full product design lives in [`plan.md`](plan.md); the architectural
decisions and their rationale (not just the outcome) live in
[`docs/adr/`](docs/adr/README.md).

## Quickstart

```sh
# 1. First-run setup: creates the one admin account (non-interactive; no prompts).
tickets setup --username admin --password <a real password>

# 2. Start the server (defaults to 127.0.0.1:8080, data dir under your OS's config directory).
tickets server

# 3. Open the web UI.
open http://127.0.0.1:8080
```

By default `tickets server` binds to loopback only and enables
anonymous read access (browse without logging in; every write still
requires authentication) — a reasonable default for a single machine.
Binding to a non-loopback address prints a warning and, per
[`docs/security-model.md`](docs/security-model.md), needs a deliberate
decision about who else can reach it. See
[`docs/install.md`](docs/install.md) for building from source and
[`docs/admin.md`](docs/admin.md) for every configuration key and
`admin` subcommand.

## Using it

- **Web UI** — served at `/` once you're signed in (or immediately, if
  anonymous read is enabled). No separate install; it's embedded in
  the binary.
- **CLI** — `tickets project|ticket|feature|decision|plan|document|...`
  talk to a running server over its HTTP API; run `tickets help` for
  the full command list, [`docs/cli.md`](docs/cli.md) for a guided
  tour, and [`docs/contracts/cli.md`](docs/contracts/cli.md) for the
  exact wire contract (flags, exit codes, JSON shapes).
- **REST API** — `api/v1`, documented in
  [`api/openapi.yaml`](api/openapi.yaml) with a prose walkthrough at
  [`docs/api.md`](docs/api.md).
- **MCP** — connect a coding agent over `/mcp` (Streamable HTTP, bearer
  token) or via the `tickets mcp` stdio bridge. Start with
  [`docs/mcp-agent-guide.md`](docs/mcp-agent-guide.md) — it covers the
  reference grammar and the recommended `project_brief`-first workflow.

## Backup, recovery, and troubleshooting

[`docs/backup-recovery.md`](docs/backup-recovery.md) covers
`admin backup`/`admin restore` (disaster recovery, same machine) and
`export`/`import` (portable, redacted JSON, for moving or archiving
content). [`docs/troubleshooting.md`](docs/troubleshooting.md) covers
common failure modes and how to diagnose them.

## Working on this codebase

See [`CLAUDE.md`](CLAUDE.md)/[`AGENTS.md`](AGENTS.md) for the layer
boundaries, contracts, and build/test commands (`task ci` is the full
local gate). This section is for using Tickets, not developing it.
