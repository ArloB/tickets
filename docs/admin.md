# Administration

Configuration keys for `tickets server`, and every `tickets admin`
subcommand. `docs/backup-recovery.md` covers `admin backup`/
`admin restore`/`export`/`import` in more depth; this document covers
the rest plus the config surface.

## Configuration

`internal/config.Load` resolves settings from four layers, lowest to
highest priority: built-in defaults, an optional JSON config file,
`TICKETS_*` environment variables, then command-line flags. There is
deliberately no `--config` flag — redirect the file itself with
`TICKETS_CONFIG_FILE` instead (see below).

| Key | Flag | Env var | Config file key | Default | Notes |
| --- | --- | --- | --- | --- | --- |
| Data directory | `--data-dir` | `TICKETS_DATA_DIR` | `data_dir` | `os.UserConfigDir()/tickets` | SQLite database + managed blob storage live here. |
| Bind host | `--host` | `TICKETS_HOST` | `host` | `127.0.0.1` | Non-loopback with anonymous read enabled prints a warning at startup. |
| Bind port | `--port` | `TICKETS_PORT` | `port` | `8080` | |
| Anonymous read | `--anonymous-read` | `TICKETS_ANONYMOUS_READ` | `anonymous_read` | enabled only when `--host` is loopback | Product spec §4.2. Every `routeViewer` (GET) route is reachable with no credentials when enabled; every mutating route still requires at least Editor. See `docs/security-model.md`. |
| Log format | `--log-format` | `TICKETS_LOG_FORMAT` | `log_format` | `console` | `console` (human-readable) or `json`; any other value is rejected at startup. |
| Shutdown timeout | `--shutdown-timeout` | `TICKETS_SHUTDOWN_TIMEOUT` | `shutdown_timeout` | `10s` | How long graceful shutdown waits for in-flight requests before giving up. Config file value is a Go duration string (`"10s"`); env/flag accept the same. |
| Max upload size | `--max-upload-bytes` | `TICKETS_MAX_UPLOAD_BYTES` | `max_upload_bytes` | `26214400` (25 MiB) | Per-version attachment upload size limit (ADR 0007). |

The config file's location is `TICKETS_CONFIG_FILE` if set, otherwise
`os.UserConfigDir()/tickets/config.json`. A missing file is not an
error — it's entirely optional. Example:

```json
{
  "data_dir": "/srv/tickets/data",
  "host": "0.0.0.0",
  "anonymous_read": false,
  "log_format": "json"
}
```

