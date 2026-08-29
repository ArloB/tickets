# Public reference contract

Backed by `internal/domain/reference.go` and `reference_test.go`. This
document and that code must not diverge — the tests are the executable
form of this contract.

## Project key

Chosen once at project creation, immutable thereafter (§5.3).

```
^[A-Z][A-Z0-9]{1,9}$
```

Uppercase ASCII letters and digits, 2–10 characters, starting with a
letter. `ABC`, `TIX9` are valid; `abc`, `1BC`, `A` are not.

## Per-kind reference tokens

| Entity | Grammar | Example |
| --- | --- | --- |
| Ticket | `{KEY}-{seq}` | `ABC-123` |
| Feature | `{KEY}-F{seq}` | `ABC-F12` |
| Decision | `{KEY}-D{seq}` | `ABC-D7` |
| Plan | `{KEY}-P{seq}` | `ABC-P4` |
| Document | `{KEY}-DOC{seq}` | `ABC-DOC9` |

`{seq}` is a positive decimal integer with no leading zeros, allocated
per ADR 0009 (per-project, per-kind, strictly increasing, starting at
1). The kind letter code (`F`, `D`, `P`, `DOC`) is fixed and never
reused for another kind.

A reference is immutable for the lifetime of the entity: renaming a
project's key is out of MVP scope specifically to preserve this
(implied by §5.2's "immutable" and not contradicted anywhere in
plan.md).

## Recognition in text

- Bare (`ABC-123`) and `#`-prefixed (`#ABC-123`) forms are equivalent
  wherever Markdown fields are scanned for references (§5.2).
- Inside a comment scoped to a known project, the short form `#123`
  resolves to that project's ticket with sequence 123 — this form has
  no kind letter and is ticket-only; it does not exist for features,
  decisions, plans, or documents, matching §5.2's example exactly.
- Mentioning a reference creates a derived `mentions` edge (§5.2); it
  never implies a typed relationship (§5.7) or scheduling semantics.

## Rendering

The web UI turns every reference it finds in a rendered Markdown body
(description, plan/document body, comment) into a hyperlink to that
record's detail page, but only after confirming the record exists —
`GET /api/v1/refs/resolve?refs=...` answers that for a batch of tokens
(ADR 0025). A well-formed reference to something that does not exist,
or that was soft-deleted, stays plain text rather than becoming a link
that 404s.

Recognition for rendering uses the same grammar as the mention scan
above, including the code-fence/inline-code exclusion and the
project-scoped `#123` short form, so what a body links is exactly what
it mentions — with one deliberate difference: a body's reference to
itself renders as a link, though `rescanMentions` skips it as an edge.
A bare project key is not recognized in prose (it would linkify every
uppercase word), even though `/refs/resolve` accepts one for callers
that already hold one, such as a backlink source. Nothing is rewritten
in storage; linkification is render-time only.

**Implementation note:** `internal/domain/reference.go` implements
`Format` and `Parse` for a single token (all 5 kinds) plus project-key
validation — the grammar this table defines. Scanning free Markdown
text for embedded references (the bullet above) is
`internal/domain/scan.go`'s `ScanReferences`, built in Phase 1
alongside comments/backlinks (ADR 0015) against exactly this grammar —
including stripping fenced code blocks and inline code spans first, so
a reference quoted as example output inside a code sample is not
treated as a mention.

## Errors

Parsing an otherwise well-formed reference against an unknown kind
letter, a zero/negative/leading-zero sequence, or an invalid project
key returns a typed error, not a partial/best-effort result — callers
(HTTP 404 mapping, MCP tool errors) decide what to do with "not a
reference" versus "well-formed but not found."
