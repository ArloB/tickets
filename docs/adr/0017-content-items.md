# 0017: Content items — shared plan/document representation model

## Context

Product spec §5.9 names plans and documents as first-class project
records, distinct from tickets, each representable as versioned
Markdown, a versioned uploaded file, a referenced filesystem path, or
an external URL. `entities.kind` has reserved `plan`/`document` values
and the reference grammar has reserved `P`/`DOC` codes since Phase 0
(`internal/domain/reference.go`), but no table backed either kind until
now. Phase 5 Step 3 builds the Markdown representation only; Steps 4–5
add the other three, reusing the same table and the blobstore Step 4
introduces.

## Decision

- **One shared `content_items` table**, not two (`plans`/`documents`)
  and not four (one per representation). `entities.kind`
  (`KindPlan`/`KindDocument`) is the authoritative plan-vs-document
  discriminator, the same role it plays for every other table — but
  unlike `decisions` (which holds exactly one kind, so nothing on that
  table needs to name it), `content_items` denormalizes a `kind` column
  of its own too. `reference_counters` numbers `plan` and `document`
  independently (ADR 0009), so `ABC-P1` and `ABC-DOC1` legitimately
  share `seq=1` in the same project — and SQLite can't express a
  uniqueness constraint across a join, so "this reference names exactly
  one row" (`UNIQUE(project_id, kind, seq)`) requires `kind` to live on
  `content_items` itself, not just on `entities`. This was discovered
  the hard way: an initial version of this table without the column hit
  a real unique-constraint collision the first time a plan and a
  document were created in the same project. Both columns are always
  written together, in the same `InsertEntity`/`InsertContentItem` call
  pair, so they can never disagree.
- **Representation-specific columns are nullable and unused until their
  step lands.** `representation` (`markdown`/`file`/`path`/`url`) is
  fixed per row: validity is enforced in Go, the same way every other
  status/kind enum in this codebase is validated in Go rather than by a
  SQL CHECK per representation. One table keeps "list every version"
  a single indexed query instead of a UNION across representation-
  specific child tables.
- **Representation is immutable after creation.** Switching a plan from
  stored Markdown to an uploaded file means creating a new content
  item, not converting one in place. This keeps the schema and version
  history simple: a `content_versions` row's shape never has to answer
  "which representation was this edit relative to" as anything other
  than the parent row's own fixed value.
- **Current-state-in-main-row + versions-hold-only-history**, the same
  pattern `decisions`/`decision_versions` established in Step 2 and
  `comments`/`comment_versions` established before that: every edit
  archives the pre-update row into `content_versions` first, in the
  same transaction as the overwrite, so a failure never leaves an
  orphaned archive entry.
- **`record_*` (MCP) gains a `kind` discriminator field** rather than
  new `plan_create`/`document_create` tools — keeps the MCP tool
  surface small (the risk table's stated concern), with the
  kind-specific branching living once, in the tool handler, not
  duplicated across both `Backend` implementations. `record_get`'s
  output becomes a superset shape covering both decision-only fields
  (context/decision/rationale/consequences/status/superseded_by) and
  content-item-only fields (body), each omitted when not applicable to
  the fetched record's kind — `mcp.AddTool`'s output schema is fixed
  per tool registration, so a single tool answering three kinds needs
  one shape wide enough for all of them, not three separately-typed
  tools.
- **Associations, links, and backlinks are not reimplemented.**
  `internal/service/association.go`'s `resolveAssociationEndpoint` and
  `internal/service/mentions.go`'s `resolveMentionTarget`/
  `mentionTargetRef` already had comments anticipating this
  (`domain.ValidAssociationKind` already allows plan/document; a
  reference to one just had nowhere to resolve). Step 3 adds the
  `KindPlan`/`KindDocument` cases those functions were already
  documented as needing; the generic `addAssociation`/`listAssociations`/
  `addLink`/`listLinks`/`listBacklinks` HTTP handlers are reused as-is
  under new `/plans/{ref}/...` and `/documents/{ref}/...` route
  registrations, the same way they're already shared across
  tickets/features/decisions.

## Consequences

- A content item has no `status` field: §5.9 names no workflow for
  plans/documents the way §5.8 names one for decisions, so none is
  added speculatively.
- Comments stayed ticket-only in Step 3 despite §5.10 naming
  projects/features/tickets/decisions/plans/documents as commentable —
  extending `ticket_comment` (or adding a generic comment target) to
  every principal entity was out of this step's scope and not required
  by the plan/document vertical slice; it was a pre-existing gap this
  step didn't widen (decisions lacked comments too). **Closed in Phase
  6 Step 2**: `internal/service/comment.go`'s `resolveCommentOwner`
  generalizes `AddComment`/`ListComments`/`EditComment`/`DeleteComment`
  to all six kinds — no migration was needed, since
  `comments.entity_id` already referenced `entities(id)` generically
  (`0002_core_domain.sql`); the restriction was purely a service-layer
  `store.GetTicketByRef` call, not a schema one.
- Steps 4–5 (attachments, and content items' file/path/url
  representations) build on this table's already-present nullable
  columns without a further migration.

**Implemented in Phase 5 Step 5.** `file`/`path`/`url` representations
land on `CreateContentItem`/`UpdateContentItem`, reusing Step 4's
`internal/blobstore` for `file` (dedup falls out of content-addressing
the same way it does for attachments) — no schema change, since the
migration already reserved these columns. `internal/httpapi` dispatches
create/update on Content-Type (`multipart/form-data` means
`representation=file`, JSON otherwise, naming `path`/`url` in the body)
the same way `attachments.go` does; new `GET /plans|documents/{ref}
/download` and `.../versions/{version}/download` routes stream a file
representation's bytes, and reject any other representation — a path
representation's target is never opened, the same ADR 0007 boundary
attachments enforce. `record_create`/`record_update` (MCP) gain
`representation`/`path`/`url` fields but no file-upload path: a tool
call has no multipart transport, so uploading a file representation
stays HTTP/CLI-only. Representation is confirmed immutable at the type
level, not just by convention — `UpdateContentItemRequest` (service,
HTTP, apiclient, and MCP) carries no representation field at all, so
there is no code path that could switch one.
