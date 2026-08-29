# 0025: Rendering references in prose as hyperlinks

## Context

Product spec §5.2 already makes a reference written into a Markdown
body meaningful: `domain.ScanReferences` finds it and
`service.rescanMentions` stores a `derived_mentions` edge (ADR 0015),
which is what feeds the backlinks panel. But that edge only ever
surfaced on the *target's* page. On the page actually being read, a
reference like `ABC-1` or `ABC-P4` in a description, plan body, or
comment was inert text — a reader had to copy it into the URL bar or
the search box to follow it.

Making it a link is the obvious counterpart, and the requirement it
introduces is verification: an unresolvable reference must not become
a link that 404s. Bodies are free text and routinely contain
well-formed references to records that do not exist (a plan drafted
before the tickets are filed) or that were soft-deleted since.

## Decision

**Linkification happens at render time only. Stored bodies are never
rewritten.** The Markdown a client `PUT`s is the Markdown it reads
back, byte for byte. Rewriting a body to hold
`[ABC-1](/tickets/ABC-1)` would corrupt the round trip every edit form
depends on and would hand MCP agents a body whose text no longer
matches what its author wrote.

**Existence is answered by a dedicated read endpoint, `GET
/api/v1/refs/resolve?refs=…`** (`internal/service/refs.go`,
`internal/httpapi/refs.go`), which reports per-token existence plus
the kind, title, and status of what resolved. Three alternatives lost:

- *Reusing `derived_mentions`.* The edges are already maintained
  transactionally, and an outbound `GET .../mentions` would mirror
  `listBacklinks` closely. But it answers the wrong question in three
  places: the editor's live preview renders **unsaved** text, for
  which no rows exist; `rescanMentions` skips self-mentions, so a body
  naming its own reference would render unlinked even though that page
  exists; and it skips projects, which have a detail route.
- *Rendering every well-formed reference as a link and letting it
  404.* Cheap, and wrong in exactly the case that matters — a plan
  full of references to tickets that do not exist yet becomes a page
  full of broken links.
- *A `POST` taking a batch body.* `route_table_test.go` reserves every
  non-GET route for Editor and above, and this must be reachable by
  any viewer, since it answers precisely what a `GET` on each named
  record would. It is also a pure read. A comma-separated query
  parameter with a 50-token cap is the shape that fits both.

The endpoint is **best-effort in the same way the scanner is**: a
malformed token, an unknown kind letter, a well-formed token naming
nothing, and a soft-deleted record all return `exists: false` inside a
`200`, never an error. A rendering path must not fail as a whole
because one token in a paragraph is junk.

**The client scans with a remark plugin, not a regex over the raw
string** (`web/src/components/refLinks.ts`). Working on the mdast tree
means fenced code blocks and inline code spans are excluded
structurally — they are separate node types, so there is no
`stripCodeRegions` equivalent to keep in sync with `scan.go`'s — and
text inside an existing link is skipped, which is what prevents an
illegal nested `<a>`. The grammar itself is duplicated in JS (the
regex in `refLinks.ts` mirrors `scan.go`'s two patterns); that
duplication is covered by `refLinks.test.ts`, whose cases are written
against the Go scanner's actual behavior, including the surprise that
`XABC-1` is a reference into project `XABC` rather than a bounded-out
match.

**Resolutions are cached per session, positives only.** A reference is
immutable for the life of its entity (`docs/contracts/references.md`),
so a token that resolved once cannot stop naming that record. A miss
is *not* cached: referencing a record that does not exist yet is a
normal state, and caching it would leave the reference unlinked for
the rest of the session even after the record is created.

## Consequences

- Reference links are root-relative paths built by `refs.ts`'s
  existing `detailRoute`, so they navigate through the router rather
  than reloading the SPA. `Markdown` overrides ReactMarkdown's `a`
  component to route any root-relative href through react-router's
  `Link`; external links keep the plain `<a>` they always had.
- The sanitize schema is widened by exactly one value-pinned
  `className` on `<a>`. `hast-util-sanitize` honors only the first
  entry it finds for a property, and `defaultSchema` already carries a
  pinned `className` on `<a>` for GFM footnote backrefs — so the
  allowed value is *added to that entry*. Appending a second entry
  silently drops the class instead of allowing it, which is a quiet
  enough failure to be worth naming here.
- Rendering a body now issues a network request. It is one batched
  request per distinct set of unresolved tokens, skipped entirely for
  a body with no references, and a failure leaves the references as
  the plain text they used to be — there is nothing to surface to the
  reader.
- `Markdown` and `MarkdownEditor` gained a `projectKey` prop. It
  scopes the ticket-only short form (`#123`) exactly as
  `ScanReferences`' `scopeProjectKey` does server-side; without it,
  only fully qualified references are recognized.
- Nothing about the MCP or CLI surface changes. Both hand raw Markdown
  to an agent that reads references directly; linkification is a
  presentation concern of the web UI alone.
