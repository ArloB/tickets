# 0006: MCP over Streamable HTTP + an HTTP-backed stdio bridge

## Context

Product spec §7.1/§8.1 requires MCP as the primary agent interface,
available both as a Streamable HTTP endpoint on the running server and
as a `tickets mcp` stdio bridge for hosts that prefer launching a
local process — with the bridge talking to the HTTP API rather than
opening SQLite directly, and neither transport duplicating business
logic. §8.2 flagged the SDK's transport support as a pre-implementation
risk. That spike ran and is fully documented in
`docs/spikes/mcp/REPORT.md`.

## Decision

Use `github.com/modelcontextprotocol/go-sdk` v1.2.0 for both
transports. All 5 spike assertions passed, including a real client
reaching a shared tool set over both Streamable HTTP and a subprocess
stdio connection, and `auth.RequireBearerToken` correctly rejecting bad
tokens while exposing `TokenInfo` to the tool handler.

- `internal/mcpsrv` exposes one `registerTools(*mcp.Server)` function.
  Both `mcp.NewStreamableHTTPHandler` (mounted on the running server)
  and the `tickets mcp` stdio bridge (`mcp.StdioTransport` +
  `server.Run`) call it — no tool is ever defined twice.
- The stdio bridge is a thin process: it constructs an
  `internal/service`-backed `*mcp.Server` the same way the HTTP path
  does, but by calling into `internal/service` **through the configured
  Tickets HTTP API**, not the local database, per §8.1's "no client
  reads SQLite directly" boundary. (The spike's stdio server called
  `internal/service` in-process only to prove the transport; the real
  bridge, built in Phase 2 Step 15, is `internal/mcpsrv.HTTPBackend`
  over `internal/apiclient.Client`, an HTTP client.)
- `auth.RequireBearerToken` wraps the HTTP handler; a tool handler
  reads the verified agent identity via
  `req.Extra.TokenInfo.UserID` (`req` being the `*mcp.CallToolRequest`).
  **Correction to this ADR's original wording:** as actually built in
  Phase 2, `UserID` carries the agent actor's `kind:name` wire form
  (`domain.ActorRef.String()`), not a UUID — see ADR 0004's
  Consequences for why `kind:name` won out (it's already the canonical
  actor identifier everywhere else in the system, and
  `Service.withTx` resolves actors by `(kind, name)`, not UUID).
  `RequireBearerTokenOptions.ResourceMetadataURL` must be set for the
  `WWW-Authenticate` header to appear on a 401 — confirmed by the
  spike; omitting it still returns 401 but silently drops the header
  that §9's error contract expects clients to be able to rely on.
- Tool registration uses `mcp.AddTool[In, Out]` (typed), not the
  untyped `Server.AddTool` — its automatic schema inference and input
  validation directly implement §7.2's "actionable validation errors"
  requirement.

## Consequences

- The full MCP tool surface (§7.2: `projects_list`, `ticket_create`,
  etc.) is built once against `internal/service` and is automatically
  available over both transports.
- Phase 0's vertical slice (Step 5) shipped 3 tools unauthenticated
  behind the loopback bind (see the Phase 0 plan). **Phase 2 (Step 8)
  activated the bearer-token wiring proven here**:
  `mcp.NewStreamableHTTPHandler` is wrapped with
  `auth.RequireBearerToken`, backed by a `TokenVerifier`
  (`internal/mcpsrv/auth.go`) that calls `service.VerifyBearerToken`;
  an unauthenticated tool call is rejected, and a valid one attributes
  the calling agent correctly in the resulting audit trail.
- No SDK fallback is needed; the API surface is pinned at v1.2.0 in
  `go.mod`.
