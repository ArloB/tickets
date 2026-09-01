# 0026: Docker packaging as an optional deployment path

## Context

ADR 0010 recorded "no Docker" as a contributor-setup guarantee: building
and running Tickets from source needs only Go (and Node for a real web
UI), never a container runtime. `docs/install.md` and the README repeat
this as a selling point — one static binary, one SQLite data directory,
no external services.

That guarantee is about what building/running *requires*, not about
what deployment options exist. Teams that already run everything as
containers (Kubernetes, Nomad, `docker compose`-based homelabs) asked
for an image so Tickets fits their existing deploy pipeline without
them hand-rolling a Dockerfile against an undocumented build.

## Decision

- Add a multi-stage `Dockerfile` at the repo root: a Node stage builds
  `web/dist` (mirrors `task web:build`), a Go stage cross-compiles the
  binary with the same `internal/buildinfo` ldflags `task build`/`task
  release` already use (`VERSION`/`COMMIT`/`DATE` build args), and an
  Alpine runtime stage carries only the binary, `ca-certificates`,
  `tzdata`, and a non-root `tickets` (uid 10001) user. `CGO_ENABLED=0`
  throughout (ADR 0003), so the same source cross-compiles `linux/amd64`
  and `linux/arm64` natively via `--platform=$BUILDPLATFORM` +
  `GOARCH=$TARGETARCH`, matching the architectures `task release`
  already ships — no QEMU emulation needed for either build stage.
- Alpine (not `scratch`/distroless) for the runtime stage: it gives the
  image a shell for `docker exec` debugging and `wget` for the
  `HEALTHCHECK`, at the cost of a larger image than a fully static
  distroless build. A pure-Go static binary doesn't need libc either
  way, so this is a debuggability trade, not a build-correctness one.
- `docker-compose.yml`: one service, a named volume at `/data`
  (`TICKETS_DATA_DIR`), port 8080 published, `stop_grace_period: 15s`
  (`--shutdown-timeout` defaults to 10s — see Consequences), and a
  healthcheck hitting `/healthz`.
- `.github/workflows/docker.yml`: tag-triggered (`push: v*`, mirroring
  `release.yml`), builds and pushes
  `ghcr.io/arlob/tickets:<version>`/`:latest` for both architectures via
  `docker/build-push-action`. Third-party actions
  (`docker/{login,metadata,build-push}-action`) are a deliberate choice
  here, the same exception `release.yml` already made for
  `softprops/action-gh-release`.
- `docs/deploy-docker.md` covers building, first-run `admin setup`,
  data persistence, and upgrading via the image.
- Two `Taskfile.yml` targets, `docker:build` and `docker:up`, so the
  image can be built/run through the same `task` entry point as
  everything else — both assume a Docker daemon is already reachable
  (WSL on this project's Windows dev machines; native on Linux CI).

## Consequences

- **ADR 0010's contributor-setup guarantee is unchanged.** `go build
  ./cmd/tickets` and `task ci` still need nothing beyond Go (+ Node for
  a real UI) — Docker is an additional way to *run* a built image, not
  a new requirement to *build* one.
- **`TICKETS_HOST=0.0.0.0` is baked into the image**, since a container
  has no other way to accept traffic from outside itself. Per
  `docs/admin.md`, anonymous read defaults on only when the bind host
  is loopback — so it defaults **off** in the container even though a
  bare-metal loopback install would default it on. Anyone who wants the
  bare-metal default back sets `TICKETS_ANONYMOUS_READ=true` explicitly,
  understanding the exposure that implies once the port is published.
- **No TLS, no installer/service unit, still true.** The image speaks
  plain HTTP like every other build; a reverse proxy in front is still
  the answer for anything beyond a trusted network, same as
  `docs/install.md`'s existing guidance.
- **A container's default stop signal is SIGTERM**, which
  `cmd/tickets/server.go`'s `signal.NotifyContext` already handles
  identically to a bare-metal `Ctrl-C` — graceful shutdown and pidfile
  cleanup both run. Only a SIGKILL (`docker kill`, or a stop that
  outlives its grace period) skips that, leaving a stale
  `tickets.pid` — no worse than a bare-metal crash, and `tickets
  server` itself doesn't refuse to start on a stale pidfile (only
  `admin restore` does; see `docs/backup-recovery.md`), so a killed
  container still restarts cleanly.
- **Registry name is lowercase** (`ghcr.io/arlob/tickets`) — GHCR
  rejects uppercase repository paths even though the Go module path is
  `github.com/ArloB/tickets`.
