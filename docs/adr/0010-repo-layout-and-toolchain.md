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
  `api/openapi.yaml` (checked-in contract) · `web/` (embedded UI,
  placeholder `dist/` until Phase 4) · `docs/{adr,contracts,spikes}`.
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
