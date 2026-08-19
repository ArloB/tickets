# 0004: Human sessions, agent bearer tokens, optional anonymous read

## Context

Product spec §4 defines three actor kinds (human, agent, system) and
two content permission levels (viewer, editor), with an operational
`admin` flag on the first human account. §10 requires the server to
default to loopback binding and warn before exposing anonymous access
externally.

## Decision

- **Humans** authenticate with username + Argon2id-hashed password and
  get a secure, HTTP-only, same-site session cookie (§10). Session
  security (CSRF, throttling, expiry) is built in Phase 2.
- **Agents** are persistent actors with a name, optional description,
  an owning human, and one or more bearer tokens. Only a token's hash
  is ever stored; the raw value is shown once at creation. Agent
  identity is what the MCP spike's `TokenInfo.UserID` (see ADR 0006)
  carries end-to-end from HTTP request to tool handler.
- **System** is a fixed actor used for migrations, imports, and other
  server-owned events — not created through any API.
- **Anonymous read** is a server-wide toggle, off by default except
  for loopback-only personal use, granting `viewer` only. Enabling it
  on a non-loopback bind must print a prominent warning (§10).
- Permission is exactly two levels — viewer and editor — with no
  per-project roles in the MVP (§4.2). `admin` is orthogonal:
  operational, not a content permission.

## Consequences

- The `entities`/service layer never needs to special-case "no actor"
  — every mutation is attributed to a human, agent, or system actor
  row, even for anonymous-read-enabled installs (anonymous requests
  simply cannot reach a mutating endpoint).
- Agent token verification is the concrete `auth.TokenVerifier`
  function plugged into `auth.RequireBearerToken` (ADR 0006):
  it hashes the presented token, looks up the `agent_tokens` row, and
  returns `TokenInfo{UserID: agent's actor UUID, Scopes: ["read","write"]}`
  or an error wrapping `auth.ErrInvalidToken`.
- Full implementation (schema, hashing, session cookies, the anonymous
  toggle and its warning) lands in Phase 2 per the roadmap; Phase 0's
  vertical slice deliberately ships with no auth at all, behind the
  loopback default (see Step 5 of the Phase 0 plan).
