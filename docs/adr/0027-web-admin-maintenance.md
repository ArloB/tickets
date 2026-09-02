# 0027: Backup, restore, export, integrity, and import reachable from the web UI

## Context

`tickets admin backup`/`admin restore`, `tickets export`/`import`, and
`tickets admin integrity [--gc]` (docs/backup-recovery.md, product
spec §12) were CLI-only. A Komodo-style deployment with no host shell
access — the same gap the web setup wizard (`/setup`) exists to close
for first-run bootstrap, see `docs/deploy-docker.md` — has no way to
run any of them: no backup before a risky change, no way to recover
from a bad state short of deleting the whole volume, no export, no
integrity check.

Two design questions had to be settled before writing any handler:

1. **A running server cannot safely replace its own open database
   file**, especially on Windows where an open file cannot be renamed
   or overwritten out from under the process holding it. `admin
   restore`'s own precondition (refuse unless the target's pidfile is
   absent, or `--force`) assumes the operator runs it from outside the
   server process entirely — an assumption a web request handler
   inside that same process cannot make on its own behalf.
2. **Where do these handlers live?** `internal/service.Service`'s own
   doc comment states it is "the single authorization/validation/
   transaction/audit/idempotency boundary shared by internal/httpapi
   and internal/mcpsrv" — the natural first instinct is to add
   `Service.Backup`/`Service.Restore`/etc. That instinct produced an
   import cycle: `internal/backup`'s own test suite already imports
   `internal/service` to build realistic fixtures (create a project
   through the real service, then back it up), so `internal/service`
   importing `internal/backup` back makes `internal/backup`'s test
   binary import itself through `internal/service`. That's not a build
   quirk to route around — `internal/backup` already sits *above*
   `internal/service` in this codebase's layering (it composes a data
   directory, `internal/store`, and `internal/blobstore` into whole-
   installation operations), so `internal/service` depending on it
   inverts that.

## Decision

- **Stage-on-upload, apply-on-restart for restore.** `POST
  /api/v1/admin/restore` accepts a zip of the same directory shape
  `admin backup` produces, extracts and validates it immediately
  (`backup.ValidateBackupDir` — the same manifest/checksum check
  `Restore` itself runs first, factored out so upload-time validation
  and restore-time validation can never drift), and — only once that
  passes — moves the extracted directory into place at
  `<data-dir>/.pending-restore/`. It does not touch the live database
  or blob store. `cmd/tickets/server.go`'s `runServer` checks for that
  directory *before* `store.Open`, and if present, runs the real
  `backup.Restore` against it there — the one place in the process
  where nothing else has the database open yet.
- **A failed apply must not crash-loop the server.** The alternative —
  `runServer` returning an error and the process exiting non-zero — is
  fine on a bare-metal host where the operator is watching, but under
  Docker's `restart: unless-stopped` (this project's own
  `docker-compose.yml`, ADR 0026) it becomes a boot loop: restart, hit
  the same broken `.pending-restore`, exit, restart, forever — with no
  shell to intervene, exactly the scenario this whole feature exists
  to serve. Since `admin restore`'s own precondition already validates
  the archive at upload time, an apply-time failure means something
  changed between staging and restart (disk corruption, a partial
  write) — rare, and not something retrying the same restart fixes.
  So a failed apply moves `.pending-restore` to
  `.pending-restore.failed`, records the error in
  `.pending-restore-error.txt`, logs it, and lets startup continue
  normally on the pre-restore data. `GET /api/v1/admin/restore`
  surfaces that state so the web UI can show it; `DELETE
  /api/v1/admin/restore` dismisses the notice.
