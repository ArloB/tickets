# 0015: Derived mentions

## Context

Product spec §5.2 requires bare (`ABC-123`) and `#`-prefixed
(`#ABC-123`, and a project-scoped ticket-only short form `#123`)
references inside Markdown bodies to become backlink edges, separate
from and implying nothing about explicit relationships or scheduling.
`docs/contracts/references.md` assigns the scanner to build "alongside
comments/backlinks" — this ADR is that build.

## Decision

**A regex scanner in `internal/domain`, not the store layer.**
`domain.ScanReferences(text, scopeProjectKey)` finds every reference
candidate in free text and returns deduplicated `domain.Reference`
values in first-occurrence order. It strips fenced code blocks (```` ``` ````
and `~~~`) and inline code spans (`` `...` ``) before matching — a
reference inside a code sample is not a mention, it's the author
quoting example output. Malformed or unresolvable candidates (an
unrecognized kind letter, a short form with no valid
`scopeProjectKey`) are silently skipped, matching this function's
best-effort-scan-over-prose contract, distinct from `Parse`'s strict
single-token grammar.

**Delete-and-reinsert on every Markdown body write, in the same
transaction as the write it describes.** `derived_mentions`' primary
key is `(source_entity_id, source_comment_id, target_entity_id)`, with
`source_comment_id` a `NOT NULL DEFAULT 0` sentinel (0 = the source
entity's own body — never a real `comments.id`, which
`AUTOINCREMENT`s from 1) rather than nullable, since SQLite's `NULL !=
NULL` would otherwise silently defeat this key's uniqueness for every
own-body mention. `internal/service/mentions.go`'s `rescanMentions`
deletes every existing row for that `(source, comment)` pair and
reinserts fresh ones from the current scan result — never a diff — so
a body edited to remove a reference drops exactly that edge. It's
wired into every Markdown-body-writing mutation: `CreateTicket`,
`UpdateTicketFields`, `CreateFeature`, `UpdateFeature`, `CreateProject`,
`AddComment`, `EditComment`.

**A well-formed but unresolvable reference is silently skipped, not
stored, not an error.** `resolveMentionTarget` dispatches a scanned
reference by kind: `KindTicket`/`KindFeature` resolve via the existing
`GetTicketByRef`/`GetFeatureByRef` (which also means a reference to a
soft-deleted record is, correctly, unresolvable — ADR 0013); anything
else (`decision`/`plan`/`document`) has no table until Phase 5 and is
always unresolvable in Phase 1. Either way, `rescanMentions` moves on
to the next candidate rather than failing the whole write — a body
that happens to mention something that doesn't exist yet (or no
longer exists) is not a validation error.

**Self-mentions are explicitly rejected, not just naturally
absent.** `rescanMentions` skips a candidate whose resolved target id
equals the source entity id. This is a real guard, not defensive
insurance: the primary key includes `target_entity_id` but has no
check constraint against `source_entity_id`, so nothing in the schema
would stop a self-referencing body from inserting a self-loop edge
without this check. `TestTicketDescriptionSelfMentionSkipped`
(`internal/service/mentions_test.go`) is the regression test.

**A mention's target is filtered at read time when soft-deleted, not
cleaned up at delete time — except for a deleted comment's own
mentions, which are deleted outright.** Same asymmetry ADR 0013
documents for relationships: `ListMentionTargetsFromSource` joins the
target against `entities` and filters `deleted_at IS NULL`, so a
mention of a since-deleted ticket disappears from the list and comes
back on restore with no extra step. `DeleteComment` is the one
exception — since comments have no restore path, its own mention rows
(where it is the *source*, i.e. `source_comment_id` = its id) are
deleted outright rather than left to be filtered forever.

## Consequences

- `TestTicketDescriptionMentionsProduceEdges`
  (`internal/service/mentions_test.go`) is verification gate 7: three
  reference forms (explicit, bare, project-scoped short) produce
  exactly three edges, with code-fence exclusion and unresolvable-
  reference exclusion asserted as two *separate* checks — testing them
  together would let a bug in either one hide behind the other's
  correct behavior.
- `@actor` mention scanning (product spec §6.4) is out of scope here:
  it's a domain function with its own tests but no persistence table —
  the table lands in Phase 5 alongside the notifications that actually
  consume it, per the Phase 1 plan's explicit deferral.
- Comments are the only source of mentions that isn't a principal
  entity's own body — `AddComment`/`EditComment` call `rescanMentions`
  with the comment's id as `source_comment_id` and the owning entity's
  `ProjectKey` as the scope. In Phase 1 this was always the *ticket's*
  `ProjectKey`, since comments only attached to tickets; **as of Phase
  6 Step 2, comments exist on all six principal kinds** (ADR 0017's
  updated Consequences), so `resolveCommentOwner`/
  `resolveCommentOwnerByEntityID` (`internal/service/comment.go`)
  resolve the scope generically. A comment on a project is a genuinely
  new mention-source case this ADR's original design didn't anticipate
  — a project has no seq-numbered reference token (`domain.Format`
  rejects `KindProject`), so a project-sourced backlink resolves
  through a new `mentionSourceRefString` helper that returns the bare
  project key instead of delegating to `mentionTargetRef`'s
  reference-only dispatch for that one kind. `TestGetBacklinksFromProjectComment`
  (`internal/service/backlinks_test.go`) is the regression test.
