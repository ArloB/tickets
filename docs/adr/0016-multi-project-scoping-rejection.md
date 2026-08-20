# 0016: Multi-project scoping is a client-side default, not a token/authorization concept

## Context

A server can host multiple, unrelated projects (a `tickets` project
and a separate web-server project, for example). An agent working on
one must not read or write the other by accident, and needs some way
to know which project it's in without being told every time (plan.md
§7.4, raised as an open question before Phase 2 started). ADR 0004's
session/token model was written without a firm answer: scoped bearer
tokens were flagged as "one option" but never actually decided, and
building them as the default answer without deciding would have added
a per-project authorization dimension nothing else in this codebase's
flat, two-level (viewer/editor) permission model has.

## Decision

**Scoped bearer tokens are rejected.** They solve a *context* problem
(which project is this agent working in right now) by reaching for an
*access-control* mechanism (which projects can this token's bearer
touch at all) — the wrong tool for the common single-user/personal
install this product optimizes for (product spec §2). Adding scope to
`agent_tokens` would mean every authorization check downstream of
`internal/service` (already the sole authorization boundary, ADR 0005)
gains a per-project dimension it doesn't otherwise have, for a problem
that isn't actually about access control.

**The `tickets mcp` stdio bridge gets a `--project`/`TICKETS_PROJECT`
default instead**, filled in client-side when an outgoing tool call
omits a project key (`internal/mcpsrv/httpbackend.go`'s
`HTTPBackend.DefaultProject`, `cmd/tickets/mcp.go`). The server never
sees this value or knows it exists — it is pure convenience on the
client's side of the wire, exactly like a shell's `$PWD` biasing a
relative path. Tokens stay server-wide, with no project dimension at
all.

The more promising direction for the underlying problem — binding
scope to how the MCP server is *launched or configured*, rather than
to something the agent must remember or pass per call — is one server
process (or one `--project` default in that project's `.mcp.json`) per
active project, so the boundary lives in *which server the agent is
even talking to*. The tradeoff is one server process per active
project instead of one shared server; cheap for a personal install,
worth reconsidering if team/shared deployments (product spec §18) make
a single shared server more attractive later.

## Consequences

- `internal/apiclient`, `internal/mcpsrv`, and `internal/httpapi` carry
  no project-scoping concept anywhere — a bearer token is either valid
  or it isn't, full stop, the same as before this ADR. There is no new
  authorization code path this decision required building or testing.
- An omitted project key with no `--project`/`TICKETS_PROJECT`
  configured is a client-side `validation_failed` before any request
  is even built (`internal/mcpsrv/httpbackend.go`'s
  `errMissingProjectKey`), not a confusing request to a malformed path
  like `/projects/` — the bridge fails clearly rather than silently
  guessing.
- This is recorded as its own ADR rather than an amendment to ADR
  0004, since it answers a distinct, previously-open question (§7.4)
  that ADR 0004 raised but never actually resolved.
- If team/shared deployments (§18) later make per-project access
  control a real requirement, that is new authorization work belonging
  in `internal/service` (ADR 0005) — not a retrofit of this client-side
  convenience, which was never meant to carry that weight.
