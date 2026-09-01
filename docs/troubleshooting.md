# Troubleshooting

Common failure modes and how to diagnose them, grouped by symptom.
When a message below cites an exit code or error code, see
[`docs/contracts/errors.md`](contracts/errors.md) and
[`docs/contracts/cli.md`](contracts/cli.md#exit-codes) for the full
catalogue.

## Server won't start

**`listen on 127.0.0.1:8080: ... address already in use`**
Another process (maybe a previous `tickets server` that didn't shut
down cleanly) is bound to that port. Find and stop it, or start this
one with `--port` pointed elsewhere.

**Startup takes far longer than ~2 seconds, or hangs**
Not normal (product spec §11's cold-start target). Check whether the
data directory sits on a network filesystem — `internal/store` prints
a best-effort warning (never refuses) if it detects one, since SQLite's
locking is unreliable there; move the data directory to local disk.

**The web UI is blank, or the server returns a 500 with a message
mentioning `task web:build`**
The binary was built without a real web UI embedded (`go build` with
only the placeholder `web/dist/.gitkeep` present, not
`task build`/`task web:build`). Run `task build` (or `task web:build`
then `go build ./cmd/tickets`) to embed a real production build — see
[`docs/install.md`](install.md#building-from-source).

**`store: database schema version N is newer than this build supports
(max M)`**
The data directory was previously opened by a newer `tickets` build
than the one you're running now. This is a deliberate refusal (product
spec §8.3) — the running build genuinely doesn't know that schema.
Upgrade to a build at least that new, or point `--data-dir` elsewhere.

## Setup and login

**`setup: --username or TICKETS_ADMIN_USERNAME is required (setup
never prompts)`**
`tickets setup` is non-interactive by design (product spec §7.3) — it
never prompts even at a terminal. Pass `--username`/`--password` or set
the corresponding environment variables.

**`tickets setup` fails with "a human account already exists"**
Setup refuses to run a second time once *any* human account exists —
by design, it only ever creates the one account (product spec's
first-run setup). For every account after the first, use `tickets
admin account create` (Phase 7) instead — see `docs/admin.md`.
Agents get their own identities via `tickets admin agent create` +
`tickets admin token create` instead — this limitation is about human
accounts specifically, and it no longer applies: a second (or later)
human account, and a password change for any account, both have a
real path now.

**Login is rejected repeatedly, even with the right password**
The DB-persisted login throttle (survives a restart) kicks in after
enough failed attempts in a trailing window and resets once the window
ages out — wait, or confirm the password is actually correct before
retrying rapidly, since retrying doesn't reset the window.

**A mutating request is rejected with `unauthorized`/`forbidden`
despite a valid session**
Check you're sending `X-CSRF-Token` on every mutating request — a
session cookie alone is never sufficient (`docs/security-model.md`).
The token comes from the login response; it doesn't change until you
log in again.

## Restore and backup

**`admin restore` refuses with a message about the server running**
`tickets admin restore` checks for the data directory's `tickets.pid`
file, written at server startup and removed on clean shutdown. If a
`tickets server` process really is running against that data
directory, stop it first. If it crashed and left a stale pidfile
behind, confirm no process is actually running (check your process
list, not just the pidfile), then re-run with `--force`. Restoring
while the server is live under WAL would corrupt the live database —
this check exists specifically to prevent that. See
[`docs/backup-recovery.md`](backup-recovery.md).

**`restore: backup schema version N is newer than this build supports`**
Same class of refusal as the server-startup one above, applied to a
backup instead of the live database — the backup was taken by a newer
build. Restore with a build at least that new.

**`admin restore` refuses with a checksum mismatch**
The backup's `manifest.json` recorded a SHA-256 for every file that no
longer matches what's on disk — the backup directory was modified or
corrupted after `admin backup` wrote it. Active state is left
untouched when this happens; re-run `admin backup` from a known-good
source, or restore from an older backup.

**`import` reports invalid references or refuses to commit**
`tickets import` runs the same validation whether or not `--commit` is
passed — a dry run is not an approximation. Read the printed report;
it names the specific reference collisions or invalid relationships.
Remember `import` requires an **empty** data directory (ids are
preserved verbatim, not remapped) — start a fresh `--data-dir` if
you're merging content from two installations.

**An imported agent can't authenticate**
Expected — agent tokens are never exported (secret redaction, see
[`docs/security-model.md`](security-model.md)). Issue a new token with
`tickets admin token create` after import.

## Everyday operations

**`version_conflict` (exit code 13) on an update**
Someone else changed the record since you last read it. Re-fetch
(`ticket get ABC-2 --json | jq .version`) and retry with the current
version in `--if-version`/`If-Version`. This is optimistic concurrency
working as intended, not a bug — see
[`docs/contracts/concurrency.md`](contracts/concurrency.md).

**`idempotency_key_reused` (exit code 14)**
The same `--idempotency-key`/`Idempotency-Key` was sent with different
content than the first call that used it. Use a new key for genuinely
different content, or confirm you're replaying the exact same
arguments if you expected a safe retry.

**`upload_too_large` (exit code 20)**
The attachment exceeds `--max-upload-bytes` (25 MiB default). Raise
the limit server-side (`docs/admin.md`) if the file is legitimately
larger, or split/compress it.

**Search results look stale or wrong**
Run `tickets admin search-reindex` — it clears and rebuilds the FTS5
index from scratch from the current database state. This is the
documented recovery path if the incremental index-update path ever
misses an entry; it's always safe to run.

**Suspected data corruption, or you just want a health check**
Run `tickets admin integrity` (add `--json` for scripting). It reports
`PRAGMA integrity_check`/`PRAGMA foreign_key_check` failures, corrupted
attachment blobs (checksum mismatch), and orphaned blobs — add `--gc`
to reclaim orphans (only ones older than an hour, to avoid racing a
mid-upload). See [`docs/admin.md`](admin.md#integrity).

## Networking and security warnings

**`WARNING: anonymous read access is enabled on a non-loopback bind ...` at startup**
You started `tickets server --host` with something other than
`127.0.0.1`/`localhost`/`::1`, with `anonymous_read` enabled — the one
combination reachable without credentials, so this warning fires only
then, never for a non-loopback bind with anonymous read off. Expected
when intentionally sharing the server on a LAN, but read
[`docs/security-model.md`](security-model.md) first — Tickets has no
built-in TLS, so put a reverse proxy in front, and reconsider whether
`anonymous_read` should really be enabled on that bind.

**A log line warns about a bearer token over plaintext HTTP**
`warnIfInsecureBearer` logs (doesn't block) whenever a bearer token
arrives over a non-loopback, non-TLS connection — the token could be
sniffed on the network. Put TLS in front if the server isn't loopback-
only.

## Still stuck

Check `internal/httpapi`'s response — every error includes a
`correlation_id`; a matching structured log line (`--log-format json`)
often has more context than the message alone. If you believe you've
found an actual bug rather than a documented refusal above, it's worth
filing as an issue with the correlation id, the exact command/request,
and the server's log output around that timestamp.
