# MCP SDK spike — result report

**Candidate:** `github.com/modelcontextprotocol/go-sdk` v1.2.0 (official SDK)
**Verdict: PASS.** All 5 assertions pass. ADR 0006 adopts this SDK for
both the Streamable HTTP endpoint and the stdio bridge.

Run with `go run ./docs/spikes/mcp` from the module root. (It relaunches
itself as `go run ./docs/spikes/mcp stdio-server` internally for the
stdio assertion — see main.go.)

| # | Assertion | Result |
| --- | --- | --- |
| 1 | One `registerTools(server)` function feeds both transports — no duplicated tool definitions | PASS (structural + exercised by 3 and 4) |
| 2 | `auth.RequireBearerToken` returns 401 with `WWW-Authenticate` for missing/invalid tokens | PASS |
| 3 | A real MCP client connects over Streamable HTTP, lists tools, calls one, and the tool handler observes `TokenInfo` from the request | PASS |
| 4 | The same client reaches the same tool over stdio by launching `mcp-spike stdio-server` as a subprocess | PASS |
| 5 | Both transports work on Windows and in WSL | PASS |

## Evidence

- Every assertion above ran via `go run` on Windows, then again in WSL
  Ubuntu 22.04 against the same commit (`0577565`) after `git pull
  --ff-only` synced the clone — identical 5/5 PASS output on both,
  including the exact `WWW-Authenticate` header value and both
  `auth_user_id`/echo values. Reproduce with `go run ./docs/spikes/mcp`
  on either platform.
- Assertion 3's HTTP leg used a real `httptest.Server` wrapping
  `mcp.NewStreamableHTTPHandler`, not a mock — the auth middleware, the
  wire protocol, and the tool dispatch are all exercised for real.
- Assertion 4's stdio leg used `os.Executable()` to relaunch the actual
  running binary (including under `go run`'s temp-binary indirection)
  as a subprocess via `mcp.CommandTransport`, so it is a real process
  boundary and a real newline-delimited-JSON stdio conversation, not an
  in-process shortcut.

## Notes for ADR 0006

- Confirmed API shape (v1.2.0), which differs slightly from some
  published examples for older SDK snapshots:
  - `mcp.NewStreamableHTTPHandler(getServer func(*http.Request) *mcp.Server, opts)`
    — `getServer` is called per session, so the same `*mcp.Server`
    can be returned for every request (as here) or looked up per
    request once auth/session concerns are added in Phase 2.
  - `mcp.AddTool[In, Out any](s, tool, handler)` is the typed
    registration path; handler signature is
    `func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)`.
    Prefer this over the untyped `Server.AddTool` for the real tool
    surface — the automatic input-schema inference and validation
    directly serve product spec §7.2's "actionable validation errors."
  - `auth.RequireBearerToken(verifier, opts) func(http.Handler) http.Handler`
    wraps the HTTP handler; a tool handler reads the verified token via
    `req.Extra.TokenInfo`, where `req` is the `*mcp.CallToolRequest`.
    This is the concrete mechanism ADR 0004's agent-token attribution
    hangs off of in Phase 2 — `TokenInfo.UserID` is the natural carrier
    for an agent identity.
  - `RequireBearerTokenOptions.ResourceMetadataURL` must be set for the
    `WWW-Authenticate` header to appear on a 401; without it the
    middleware still returns 401 but the header is empty. Set it (even
    to a real Tickets-server URL later) so §9's error contract holds.
  - Server-side stdio is `mcp.StdioTransport{}` plus `server.Run(ctx, t)`;
    client-side stdio to a subprocess is `mcp.CommandTransport{Command: cmd}`.
    Neither transport duplicates tool registration — both just call
    `registerTools` on a fresh or shared `*mcp.Server`, matching product
    spec §8.1's "neither interface duplicates business logic."