- **`internal/httpapi` calls `internal/backup` (and, for these routes
  only, `internal/store`/`internal/blobstore`) directly, not through
  `internal/service`.** This is a deliberate, narrow exception to
  `Service`'s doc comment, not an erosion of it: every operation this
  ADR adds — a whole-database `VACUUM INTO`, a wholesale blob-store
  swap, an integrity sweep, an import into an empty database — has
  none of the five properties `Service` exists to centralize.
  Authorization is `routeAdmin`'s job at the HTTP layer (nothing here
  needs entity-scoped or actor-scoped checks); there is no per-row
  transaction to manage (`backup.Backup`/`Restore`/`Import` already
  manage their own); nothing here emits an `audit_events` row (these
  aren't domain mutations product spec §5.12 tracks); and none of it
  is idempotency-key-gated (`docs/contracts/concurrency.md`'s
  idempotency story is about ticket/feature/comment writes). Routing
  it through `Service` anyway would have meant `Service` re-exposing
  the `internal/store`/`internal/blobstore` handles it otherwise keeps
  private, for no safety property in return — the import-cycle
  discovery above is what actually forced this to be looked at
  directly rather than assumed. `internal/httpapi`'s new handlers
  (`admin_backup.go`, `admin_restore.go`, `admin_export.go`,
  `admin_integrity.go`, `setup_import.go`) open their own
  `store.Open(dataDir)`/`blobstore.Open(dataDir)` per call, the same
  pattern `backup.Backup` itself already uses internally rather than
  assuming an existing connection. `httpapi.SetDataDir` (a package-
  level var + setter, mirroring the already-established
  `SetLogger`/`SetMaxUploadBytes` pattern in this same package) is how
  `cmd/tickets/server.go` gives these handlers the path to open.
- **Export is a zip only when attachments are requested; otherwise
  plain JSON.** `GET /api/v1/admin/export` defaults to streaming the
  envelope directly as `application/json` (matching `tickets export`
  with no `--attachments`); `?attachments=true` instead builds a temp
  directory containing `envelope.json` plus the copied blob tree and
  zips that (matching `tickets export --attachments DIR`).
- **`admin integrity`'s report-building logic moved from `cmd/tickets`
  into `internal/backup`** (`integrity.go`, exported as
  `BuildIntegrityReport`/`IntegrityReport`) so the CLI command and the
  new `GET /api/v1/admin/integrity` handler share one implementation
  rather than the web route reimplementing the PRAGMA/blobstore-verify
  sweep. `--gc`'s web equivalent, `POST /api/v1/admin/integrity/gc`,
  requires a JSON body of `{"confirm": true}` — a deliberate, separate
  step from the read-only report, matching this codebase's existing
  stance that GC is an operator action, not an automatic one.
- **Import is folded into the setup wizard as an alternative first
  step**, not a separate admin-only route: `POST /api/v1/setup/import`
  is unauthenticated, like `POST /api/v1/setup` itself, and for the
  same reason — requiring credentials to bootstrap the credentials
  would be circular. It stays safe unauthenticated for exactly the
  reason `/setup` already is: `backup.Import`'s own `checkTargetEmpty`
  refuses to commit (reporting `Problems` in a 200 response, not an
  error) unless `entities` is empty and `actors` holds only the two
  seeded rows — true only in the narrow first-boot window before any
  project or admin account exists, and never true again afterward.
  `service.CreateAdminAccount`'s existing attach-or-create logic
  (`accountStateForUsername`) already handles the resulting case where
  the wizard's next step, admin account creation, needs to attach a
  password to an actor the import just created rather than erroring or
  duplicating it.
- **Browser downloads of admin-only routes need a real `fetch`, not an
  `<a href>`.** `internal/httpapi/auth_middleware.go`'s `requireEditor`
  checks `X-CSRF-Token` on every session-authenticated request it
  wraps regardless of HTTP verb, and `routeAdmin` composes it — so a
  plain link to `/api/v1/admin/backup` 403s for a signed-in browser
  session. `web/src/api/client.ts` gained `apiFetchBlob` (GET, returns
  a `Blob`) and `apiFetchRaw` (arbitrary-body POST/DELETE with a JSON
  response) alongside the existing `apiFetch`/`apiFetchMultipart`, both
  attaching the CSRF header the same way; `AdminMaintenance.tsx`
  triggers the actual browser save via a `URL.createObjectURL` +
  synthetic `<a>` click, the standard pattern for a fetch-obtained blob.

## Consequences

- **A staged restore directory living at `<data-dir>/.pending-
  restore/` cannot collide with `blobstore.Open`/`store.Open`**:
  `blobstore.Open(dataDir)` roots itself at `dataDir/blobs` specifically
  (not a recursive walk of `dataDir`), and `store.Open(dataDir)` only
  ever opens `dataDir/tickets.db` — neither notices a sibling dot-
  prefixed directory. Confirmed by reading both, not just asserted.
- **`store.Open`'s repeated-call safety was checked, not assumed**,
  since `admin_export.go`/`admin_integrity.go` now open a fresh
  connection on every request rather than reusing one: `migrate` only
  runs `backupBeforeMigration` (writing into `<data-dir>/backups/`)
  when `0 < maxApplied < highest` — a no-op on an already-current
  database, so repeatedly clicking "Run integrity check" never
  accumulates pre-migration backups.
- **This surfaced and fixed a real, pre-existing bug in
  `internal/backup.Import`**: `insertAll` inserted `env.Entities`
  before `env.Actors`, so any entity whose `created_by` pointed at a
  real (non-seed) actor — i.e. almost every entity in a real
  installation — failed its foreign-key check on import. Every
  existing `export_import_test.go` fixture happened to only ever
  create entities as one of the two seed actors, so the bug was never
  exercised until this ADR's own round-trip test (export a project
  created by a genuine third actor, then import it into a fresh
  server) hit it. Actors are now inserted first.
- **`internal/httpapi` now imports `internal/store` and
  `internal/blobstore` directly**, which it did not before this ADR.
  That is the one line item worth a reviewer's attention if this
  boundary is revisited later — see the Decision section's reasoning
  for why these specific five routes are the exception rather than a
  precedent for routing more of `internal/httpapi` around `Service`.
- **No new config surface for the admin-upload size cap.**
  `httpapi.SetMaxAdminUploadBytes` exists (mirroring
  `SetMaxUploadBytes`) but `cmd/tickets/server.go` does not call it,
  so restore/import uploads are capped at a fixed 10 GiB rather than
  something `internal/config` exposes — revisit if a real installation
  needs a backup larger than that.
