# 0023: Explicit feature selection required at creation

## Context

ADR 0001 gave every ticket a fallback (`General`) so the
`Project → Feature → Ticket` hierarchy wouldn't force a feature choice
on small/personal use. In practice, `feature` was never wired into
`CreateTicketRequest` at any layer — every ticket landed in `General`
unconditionally, with no way to choose otherwise at creation time.
Real usage surfaced this as a problem in the other direction: without
a visible choice to make, tickets accumulate in `General` by default
rather than by decision, and there was no way to ask an MCP agent to
put a ticket somewhere else without a separate move afterward.

## Decision

- `service.CreateTicketRequest` gains `FeatureRef` (an explicit
  feature) and `UseGeneralFeature` (an explicit opt-in to `General`).
  At this layer both stay optional and not mutually exclusive with
  being unset together — if neither is given, ticket creation still
  resolves to `General`, unchanged from before. This keeps the service
  layer a permissive primitive; it does not itself enforce a choice.
- Every human/agent-facing entry point built on top of it does enforce
  the choice: `POST /projects/{key}/tickets` (`feature` xor `general`
  in the body), `tickets ticket create` (`--feature` xor `--general`),
  and the MCP `ticket_create` tool (`feature` xor `general` in its
  input) all reject a request that supplies neither or both, instead
  of silently defaulting.
- The MCP in-process backend (`internal/mcpsrv/inprocess.go`, used by
  the server's own `/mcp` endpoint) enforces this itself rather than
  relying on the HTTP handler, since it calls `service.CreateTicket`
  directly and bypasses `internal/httpapi` entirely.
- `General` remains an ordinary feature per ADR 0001 — this ADR governs
  who has to ask for it, not what it is.

## Consequences

- Internal Go callers of `service.CreateTicket` (batch tooling, and
  the ~100 existing service-level tests that predate this ADR) are
  unaffected: they keep relying on the General default and don't need
  to make an explicit choice.
- The "exactly one of feature/general" check is duplicated across
  three call sites (HTTP handler, CLI flag parsing, MCP in-process
  backend) rather than centralized in the service layer, trading a
  small amount of duplication for not requiring every internal caller
  and test to declare a feature choice it doesn't care about.
- Moving a ticket to a different feature after creation
  (`MoveTicketFeature`) already existed at the service/HTTP/CLI layers
  before this change; MCP still has no equivalent tool for it — a
  known gap, not addressed here.
