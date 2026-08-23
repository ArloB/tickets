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
  function plugged into `auth.RequireBearerToken` (ADR 0006): it
  hashes the presented token, looks up the `agent_tokens` row, and
  returns `TokenInfo{UserID: "kind:name", Scopes: ["read","write"]}` or
  an error wrapping `auth.ErrInvalidToken`. **Correction to this ADR's
  original wording:** `UserID` carries the `kind:name` wire form
  (`domain.ActorRef.String()`), not the actor's UUID as first written
  here — `kind:name` is already the canonical actor identifier
  everywhere else in the system (ADR 0012), and `Service.withTx`
  resolves actors by `(kind, name)`, not by UUID, so a UUID-carrying
  `UserID` would have needed a new lookup path whose only caller was
  this one.
- **Implemented as of Phase 2 (Steps 1-9).** `internal/auth` provides
  Argon2id password hashing, session/bearer token generation, and
  login throttling; migration `0004_identity_and_auth.sql` adds
  `human_accounts`, `sessions`, `agent_tokens`, `login_attempts`.
  `internal/httpapi`'s authentication middleware resolves a
  `auth.Principal{Actor, Permission, IsAdmin, AuthMethod}` from a
  session cookie, a bearer token, or anonymous access (in that order),
  and stores it on the request context — `requestActor(r)` becomes a
  one-line context read, exactly as this ADR's original "Consequences"
  anticipated, with no change to any of the ~20 service methods that
  consume the result. `internal/mcpsrv`'s `withCallerActor` does the
  analogous thing for the MCP transport, reading
  `req.Extra.TokenInfo.UserID`.
- **A documented exception to ADR 0005.** Permission-level checks
  (`requireEditor`/`requireAdmin` in `internal/httpapi/server.go`,
  driven by the route table's `routePermission` field) live in the
  translation layer, not in `internal/service` — a deliberate
  exception to ADR 0005's "internal/service is the sole
  authorization boundary," because the check depends only on the
  request's authenticated `Principal`, never on any entity a service
  method would inspect. If per-project ACLs are ever added (product
  spec §18), that check would have to move into `internal/service`,
  since it would then depend on *which* project is targeted — noted
  here so the exception doesn't quietly become the permanent shape of
  every future permission check.
- Session ids are stored raw; agent tokens are hashed. Asymmetric but
  deliberate: sessions are short-lived and never logged or exported
  the way a long-lived agent token might be, so the extra hashing cost
  buys little for them.
- Multi-project scoping ("how does an agent know which project it's
  in without a per-project token dimension" — plan.md §7.4, left open
  when this ADR was written) is resolved separately as ADR 0016 — a
  client-side `--project` default, not a token/authorization concept.
- **Two small Phase 4 additions, both unauthenticated like login and
  for the same reason (obtaining credentials can't itself require
  credentials):**
  - `POST /api/v1/setup` creates the first admin account over HTTP, so
    product spec §6.5's "first-run setup" web view doesn't need
    `tickets setup` on a terminal. It is a thin wrapper around
    `service.CreateAdminAccount`, which already refused a second call
    once any human account exists; that existence check was
    strengthened to re-run inside the write transaction (not just
    before it) specifically because this endpoint makes the check
    reachable by two genuinely concurrent requests in a way a single
    local CLI invocation never could — SQLite's `_txlock=immediate`
    (`internal/store/store.go`) serializes the two transactions, so the
    second one's in-transaction recheck reliably sees the first's
    committed row and fails clean with `already_exists` rather than
    racing it.
  - `GET /auth/me` now echoes `csrf_token` for a session-authenticated
    caller (never bearer or anonymous), sent with `Cache-Control:
    no-store`. Without it, a browser tab that reloads mid-session — a
    live session cookie, no in-memory CSRF token left — had no way to
    recover the token `requireEditor` needs on the next mutating
    request short of forcing a fresh login.
