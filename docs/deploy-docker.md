# Running Tickets in Docker

An optional deployment path for teams that already run everything as
containers — see [ADR 0026](adr/0026-docker-packaging.md) for why this
doesn't change the "no Docker required to build/run from source"
guarantee in [`docs/install.md`](install.md). Same single binary, same
SQLite-backed data directory; the container just gives it a filesystem
and a network namespace.

## Quickstart

```sh
docker compose up -d
open http://127.0.0.1:8080
```

`docker-compose.yml` pulls the published `ghcr.io/arlob/tickets:latest`
image rather than building locally — see "Building the image
directly" below for a local build. The first visit to the URL above
walks through creating the admin account and, optionally, a first
agent token entirely in the browser (product spec §6.5's "first-run
setup" web view) — no shell access to the container needed, which
matters for anyone deploying through a GUI-only tool (Komodo,
Portainer, etc.) with no host CLI.

`tickets setup` (below) and `docker compose run --rm tickets setup
...` remain available for a scripted/headless bootstrap, and are
equivalent to the web wizard's first step — both ultimately call
`service.CreateAdminAccount`, which refuses either path once one human
account exists.

## Building the image directly

```sh
docker build \
  --build-arg VERSION=$(git describe --tags --always --dirty) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t tickets:local .
```

Or `task docker:build`, which fills in the same three build args from
the Taskfile's existing `VERSION`/`COMMIT`/`DATE` vars — the same
values `task build`/`task release` embed. Without them the image still
builds; `tickets --version` just reports `dev`/`none`/`unknown` (same
fallback as a bare `go build ./cmd/tickets`).

The image cross-compiles natively for both `linux/amd64` and
`linux/arm64` via buildx (`--platform`), matching the two Linux targets
`task release` already ships.

## Configuration

Every key in [`docs/admin.md`](admin.md)'s configuration table resolves
from `TICKETS_*` environment variables, so set them on the `tickets`
service in `docker-compose.yml` or via `docker run -e`. Two container
specific things to know:

- **`TICKETS_HOST` defaults to `0.0.0.0`** inside the image (baked in
  via `ENV`) — a container has no loopback anyone outside it can reach.
  Per `docs/admin.md`, anonymous read defaults on only when the bind
  host is loopback, so it defaults **off** here even though a bare
  loopback install would default it on. Set `TICKETS_ANONYMOUS_READ=true`
  explicitly if you want that convenience back, understanding that
  publishing the container's port makes anonymous read reachable by
  whoever can reach that port — doing so also makes the container log
  a startup warning, same as any other non-loopback bind with
  anonymous read enabled (see `docs/admin.md`).
- **`TICKETS_DATA_DIR` defaults to `/data`**, matching the volume
  mount in `docker-compose.yml`. Point a bind mount there instead of
  the named volume if you want the data directory on the host
  filesystem — the container runs as uid 10001, so a bind-mounted host
  directory needs to be writable by that uid (`chown 10001:10001` on
  the host, or `docker compose run --rm --user root tickets chown
  tickets:tickets /data` once).

TLS still isn't built in — the container speaks plain HTTP like every
other build. Put a reverse proxy in front for anything beyond a
trusted network (`docs/security-model.md`).

## Upgrading

```sh
docker compose pull   # or docker compose build, for a locally-built image
docker compose up -d
```

The named volume (or bind mount) persists across the recreate.
`internal/store.Open` runs pending schema migrations automatically on
the new image's first start, same as a bare-metal upgrade
(`docs/install.md`'s "Clean install vs. upgrade").

## Stopping

`docker compose stop`/`down` sends SIGTERM, which `tickets server`
handles identically to a bare-metal `Ctrl-C` — graceful shutdown,
pidfile removed. `docker-compose.yml` sets `stop_grace_period: 15s` so
Docker's own kill timeout doesn't race `--shutdown-timeout`'s 10s
default. A `docker kill` (or a stop that exceeds the grace period)
skips that and leaves a stale `tickets.pid` in the data directory — no
worse than a bare-metal crash; `tickets server` doesn't refuse to start
on a stale pidfile (only `admin restore` does).

## Backup and restore

`docker compose exec` reaches a shell if you have it, but the point of
this deployment path is that you might not. Every backup/restore/
export/integrity operation in `docs/backup-recovery.md` is reachable
from an admin session's Maintenance page instead — download a backup
before a risky change, or upload one to stage a restore. A staged
restore only applies on the container's next restart (`docker compose
restart`), since the running server can't safely replace its own open
database file; ADR 0027 has the full design.
