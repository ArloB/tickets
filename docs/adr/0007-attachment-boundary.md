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
