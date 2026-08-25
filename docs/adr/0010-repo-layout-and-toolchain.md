# 0010: Go version, module path, repo layout, embedded web assets

## Context

Phase 0 Step 1 had to fix a concrete Go version, module path, and
directory layout before any spike or contract work could start, since
every later step builds on them. This ADR records what was chosen and
why, for anyone joining after Phase 0.

## Decision

- **Go version:** 1.26.6, pinned identically in `go.mod`'s `go`
  directive on both Windows and WSL. Installed via `winget` on Windows
  and a direct tarball into `~/go-sdk` on WSL (no `sudo`, and
  explicitly *not* Ubuntu 22.04's `apt` `golang-go` package, which
  resolves to gccgo 1.16.5 — unusable).
- **Module path:** `github.com/ArloB/tickets`.
- **Layout:** `cmd/tickets` (single entry point, subcommand dispatch
  only) · `internal/{config,domain,store,service,httpapi,mcpsrv}` (one
  package per architectural boundary — see each package's `doc.go`) ·
  `api/openapi.yaml` (checked-in contract) · `web/` (embedded UI —
  Vite/React/TS scaffold as of Phase 4, see this ADR's Phase 4
  addendum below) · `docs/{adr,contracts,spikes}`.
- **Task runner:** `go-task` (`Taskfile.yml`), not `make` — neither
  `make` nor `task` was preinstalled on Windows, and `go-task` is
  cross-platform by design and installable identically via
  `go install` on both Windows and WSL, unlike `make` which needs a
  separate toolchain on Windows.
- **CI:** `.github/workflows/ci.yml` exists and mirrors the Taskfile
  targets exactly, but is dormant — no GitHub remote exists yet (a
  deliberate Phase 0 decision; see the Phase 0 implementation plan).
  Linux verification runs locally in WSL Ubuntu 22.04 via `task
  ci:linux`, which pulls the WSL clone before testing.
- **Line endings:** `.gitattributes` forces `eol=lf` on all text files.
  Without it, Windows `core.autocrlf` produces CRLF commits that break
  the moment WSL clones and diffs them.
- **`go:embed` placeholder:** `web/dist/index.html` is committed (with
  `.gitignore` carving out an exception for it inside an otherwise
  ignored `web/dist/*`) purely so `//go:embed all:dist` and `go build`
  succeed before Phase 4 produces a real Vite build.

## Consequences

- Any contributor's setup is exactly: install Go 1.26.6, `go install`
  `go-task` and `golangci-lint`, clone, `task ci`. No `make`, no
  Docker, no manual pragma or PATH surgery beyond what those installs
  already do.
- The Windows PATH gained `C:\Program Files\Go\bin` and
  `%USERPROFILE%\go\bin` (persisted via `[Environment]::SetEnvironmentVariable`,
  not just the current shell); WSL's `~/.profile` gained
  `~/go-sdk/bin`, `~/go/bin`, and (needed once Node/`npx` entered the
  picture for `task openapi`) `~/node-sdk/bin` ahead of the Windows
  interop path, since WSL's apt Node is v12 and its `npm`/`npx`
  otherwise silently cross into Windows via `/mnt/c`, which cannot
  resolve WSL's UNC home-directory path.

**Phase 4 addendum — the placeholder scheme changed shape, and a real
toolchain joined it.**

- `npm create vite@latest -- --template react-ts` scaffolded `web/`:
  React 19, Vite 8, TypeScript 6, oxlint (not ESLint — the
  scaffolder's current default; fast, zero-config), Vitest for unit
  tests. `web/src/api/` is the hand-written TS API client
  (`internal/apiclient`'s own doc comment gives the same reasoning for
  staying hand-written rather than OpenAPI-codegenerated).
- **The committed `web/dist/` placeholder moved from `index.html` to
  `.gitkeep`.** Once `task web:build`/`npm run build` actually
  produces content, a tracked `index.html` would show as a modified
  file in `git status` after every local build forever — `.gitkeep`
  (which `go:embed all:dist`'s `all:` prefix still picks up, being a
  dotfile) sidesteps that while keeping the same "bare `go build`/
  `go test` still compiles and passes" guarantee this ADR originally
  promised. `internal/httpapi`'s static handler returns a clear 500
  ("run `task web:build`...") when `index.html` is missing, rather
  than a confusing 404 — see `internal/httpapi/static.go`.
- **Go tests never depend on a real npm build having run.**
  `internal/httpapi`'s static-handler tests inject a small in-memory
  `fstest.MapFS` (`newStaticHandler`, not the package-level
  `staticHandler` that wraps the real `web.Dist`) rather than reading
  whatever happens to be in `web/dist/` locally — the same "Go-only
  contributor" guarantee this ADR's Consequences section already
  claimed for `go build`, now verified for `go test` too, after an
  earlier version of these tests briefly broke it by depending on
  build state.
- **`task build` now depends on `web:build`**, so `task ci`/`task
  build` always embed a real UI, never the empty placeholder — this is
  a real new requirement (Node/npm must be installed to run `task
  build`/`task ci`), but not a *new* one in practice: `task openapi`
  already required Node/`npx` since Phase 0, and both Windows and WSL
  setups already have it on `PATH` per this ADR's Consequences above.
  A bare `go build ./cmd/tickets` (no Task, no Node) still works,
  embedding whatever's already sitting in `web/dist/`.
- **Dev workflow:** `task web:dev` runs the Vite dev server, proxying
  `/api/v1` and `/healthz` to a separately-running `tickets server`
  (default `http://127.0.0.1:8080`, override via `TICKETS_DEV_API_URL`)
  — a same-origin proxy, not cross-origin `fetch`, because the session
  cookie is `SameSite=Lax` (ADR 0004) and a cross-origin dev request
  would silently drop it. `task web:install`/`web:lint`/`web:test`
  round out the individual steps `task ci` composes.

**Phase 6 Step 10 addendum — release archives.** `tools/release`
(a small `main` package, invoked by `task release`) cross-compiles
`linux/amd64`, `linux/arm64`, and `windows/amd64` binaries and
packages each into an archive (`.tar.gz` on Linux, `.zip` on Windows)
plus a `SHA256SUMS` manifest into `dist/` (already covered by
`.gitignore`'s `/dist/` pattern from Phase 0). It lives at the repo
root, a sibling of `cmd/`, not under `internal/` — it is build/release
tooling, not part of the shipped product, so it doesn't count against
this ADR's "`cmd/tickets` is the single entry point" claim, the same
distinction that already excludes `go test`/`task` themselves from
that claim. No extra toolchain is needed for any target: pure Go,
`CGO_ENABLED=0` set explicitly during cross-compilation, matching ADR
0003. `.github/workflows/release.yml` (tag-triggered, `push: v*`)
calls `task release` and uploads `dist/*` to the GitHub release —
dormant alongside `ci.yml` until a remote exists (see that workflow's
header for why).
