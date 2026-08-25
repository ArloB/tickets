# 0007: Attachment boundary — content-addressed store, path references never read

## Context

Product spec §5.11 and §10 require uploaded attachments to live in a
managed, content-addressed store outside SQLite, while path
attachments are references only: the server must never resolve or
serve an arbitrary filesystem path through the web API, since doing so
would turn a stored string into unintended file disclosure.

## Decision

- Uploaded content is written under the configured data directory,
  named by content hash (dedup falls out of this naturally), and
  referenced from `attachments`/`attachment_versions` rows. Streamed
  on upload/download per §9 — never buffered whole in memory.
- A path attachment stores its path string and metadata only.
  `internal/httpapi` and `internal/mcpsrv` have no code path that opens
  a path attachment's target; there is no "serve this path" endpoint
  in the MVP. A later explicit *import* action (§5.11, out of MVP
  scope) would be the only way a path's bytes ever enter managed
  storage, and it would go through the same content-addressed pipeline
  as a direct upload.
- Upload size limit defaults to 25 MiB per version, configurable
  (§5.11), enforced by `internal/httpapi` before the handler body
  starts writing to storage.

## Consequences

- No path-traversal surface exists for path-reference attachments,
  because there is no read path to traverse.
- Attachments are not built in Phase 0's vertical slice (deferred per
  Step 5); this ADR fixes the boundary so Phase 5's implementation
  doesn't have to relitigate it.
- **Implemented in Phase 5 Step 4.** `internal/blobstore` writes the
  blob (via `Put`) before the enclosing `internal/service` transaction
  commits, so a transaction rollback (e.g. the owning entity/comment
  turns out not to exist) can leave an orphaned blob on disk with no
  `attachments`/`attachment_versions` row pointing at it. This is
  harmless under content-addressing (the same bytes uploaded again
  later dedup onto the same orphaned file rather than writing a
  second copy).
  **The reconciliation/GC pass landed in Phase 6 Step 3**, as
  `tickets admin integrity --gc`: `store.ListReferencedBlobHashes`
  unions `file_hash` across `attachments`/`attachment_versions`/
  `content_items`/`content_versions` (every current and historical
  version, since a prior version's blob must stay reachable through
  its own `.../versions/{version}/download` route), diffed against
  `blobstore.Store.Hashes`' on-disk inventory. Deliberately still an
  operator-run command, not an automatic background sweep — matching
  this ADR's original reasoning that orphaned blobs are harmless, not
  urgent. `--gc` also leaves alone any orphan written within the last
  hour (`gcMinOrphanAge`, `cmd/tickets/admin_integrity.go`): `Put`
  writes a blob's bytes before its enclosing `internal/service`
  transaction commits, so a blob that's merely mid-upload — its
  `attachments` row hasn't committed yet — looks identical to a
  genuine orphan for the seconds between `Put` and commit, and this
  command has no way to tell the two apart other than age. A corrupted blob (content that no longer hashes to its own
  content-addressed filename — `blobstore.Store.Verify`) is reported
  by the same command but never auto-removed by `--gc`, even though
  it's also unreferenced-or-not: corruption might be partially
  recoverable, which a genuine orphan never needs to be, so the two
  findings stay distinct rather than collapsing into one
  "delete anything --gc doesn't like" behavior.
