# Running Tickets in Docker

An optional deployment path for teams that already run everything as
containers — see [ADR 0026](adr/0026-docker-packaging.md) for why this
doesn't change the "no Docker required to build/run from source"
guarantee in [`docs/install.md`](install.md). Same single binary, same
SQLite-backed data directory; the container just gives it a filesystem
and a network namespace.

## Quickstart

```sh
docker compose up -d --build
docker compose run --rm tickets setup --username admin --password <a real password>
open http://127.0.0.1:8080
```

`docker compose run --rm tickets setup ...` runs `tickets setup`
against the same `/data` volume the `tickets` service uses — it opens
the data directory directly (same as bare metal), so it works whether
or not the `tickets` service is already up.

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
