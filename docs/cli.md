# CLI user guide

A tour of `tickets`' client-mode commands — the ones that talk to a
running server over its HTTP API. This is a guided walkthrough for a
human running the CLI interactively or scripting it; the precise wire
contract (exact flags, exit codes, JSON shapes, config precedence) is
[`docs/contracts/cli.md`](contracts/cli.md) — that document is the
source of truth if the two ever disagree. `tickets help` also always
reflects the current command list.

Server administration (`server`, `setup`, `admin`) is covered in
[`docs/install.md`](install.md) and [`docs/admin.md`](admin.md), not
here — those commands open the data directory directly rather than
talking to a running server.

## Connecting

Client-mode commands (`project`, `feature`, `ticket`, `comment`,
`decision`, `plan`, `document`, `activity`, `attachment`, `link`,
`search`, `subscribe`/`unsubscribe`, `notifications`) resolve their
target server from, lowest to highest priority: a built-in default
(`http://127.0.0.1:8080/api/v1`), a client config file
(`TICKETS_CLIENT_CONFIG_FILE`, or `os.UserConfigDir()/tickets/client.json`),
environment variables, then flags:

```sh
# One-off, via flags:
tickets project list --url http://127.0.0.1:8080/api/v1

# Persistent, via a config file — same keys as the env vars below:
cat > ~/.config/tickets/client.json <<'EOF'
{ "api_url": "http://127.0.0.1:8080/api/v1", "project": "ABC" }
EOF

# Or via environment:
export TICKETS_API_URL=http://127.0.0.1:8080/api/v1
export TICKETS_PROJECT=ABC   # default --project when a command needs one but you omit it
```

**Bearer tokens never go on the command line** — there is no
`--token <value>` flag, deliberately (it would land in shell history
and `ps` output). Supply one via the client config file, the
`TICKETS_API_TOKEN` environment variable, or `--token-stdin` (reads
one line from stdin). Human commands authenticate with a session
instead — log in through the web UI, or see
[`docs/contracts/cli.md`](contracts/cli.md) if you need session-based
CLI auth details.

## A typical session

```sh
# Create a project (and its mandatory General feature).
tickets project create --key ABC --title "Widget Overhaul"

# Orient yourself — the same aggregation an agent's project_brief MCP
# tool call returns: in-progress/upcoming work, issue register
# highlights, features, recent activity, recent decisions/plans.
tickets project brief ABC

# Create a ticket.
tickets ticket create --project ABC --type task --title "Redesign the settings page" --priority high

# List, filtered and sorted the same way the web UI's priority queue is.
tickets ticket list --project ABC --view priority_queue

# Update with optimistic concurrency — --if-version guards against a lost update.
tickets ticket get ABC-1 --json | jq .version
tickets ticket update ABC-1 --status in_progress --if-version 1

# Comment, search, and explore relationships.
tickets comment add ABC-1 --body "Kicked off — see #ABC-2"
tickets search "settings page"
tickets ticket relate ABC-1 --type blocks --target ABC-2
tickets ticket relationships ABC-1

# Watch a reference for change notifications.
tickets subscribe ABC-1
tickets notifications list
```

## Output: table vs. `--json`

Every command defaults to a human-readable table (or a single-line
confirmation for a mutation). Pass `--json` for a script-friendly,
stable-key-order JSON document instead — this is what an agent using
the CLI JSON path (rather than MCP) should always use.

```sh
tickets ticket list --project ABC --json | jq '.[] | select(.priority == "critical")'
```

Table output colors the status/priority/severity columns unless
`--json` is set, `--no-color`/`NO_COLOR` is set, or stdout isn't a
terminal (piped or redirected) — so a script never has to strip ANSI
codes.

## Server-side field projection and expansion

`ticket get`/`ticket list` support `--fields` (return only the named
top-level keys) and `ticket get` alone supports `--include` (expand
`comments`/`relationships` inline):

```sh
tickets ticket list --project ABC --fields ref,title,status
tickets ticket get ABC-2 --include comments,relationships --json
```

No other entity's `get`/`list` supports either flag today — the
server doesn't implement projection outside the ticket endpoints. See
[`docs/contracts/cli.md`](contracts/cli.md) (the "`--fields` /
`--include`" section) for the
exact rendering rules (a `--fields` result is never decoded into the
normal typed struct, to avoid a projected-out field silently looking
like a real empty value).

## Idempotent retries

`comment add`, `decision create`, and `project create` accept
`--idempotency-key <key>`. Reusing the same key with identical
arguments replays the original result instead of creating a duplicate
— useful for a script with its own retry loop. Reusing it with
different arguments is an error (exit code 14).

```sh
tickets comment add ABC-2 --body "Deploying now" --idempotency-key deploy-2026-01-15
```

## Decisions, plans, and documents

Decisions and content items (plans/documents) carry full version
history:

```sh
tickets decision create --project ABC --title "Use Postgres for search" --decision-file decision.md
tickets decision versions ABC-D1
tickets decision diff ABC-D1 --from 1 --to 2

# Plans/documents can be Markdown, an uploaded file, a filesystem path
# reference, or a URL reference — see product spec §5.11 for the four
# representations. Only the uploaded-file representation supports download:
tickets plan create --project ABC --title "Q1 roadmap" --file roadmap.pdf
tickets plan download ABC-P1 --output roadmap.pdf
```

## Attachments and links

```sh
tickets attachment upload ABC-1 screenshot.png --title "Before/after screenshot"
tickets attachment list ABC-1
tickets attachment download <attachment-id> --output screenshot.png

tickets link add ABC-1 --title "Vendor incident" --link-url https://vendor.example/incident/123
tickets link list ABC-1
```

## Exit codes

Every client-mode command maps server errors to a stable exit code
(`validation_failed` → 10, `not_found` → 11, `version_conflict` → 13,
and so on), so a script can branch on `$?` instead of parsing stderr.
The full table is in
[`docs/contracts/cli.md`](contracts/cli.md#exit-codes).

```sh
tickets ticket update ABC-2 --status done --if-version 1
case $? in
  0) echo "updated" ;;
  13) echo "someone else changed it first — re-fetch and retry" ;;
  *) echo "failed" ;;
esac
```
