# Installing and running Tickets

Tickets is a single Go binary with the web UI embedded. It runs on
Linux and Windows (ADR 0003's pure-Go SQLite driver, `modernc.org/sqlite`
— no CGO, so no C toolchain is needed on either platform, and
cross-compilation needs nothing extra).

## Building from source

Prerequisites: Go 1.26.6 (pinned in `go.mod`), and Node/npm if you want
a real embedded web UI rather than the empty placeholder (see below).

```sh
git clone https://github.com/ArloB/tickets.git
cd tickets
go install github.com/go-task/task/v3/cmd/task@latest   # optional but recommended
task build                                                # builds bin/tickets, embeds the real web UI
```

`task build` depends on `task web:build` (`npm install` + a production
Vite build into `web/dist`), so the binary it produces has a working
web UI and a real version/commit/date recorded via `-ldflags` (see
`tickets --version`).

### Building release archives for another platform

`task release` cross-compiles archives for every released target
(`linux/amd64`, `linux/arm64`, `windows/amd64`) from whatever host
you run it on — pure Go, no CGO anywhere in this module, so producing
a Windows binary from Linux (or vice versa) needs nothing beyond the
Go toolchain already installed for `task build`. Output goes to
`dist/`: one `.tar.gz` (Linux) or `.zip` (Windows) per target, plus a
`SHA256SUMS` manifest checkable with `sha256sum -c SHA256SUMS`.

A bare `go build ./cmd/tickets` also works with no Task and no Node —
it embeds whatever is already sitting in `web/dist/` (the placeholder
if you've never run `web:build`) and reports `dev`/`none`/`unknown` for
version/commit/date instead of real values, since those are only set
via `task build`'s `-ldflags`. This is deliberate (ADR 0010's
"Go-only contributor" guarantee) — useful for a quick `go test ./...`
loop, not for a binary you intend to run day to day.

There are no other runtime dependencies: no external database, no
message queue, no Docker. The data directory (SQLite database +
managed blob storage) is the only state.

## Running the server

```sh
tickets server
```

Defaults: binds `127.0.0.1:8080`, and the data directory is
`os.UserConfigDir()/tickets` (`~/.config/tickets` on Linux,
`%AppData%\tickets` on Windows). See [`docs/admin.md`](admin.md) for
every configuration key, precedence, and the `--data-dir`/`--host`/
`--port`/`--anonymous-read`/`--log-format` flags.

`tickets server` writes a pidfile (`tickets.pid`) into the data
directory at startup and removes it on clean shutdown
(`Ctrl-C`/`SIGTERM`, which triggers a graceful shutdown bounded by
`--shutdown-timeout`). `tickets admin restore` refuses to run while
that pidfile is present — see
[`docs/backup-recovery.md`](backup-recovery.md).

## First-run setup

Before the server is useful, create the one admin account:

```sh
tickets setup --username admin --password <a real password>
```

`tickets setup` is non-interactive by design — it never prompts, even
at a terminal (product spec §7.3). Credentials come from
`--username`/`--password` flags or `TICKETS_ADMIN_USERNAME`/
`TICKETS_ADMIN_PASSWORD` environment variables (useful for scripted
provisioning without the password landing in shell history). It opens
the data directory directly, so it can run before the server is
started, and refuses if a human account already exists — safe to leave
out of a restart script, but not safe to run twice expecting a second
admin.

## Clean install vs. upgrade

- **Clean install:** point `--data-dir` at an empty (or nonexistent —
  it's created) directory and start the server. Startup normally
  completes in under 2 seconds (product spec §11).
- **Upgrade:** stop the old binary, replace it with the new one, start
  it again against the same `--data-dir`. `internal/store.Open` runs
  any pending schema migrations automatically and, before applying the
  first one, takes a timestamped pre-migration snapshot
  (`VACUUM INTO`) into the data directory as a safety net — see
  `internal/store/premigration_backup_test.go`'s recovery drill and
  [`docs/backup-recovery.md`](backup-recovery.md). A newer database
  than the running build supports is refused outright rather than
  risking a downgrade against unknown schema.

Both paths work identically on Windows and Linux, including data
directory paths containing spaces and non-ASCII characters — verified
by `cmd/tickets/coldstart_test.go` run natively on both platforms (see
`docs/mvp-acceptance.md` row 1).

## Platform notes

- **Linux/Windows only** — these are the two target platforms (product
  spec §15) and the only ones tested or supported. Other platforms are
  unsupported.
- **No installer/service unit is provided.** Run `tickets server`
  under whatever process supervisor your platform normally uses
  (systemd, a Windows service wrapper, a container runtime, or just a
  terminal for personal use) — this is deliberately out of scope. A
  Docker image is available for teams that already run everything as
  containers; see [`docs/deploy-docker.md`](deploy-docker.md) and ADR
  0026. It doesn't change the guarantee above: building and running
  from source still needs nothing beyond Go (and Node for a real web
  UI) — Docker is one way to *run* a built image, not a new
  requirement to *build* one.
- **TLS is not built in.** `tickets server` speaks plain HTTP. For
  anything beyond a loopback-only personal install, put a reverse
  proxy (nginx, Caddy, etc.) in front for TLS — see
  [`docs/security-model.md`](security-model.md) for why this matters
  once a bearer token is in play.

## Verifying an install

```sh
tickets --version          # build version/commit/date
curl http://127.0.0.1:8080/healthz   # 200 once the server is up
```
