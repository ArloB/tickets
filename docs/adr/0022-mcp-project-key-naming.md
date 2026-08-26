# 0022: One spelling for the project key across the MCP tool surface

## Context

The MCP tool surface used three different JSON field names for one
concept — the project key an agent passes to identify a project:

- `key` on the tools where a project is the *subject* of the call:
  `project_brief`, `project_get`, `project_create`, `project_update`.
- `project` on `search`.
- `project_key` on the tools where a project *scopes* the call:
  `tickets_list`, `features_list`, `feature_create`, `record_create`,
  and the content-item tools.

Read as a whole this had an internal logic — subject versus scope — and
each individual tool's schema was self-consistent and correctly
described. It survived every Go test, because a test calls tools with
the argument names the code declares; it can never discover that a
*reader* of the surface would guess a different one.

`tools/row10drill` (`docs/row10-live-agent-drill.md`) found it on its
first outing. A live Codex agent called
`project_brief {"project_key": …}`, got `-32602: invalid arguments`,
and recovered by retrying with `{"key": …}` — in 2 of 3 sessions. This
is exactly the failure the distinction invites: an agent that has
learned `project_key` from `tickets_list` generalizes it to
`project_brief`, because nothing in `project_brief`'s description
signals that this one takes a different name.

`docs/mcp-agent-guide.md` compounded it. The guide documents
`project_key` and never mentions `key` at all, so an agent following
the project's own guidance would produce precisely the call that fails.

## Decision

Every tool spells the project key `project_key`. `key` and `project` are
gone from the MCP input surface.

`internal/mcpsrv.TestEveryToolNamesTheProjectKeyIdentically` locks this
in: it lists the real tool surface over a live MCP session and fails if
any tool exposes an input property named `key` or `project`. The test
was confirmed to fail when the old name is restored, so it is a real
guard rather than a passing assertion that never had teeth.

The subject/scope distinction is deliberately abandoned rather than
documented. It was a distinction the surface made for its own internal
coherence, and its only observed effect on a reader was a failed call.
Consistency an agent can rely on without reading a rule is worth more
here than a rule that is coherent once explained.

`project_brief` and `project_get` additionally declare `project_key` as
*optional* rather than required. This is not a weakening of the schema
but a correction of it: `HTTPBackend.GetProject`/`GetProjectBrief`
already fall back to the bridge's configured `--project` default when
the key is omitted, and `docs/mcp-agent-guide.md` already promised that
"every tool call can omit `project_key` entirely" — while the schema
required it, making the documented and implemented behavior
unreachable. `project_create`, `project_update`, and `search` keep
their current requiredness, because no such fallback exists for them
(defaulting the key of a project being *created* would be wrong).

No alias was added. Accepting `key` as a hidden alias alongside
`project_key` would require dropping `project_key` from the schema's
`required` list — an alias-only call fails schema validation while the
canonical field is required — which trades a guessable-name failure for
a silently-omittable-field failure. That is a worse surface for the same
agents this change is meant to help. A clean single name keeps
`required` meaningful.

## Consequences

- **This is a breaking change to the MCP tool surface.** A client
  sending `{"key": …}` to `project_brief`, `project_get`,
  `project_create`, or `project_update`, or `{"project": …}` to
  `search`, now gets `-32602: invalid arguments`. Three call sites in
  `internal/mcpsrv/mcpsrv_test.go` were the only in-repo consumers.
  Taken pre-1.0 and deliberately: the surface is young, `project_brief`
  is new in Phase 6, and shipping 1.0 with a known agent-hostile
  inconsistency is the more expensive option.
- The HTTP API, CLI, and web UI are untouched. This is the MCP tool
  schema only; `domain.Project.Key` and every internal Go struct field
  keep their names, since Go struct conversion ignores tag differences.
- `docs/mcp-agent-guide.md`'s existing `project_key` guidance is now
  accurate for the whole surface rather than most of it.
- **The class of defect generalizes, and this is the durable lesson.**
  No Go test can find a naming choice that misleads a reader, because
  tests and tool schemas share an author: a test calls a tool with the
  names the code declares, so it can never discover that a reader would
  guess a different one. This defect survived the entire Go suite *and*
  was actively contradicted by the project's own agent guide, and
  `tools/row10drill` found it on its first outing. Run the drill on
  tool-surface changes, not only at acceptance time.
- `TestEveryToolNamesTheProjectKeyIdentically` is the cheap standing
  guard, but it only enforces *this* consistency rule. It would not have
  found the original problem — nothing in the repo would have. The drill
  is what turns "an agent might misread this" from a matter of opinion
  into an observation with a reproduction rate.
