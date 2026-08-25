# CLI contract

Backed by `cmd/tickets` and its tests. Covers every client-mode
subcommand (`project`, `ticket`, `feature`, `decision`, `comment`,
`admin agent`/`admin token`) — not `server`/`setup`/`mcp`, which have
their own established shapes predating Phase 3.

Most of these commands are a thin `internal/apiclient` wrapper around
the same HTTP routes `docs/mcp-agent-guide.md`'s MCP tools reach
through `internal/service` (product spec §16's "the same workflow is
possible through CLI JSON") — but the two surfaces are not fully
parallel. `admin agent`/`admin token` exist only here, not as MCP
tools, for a real reason (see their section below). `ticket get`'s
`--fields`/`--include` have no MCP equivalent either — no `_get` tool
in Phase 3 exposes server-side field projection or sub-resource
expansion.

## Connection config precedence

Lowest to highest, same layering `internal/config.Load` uses for the
server's own settings (`cmd/tickets/clientconfig.go`):

1. Built-in defaults (`--url` defaults to
   `http://127.0.0.1:8080/api/v1`, matching `tickets mcp`'s own
   default so both talk to the same out-of-the-box server).
2. The client config file — `TICKETS_CLIENT_CONFIG_FILE`, or
   `os.UserConfigDir()/tickets/client.json` if unset. Deliberately a
   different file from the server's own `config.json`: a CLI operator
   and a server operator are often different concerns even on one
   machine. Recognized keys: `api_url`, `token`, `project`.
3. Environment variables: `TICKETS_API_URL`, `TICKETS_API_TOKEN`,
   `TICKETS_PROJECT`, `NO_COLOR`.
4. Flags: `--url`, `--project`, `--json`, `--no-color`,
   `--token-stdin`.

## Token handling

**No client-mode command has a `--token <value>` flag.** Only the
config file, `TICKETS_API_TOKEN`, or `--token-stdin` (reads one line
from stdin) can supply a bearer token — a flag value would land in
shell history, `ps` output, and often a script's own source. This is
why `tickets mcp --token` looks like an exception: it's fine there
specifically because a `.mcp.json` entry invokes it non-interactively,
never typed at a shell.

`admin agent`/`admin token` have no token at all — see their own
section below.

## `--fields` / `--include`

Server-side sparse fields and sub-resource expansion
(`docs/contracts/representations.md`), forwarded straight through as
`?fields=`/`?include=` query params — the CLI does no client-side
projection. As of Phase 3, both are implemented on exactly one entity:

- `tickets ticket get <ref> --fields ref,title,status` and
  `tickets ticket list --fields ref,title` narrow the response to
  exactly the named top-level keys. An unknown field name is rejected
  server-side (`validation_failed`, exit code 10) — there is no
  client-side allow-list to keep in sync.
- `tickets ticket get <ref> --include comments,relationships` adds
  those keys to the response. Unlike `--fields`, `--include` has no
  server-side validation for an unrecognized name (the handler just
  checks two known keys and no-ops otherwise), so the CLI rejects a
  typo itself before making the request — see `runTicketGet` in
  `cmd/tickets/ticket.go`.
- `feature get`/`decision get` do not support either flag — the server
  doesn't implement `fields`/`include` outside the ticket endpoints
  today (`docs/contracts/representations.md`).
- `ticket list` accepts `--fields` but not `--include` — the list
  handler (`internal/httpapi/tickets.go`'s `listTickets`) never calls
  `includeNames`; expansion only exists on the single-ticket `getTicket`
  handler.

**A projected response is never decoded into the CLI's normal typed
struct.** `apiclient.GetTicket`/`ListTickets` return the full typed
`Ticket`/`TicketCompact`; decoding a `?fields=`-narrowed response into
those would silently zero-pad every excluded field (e.g. `status`
becoming `""`, indistinguishable from a real empty value), which is
worse than not supporting projection at all. `GetTicketFields`/
`ListTicketsFields` decode into `map[string]any` instead, and `--json`
prints exactly that map — the key set is exactly what was requested,
nothing more. Non-`--json` output renders the requested fields as
table columns, in the order given. `--include` alone (no `--fields`)
has no sane table form for its nested comments/relationships arrays,
so that combination prints JSON regardless of `--json`. `--fields`
combined with `--include` prints the `--fields` table instead — the
included sub-resources are still present in the response `apiclient`
receives, but the table render only shows the requested columns.

One consequence: `ticket list --fields ref,status` does **not** color
the `status` column the way plain `ticket list` does — the projected
render path doesn't know a given column is semantically a status
(`cmd/tickets/output.go`'s `writeProjectedRows`).

## `--no-color` / `NO_COLOR`

Table output colors the STATUS column (green for a settled-good state
like `done`/`accepted`, red for a settled-bad one like
`blocked`/`cancelled`/`rejected`, yellow for active work like
`in_progress`/`review`) and the PRIORITY/SEVERITY columns (red for
`critical`, yellow for `high`). Disabled automatically whenever any of
these hold: `--json` is set (a script consumer, and an escape sequence
must never leak into a parsed value), `--no-color` or `NO_COLOR` is
set (https://no-color.org's convention — any non-empty `NO_COLOR`
value disables it, `--no-color` always does regardless of value), or
stdout isn't a terminal (a pipe or redirect — checked via
`os.ModeCharDevice`, no terminal-detection dependency).

`cmd/tickets/output.go`'s `writeTable` computes column widths from
each cell's *visible* rune width (ANSI escapes stripped before
measuring), not its raw length — a colored cell is more bytes than
characters, and measuring raw length misaligns every column after it.
This replaced an earlier `text/tabwriter`-based implementation for
exactly that reason.

## Exit codes

`cmd/tickets/exit.go`'s `exitCode(domain.ErrorCode)`. `main.go` maps
both `*apiclient.Error` (a client-mode command's HTTP round trip) and
`*service.Error` (an `admin agent`/`admin token` command, which calls
`internal/service` directly with no HTTP hop) through the same table,
so a script gets one consistent vocabulary regardless of which kind of
command failed:

| Exit code | `domain.ErrorCode` |
| --- | --- |
| 10 | `validation_failed` |
| 11 | `not_found` |
| 12 | `already_exists` |
| 13 | `version_conflict` |
| 14 | `idempotency_key_reused` |
| 15 | `unauthorized` |
| 16 | `forbidden` |
| 17 | `relationship_cycle` |
| 18 | `has_dependents` |
| 19 | `throttled` |
| 20 | `upload_too_large` |
| 1 | Anything else, including `internal_error` and non-domain errors (a network failure, an unparseable response). |
| 2 | Usage error: unknown subcommand, flag parse failure, missing required flag/argument. |

`docs/contracts/errors.md`'s code catalogue is the source of truth
this table is derived from — a new code added there needs a row here
too, or it silently falls through to exit 1.

This table covers commands that go through `apiclient`/`service.Error`.
`tickets admin integrity` doesn't return a `domain.ErrorCode` — it
returns exit 1 for a plain `error` whenever it finds a genuine problem
(a failed `PRAGMA` check, a foreign-key violation, a corrupted blob, or
a `--gc` removal failure), the same generic-error exit code as anything
else in the "Anything else" row above. An orphan report without `--gc`
is informational and does not affect the exit code.

## Idempotency keys

`tickets comment add`/`tickets decision create`/`tickets project
create` accept an optional `--idempotency-key <key>`, forwarded as the
`Idempotency-Key` header (`docs/contracts/concurrency.md`). Reusing
the same key with identical arguments returns the original result
instead of creating a duplicate; reusing it with different content is
`idempotency_key_reused` (exit code 14). No other create command
exposes this flag today — it exists specifically for a caller that
owns its own retry loop and needs to replay the exact same key, which
a fresh CLI invocation (a new process) generally isn't; auto-generating
a key per call would not help a retried command, since the retry is a
different process with a different auto-generated key.

## `admin agent` / `admin token`

Structurally different from every other client-mode command: it opens
`internal/store` directly against a local data directory
(`--data-dir`, same resolution `tickets server` uses) rather than
talking to a remote server over `--url`. This is deliberate, not an
oversight — `internal/apiclient` has no session/CSRF support yet, so
there is no remote-server path for agent/token management in Phase 3,
and an MCP tool for it would be unenforced (`InProcessBackend` bypasses
`internal/httpapi`'s `requireAdmin` wrapper entirely). See
`cmd/tickets/admin_agent.go`'s package doc comment.

Because there's no bearer token or session to derive an actor from,
every mutating `admin agent`/`admin token` subcommand takes
`--as <actor>` instead — the human account performing the action
(`--as arlo`), or an explicit `kind:name` actor ref for scripts with no
human account to act as (`--as system:system`, the actor every
installation's migrations seed). `--as` rejects an agent actor
(product spec §4.1: a human creates and revokes agent identities, not
another agent).

`admin token create` prints the raw token exactly once, to stdout —
the same "shown once" rule `docs/contracts/concurrency.md`/ADR 0004
apply everywhere else a token is issued. It is not recoverable from
`admin token list`, which only ever shows `id`/`description`/
`created_at`/`expires_at`/`revoked_at`.

## JSON output

`--json` writes indented JSON via `encoding/json`'s default struct-tag
field order (`cmd/tickets/output.go`'s `writeJSON`) — stable across
runs for a given Go type, so a script can rely on key order without
re-sorting. A `?fields=`-projected response is the one exception, in
the opposite direction from what might be assumed: `GetTicketFields`/
`ListTicketsFields` decode into `map[string]any`, and `encoding/json`
always serializes a Go map with its keys sorted alphabetically —
`--fields title,ref` and `--fields ref,title` produce byte-identical
JSON (`ref` before `title` either way), regardless of the order given
on the command line. Only the non-`--json` table rendering
(`writeProjectedRow`/`writeProjectedRows`) honors the requested order,
since it builds columns straight from the `--fields` slice rather than
a map.