An absent key in the file means "no opinion" — it never overrides a
value already resolved from a lower-priority layer with a zero value
(e.g. omitting `"port"` doesn't reset the port to `""`).

**Binding to a non-loopback host with anonymous read enabled** prints a
warning to stderr — anyone who can reach that address can read every
project unauthenticated. A non-loopback bind with anonymous read off
prints nothing, since every route still requires authentication either
way. See [`docs/security-model.md`](security-model.md) before binding
non-loopback outside a trusted network, and put TLS in front (Tickets
itself speaks plain HTTP only).

## `tickets admin` subcommands

All `admin` subcommands accept `--data-dir` (defaults to the same
resolution `tickets server` uses) and open the data directory
directly — they do not talk to a running server over HTTP. Most also
accept `--json` for machine-readable output.

### `search-reindex`

```sh
tickets admin search-reindex [--data-dir DIR]
```

Clears and rebuilds the FTS5 search index from scratch
(`store.RebuildSearchIndex`). The documented recovery path if the
incremental index-update path ever misses or corrupts an entry. Prints
`reindexed N search document(s)`.

### `integrity`

```sh
tickets admin integrity [--data-dir DIR] [--gc] [--json]
```

Runs `PRAGMA integrity_check`, `PRAGMA foreign_key_check`, and a
blobstore verify/orphan sweep in one report: every attachment/content
version's checksum is confirmed against the bytes actually on disk,
and every blob on disk is confirmed to be referenced by something.

- Without `--gc`, orphaned blobs are only reported (informational,
  does not affect the exit code).
- With `--gc`, orphaned blobs older than one hour are deleted. The
  one-hour floor exists because a blob write can commit to the
  blobstore moments before its owning database row commits (ADR
  0007's Consequences) — a very recent "orphan" may just be a
  mid-upload, not abandoned.
- A corrupted blob (bytes no longer match their own hash) is always
  reported, never auto-removed by `--gc` — the bytes might still be
  partially recoverable, unlike a genuine orphan.
- Exit code is non-zero only for a genuine finding: a failed `PRAGMA`
  check, a foreign-key violation, a corrupted blob, or a `--gc`
  removal failure. An orphan report alone does not fail the command.

### `backup` / `restore`

See [`docs/backup-recovery.md`](backup-recovery.md).

### `purge-idempotency-keys`

```sh
tickets admin purge-idempotency-keys [--data-dir DIR] [--older-than 720h]
```

Deletes idempotency keys older than the given duration (default 30
days, matching `docs/contracts/concurrency.md`'s bounded-retention
intent). `idempotency_keys` retention is otherwise unbounded — run
this periodically (e.g. a scheduled task) on a long-lived install.

### `agent` / `token`

Agent identity and bearer token management. Deliberately CLI-only,
local-store-only — there is no HTTP route, MCP tool, or remote
(`--url`) form for these, because the trust model differs from every
other admin action: see `cmd/tickets/admin_agent.go`'s package doc
comment. Every mutating subcommand takes `--as <actor>` — the human
account performing the action (product spec §4.1: a human creates and
revokes agent identities, never another agent).

```sh
tickets admin agent create --name ci-bot --description "release pipeline" --as arlo
tickets admin agent list
tickets admin agent get ci-bot

tickets admin token create ci-bot --description "prod deploy" --expires-in 720h --as arlo
tickets admin token list ci-bot
tickets admin token revoke <token-id> --as arlo
```

`admin token create` prints the raw bearer token to stdout **exactly
once** — it is not logged and cannot be retrieved again.
`admin token list` shows only `id`/`description`/`created_at`/
`expires_at`/`revoked_at`, never the token value. Hand the printed
token to the agent's MCP client config (`tickets mcp --token ...`) or
`TICKETS_API_TOKEN` immediately.

`--as` accepts a bare name (treated as a human account, e.g. `arlo`)
or an explicit `kind:name` actor ref for scripts with no human account
to act as (`--as system:system`, the actor every installation's
migrations seed). It rejects an agent actor.

### `account`

Human account management (Phase 7 — product spec §4.2/§13). Like
`agent`/`token`, this is CLI-only, local-store-only:
`internal/apiclient` has no session/CSRF support, so there is no
remote-server (`--url`) path for admin operations, only direct-to-store
administration by whoever has CLI/filesystem access to the data
directory — the same trust boundary `tickets setup` already uses.
`tickets setup` still creates the very first account (a one-time
bootstrap outside `admin account`); every account after that goes
through `admin account create`.

```sh
tickets admin account create --username bob --password '...' --as arlo
tickets admin account create --username ops --password '...' --admin --as arlo
tickets admin account list
tickets admin account change-password --username bob --new-password '...' --as arlo
```

`admin account change-password` resets a password **without** asking
for the current one — this is the operator/admin reset path, the
CLI/local-store counterpart to `POST /api/v1/accounts/{username}/password`
run by an admin session. A logged-in human changing their own password
uses that same HTTP route instead, self-service (the web UI's Accounts
page, or a direct call with `old_password` set) — there is no CLI
self-service form, since the CLI's admin path doesn't authenticate as
a specific logged-in user at all. Either path invalidates every
existing session for that account immediately.

`--admin` grants the operational admin flag (product spec §4.2) at
account creation. There is no separate promotion command: the flag is
set once, at creation, and stays fixed for that account's lifetime.

## Health and version

```sh
curl http://127.0.0.1:8080/healthz   # 200 once the server and database are reachable
tickets --version                     # build version, commit, date (real values only from `task build`/`task release`)
```
