# Backup and recovery

Two independent mechanisms (product spec §12). Use `admin backup`/
`admin restore` for disaster recovery on the same machine; use
`export`/`import` to move or archive a project's content independent
of any particular server installation.

Every operation on this page is also reachable from the web UI, for a
deployment with no host shell access (ADR 0027) — an admin session's
Maintenance page (Admin → Maintenance) for backup, restore, export,
and integrity/GC, and the first-run setup wizard's "Restore from an
export" path for import. The web restore path stages an uploaded
backup and applies it on the server's next restart, since a running
server cannot safely replace its own open database file; see ADR
0027 for why.

## `tickets admin backup` / `tickets admin restore`

`tickets admin backup --data-dir DIR --output OUT` writes a
self-contained snapshot of the database (via SQLite's `VACUUM INTO`,
safe to run against a live server) and the managed blob store to `OUT`,
plus a `manifest.json` recording per-file SHA-256 checksums, the server
version, and the schema version. `OUT` must not already exist.

`tickets admin restore --data-dir DIR --input OUT` verifies every
manifest checksum before touching anything, refuses a backup whose
schema version is newer than the running build supports, then swaps
the database and blob store into place.

**The server must not be running against `DIR` when you restore.**
`tickets server` writes a pidfile (`tickets.pid`) into its data
directory at startup and removes it on clean shutdown; `admin restore`
refuses if that pidfile is present. This is a presence check, not a
liveness probe — there is no portable way (Linux and Windows, per ADR
0003's pure-Go constraint) to confirm a PID is still alive. If a prior
crash left a stale pidfile behind, first confirm no `tickets server`
process is actually running against that data directory, then pass
`--force` to skip the check.

Restoring replaces the blob store wholesale, not merged — the result
is exactly the backup's state, not a mix of the backup and whatever was
written after it.

## `tickets export` / `tickets import`

`tickets export --data-dir DIR --output FILE` writes a versioned,
redacted JSON document covering the data directory's non-secret domain
content: entities, projects, features, tickets, decisions and their
version history, content items (plans/documents) and their version
history, comments and comment versions, attachments and attachment
versions, relationships, associations, derived mentions, external
links, audit events, subscriptions, notifications, and actor
identities. Password hashes, sessions, and agent token hashes are
never included.

`tickets import --data-dir DIR --input FILE [--commit]` validates the
export against `DIR` and, by default, writes nothing — `--commit` is
required to actually perform the import. Both modes run the same
validation and print the same report, so a dry run is not an
approximation of the real thing.

Import requires `DIR` to be an **empty** data directory: internal ids
are preserved verbatim from the export rather than remapped, so
importing into a database that already has content would either
collide on primary keys or silently attach imported data to unrelated
existing rows. To merge two installations' content, start a fresh data
directory and import into that.

An imported agent actor arrives without a working bearer token (agent
tokens are never exported) and needs a new one issued via
`tickets admin token create`. An imported human actor arrives without
a password too (`human_accounts` is deliberately never exported), but
`tickets admin account create --username <name> --password '...'`
closes that: `CreateHumanAccount` detects that the actor row already
exists with no matching `human_accounts` row and attaches the new
password to that existing actor instead of trying to create a second
one. Attempting it again after a password is already attached is a
normal `already_exists`, the same as creating any other duplicate
account.
