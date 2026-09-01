# Security model

Product spec §10's threat model, per Phase 6 Step 7: every risk area
mapped to the control that mitigates it and the test that proves it.
This document is written against the shipped surface, not a design
aspiration — every test cited here exists and passes as of this
writing.

Tickets is designed for **trusted-LAN self-hosting**: a small team or a
mix of humans and coding agents sharing one server on a network they
control, not a multi-tenant public service. Several controls below are
calibrated for that threat model explicitly (see "Out of scope" at the
end).

## Authentication and sessions

| Risk | Control | Proof |
| --- | --- | --- |
| Password stored recoverably | argon2id, not a reversible hash or plaintext (`internal/auth/password.go`) | `internal/auth/password_test.go` |
| Session hijacking via XSS | Session cookie is `HttpOnly`; CSRF token is a separate value the client must echo in a header, so a cookie alone (stealable via XSS) can't mutate state | `TestMutatingRequestWithoutCSRFTokenRejected`, `TestMutatingRequestWithWrongCSRFTokenRejected`, `TestMutatingRequestWithSessionAndCSRFSucceeds` |
| Stale session reuse | Sessions expire; an expired session is rejected, not silently renewed | `TestExpiredSessionRejected` |
| Bearer token replay after revocation | Revoking a token invalidates it immediately (`internal/store`'s `revoked_at` check in `VerifyBearerToken`) | `TestRevokedBearerTokenRejected` |
| Credential-stuffing / brute force | DB-persisted login throttle (survives a restart, unlike an in-memory counter), a trailing window that ages out rather than permanent lockout | `TestLoginThrottledAfterMaxFailures`, `TestLoginThrottleResetsAfterWindow` |
| Bearer token used over plaintext LAN HTTP | `warnIfInsecureBearer` logs (doesn't block — a LAN install may accept the risk) whenever a bearer token arrives over a non-loopback, non-TLS connection | `internal/httpapi/auth_middleware.go`'s `warnIfInsecureBearer`, exercised by `TestBearerTokenNeverAppearsInLogOutput` |
| Token value leaking into logs | The one call site that logs anything during bearer handling logs only the request's `Host`, never the token | `TestBearerTokenNeverAppearsInLogOutput` |
| SQL injection via any user-supplied string | Every `internal/store` query is a static placeholder string; arguments never get string-concatenated into SQL | `TestSQLMetacharactersAreTreatedAsInertData` (a live HTTP round trip, not just a grep) |

## Uploads

| Risk | Control | Proof |
| --- | --- | --- |
| Oversized upload exhausts disk/memory | `maxUploadSize` enforced via `http.MaxBytesReader` before the body is read, not after | `TestAttachmentUploadTooLarge` |
| A malicious filename writes outside the intended storage location | Uploaded bytes are stored content-addressed by SHA-256 hash, sharded by hash prefix (`internal/blobstore`) — the client-supplied filename is never used to construct a filesystem path at all, so there is no path to traverse | `internal/blobstore`'s own tests; architectural, not filename-dependent |
| A malicious filename injects HTTP response headers on download | `Content-Disposition` is built via `fmt.Sprintf("...filename=%q", ...)` — Go's `%q` escapes every control character and embedded quote into a literal backslash sequence, so a filename can never break out of the header value or add a second header line | `TestAttachmentFilenameCannotInjectResponseHeaders` (seeds a filename containing raw CR/LF and a `Set-Cookie:` line directly, confirms no header injection and the body is unaffected) |
| Uploaded content served with an attacker-chosen `Content-Type` (e.g. `text/html`) renders inline as a page on the app's own origin (stored XSS) | `media_type` is genuinely client-supplied and echoed verbatim into `Content-Type` with no allow-list — so this is *not* prevented by Content-Type restriction. Three independent controls prevent inline rendering instead: (1) an upload can never have an empty filename — `mime/multipart`'s own parser only classifies a part as a file when its `filename` param is non-empty, so `Content-Disposition` is never skipped; (2) `Content-Disposition` is hardcoded to the `attachment` disposition type, never `inline`, which is what actually stops a browser from rendering the response rather than downloading it, regardless of `Content-Type`; (3) `securityHeaders` wraps the entire mux (not just the SPA), so `X-Content-Type-Options: nosniff` and a strict CSP apply to the download route too, as defense in depth | `TestUploadedHTMLNeverRendersInline` (uploads a `text/html`-declared file, confirms `Content-Type` is genuinely echoed as-is, then confirms disposition/`nosniff`/CSP are all present anyway) |

## Path and URL references (§5.11's "external link" and `path`-representation attachments/content items)

| Risk | Control | Proof |
| --- | --- | --- |
| A `path`-kind attachment's target is read and served, leaking arbitrary local files | `DownloadAttachment`/`DownloadContentItem` explicitly reject any kind/representation other than the actual uploaded-file one — a `path` value is stored and returned as inert metadata; nothing in this codebase ever calls `os.Open` (or equivalent) on it | `TestAttachmentPathReferenceNeverRead`, `content_items_representations_test.go`'s content-item counterpart (both write a real, readable secret file and confirm the download route never serves it) |
| An external link URL is used for something other than rendering a clickable link | `domain.ValidateLinkURL` restricts a named external link to `http`/`https`/`mailto` schemes server-side. Inline Markdown links (client-side only, see below) use a slightly wider but equally script-free allow-list — `http`/`https`/`mailto`/`tel` — never the same list, but neither allows `javascript:`/`data:` | `internal/domain` link-validation tests; `web/src/components/Markdown.test.tsx`'s `javascript:`/`data:` href cases |

## Markdown rendering (client-side only — no server-side HTML generation)

Verified, not assumed: no package under `internal/` imports a
Markdown-to-HTML renderer (`goldmark`, `blackfriday`, or similar).
Ticket/feature/decision/comment bodies are stored and transmitted as
plain text; rendering to HTML happens exclusively in the browser via
`web/src/components/Markdown.tsx`.

| Risk | Control | Proof |
| --- | --- | --- |
| `<script>`, an event handler (`onerror=`, `onclick=`), or a `javascript:`/`data:` URL executes from a rendered ticket/comment body | `react-markdown` + `rehype-sanitize`'s `defaultSchema` strips script tags, event-handler attributes, and any link/image scheme outside `http`/`https`/`mailto`/`tel` | `web/src/components/Markdown.test.tsx`'s explicit XSS payload table (script tag, `onerror`, `onclick`, `javascript:` href, `data:` href) |
| CSP misconfiguration lets a sanitizer bypass execute anyway | A strict CSP (`default-src 'self'`, no `unsafe-inline`, no `unsafe-eval`) is asserted against the real production build in a real browser, both "zero violations on real content" and "a deliberately injected inline script is actually blocked from running, not just reported" | `web/e2e/csp.spec.ts` |

## Anonymous access (product spec §4.2)

Anonymous read is an explicit, off-by-default server setting. When
enabled, every `routeViewer` route (GET) is reachable with no
credentials; every other route still requires at least Editor.

| Risk | Control | Proof |
| --- | --- | --- |
| A route added later is accidentally registered as anonymously *writable* | `routeTable()` is the single source of truth NewHandler wraps from; a static test reads the table directly and fails if any non-GET route is `routeViewer` | `TestEveryMutatingRouteRequiresAtLeastEditor` |
| Anonymous read doesn't actually cover a route that should be viewer-readable, or silently covers one that shouldn't be (notifications, subscription status — both need a real identity even though they're GET) | Live HTTP round trips against the real route surface, not just the static table | `TestAnonymousReadAllowedWhenEnabled`, `TestAnonymousReadRejectedWhenDisabled`, `TestAnonymousWriteRejectedEvenWhenReadEnabled`, `TestAnonymousReadCoversStep10Through14Routes`, `TestAnonymousReadCoversPhase4And5Routes` (decisions, plans/documents incl. version history and file download, links, backlinks, activity, search, an attachment's own bytes — and confirms notifications/subscribe status stay rejected) |
| Anonymous read was widened to attachment *bytes* in Phase 5 without anyone noticing the exposure grew (ADR 0004's Consequences flags this explicitly) | Same test as above exercises `/attachments/{id}/download` anonymously — this is the literal risk ADR 0004 names, not inferred | `TestAnonymousReadCoversPhase4And5Routes` |
| An operator enables anonymous read on a non-loopback bind without realizing every project becomes readable to any host that can reach it | `internal/config`'s `warnOnInsecureDefaults` prints a startup warning for exactly that combination — non-loopback *and* anonymous read enabled — and stays silent otherwise, since a non-loopback bind with anonymous read off still requires authentication on every route (product spec §10, ADR 0004) | `TestWarnOnInsecureDefaults` |

## MCP token handling

MCP is reachable two ways (ADR 0006): the HTTP-mounted `/mcp` endpoint
(`HTTPBackend`/real network exposure, bearer-token authenticated
exactly like `internal/httpapi`) and the `tickets mcp` stdio bridge
(`InProcessBackend`, calling `*internal/service.Service` directly with
no HTTP layer at all).

| Risk | Control | Proof |
| --- | --- | --- |
| A tool call over `/mcp` succeeds with no token | `tokenVerifier` adapts `service.VerifyBearerToken` into the MCP SDK's `RequireBearerToken` middleware — every tool call is gated the same way an HTTP request is | `TestToolsOverRealStreamableHTTPRejectsMissingToken` |
| A tool call over `/mcp` succeeds with a revoked token | Same `service.VerifyBearerToken` call revocation already covers at the HTTP layer, proven again at the MCP transport layer rather than left to be inferred | `TestToolsOverRealStreamableHTTPRejectsRevokedToken` |
| A tool call is misattributed to the wrong actor | The verified token's actor (`kind:name`) is what every tool call's audit/creator attribution actually uses, not a client-supplied value | `TestTicketCreateOverRealStreamableHTTPWithBearerToken` |
| The `tickets mcp` stdio bridge has no equivalent gate | **Accepted, not a gap**: `InProcessBackend` intentionally bypasses HTTP auth because stdio is inherently a trusted local channel — whoever can start the `tickets mcp` process already has the same access a local shell on that machine would. There is no admin-management tool over MCP at all for the identical reason (`cmd/tickets/admin_agent.go`'s package doc comment) | Architectural; see ADR 0006 |
| A token value leaks into the audit trail or an export | Token values are never written to `audit_events` (only the operation and actor are recorded — migration `0013`); `agent_tokens`/`sessions`/`human_accounts` are never selected by `Export` at all | `TestAgentTokenAuditEventNeverCarriesTokenValue`, `TestExportNeverContainsSecrets` (extended in Phase 6 Step 7 to sentinel-check a session id, CSRF token, and agent token hash individually, not just a password hash) |

## Secret redaction in export/backup

`tickets export` (portable JSON, product spec §12) and
`tickets admin backup` (raw SQLite snapshot) are two different
mechanisms with different exposure:

- **`admin backup`** is a full database snapshot — it necessarily
  contains everything `export` deliberately omits (password hashes,
  session rows, token hashes). This is by design: it's meant for
  same-machine disaster recovery, protected by the same filesystem
  permissions as the live database, not for sharing.
- **`export`** never selects from `human_accounts`, `sessions`, or
  `agent_tokens` at all — verified with three independent sentinel
  values (a password hash, a session id/CSRF token, an agent token
  hash) confirmed absent from the exported JSON, not merely assumed
  from reading `Export`'s column list.

Proof: `TestExportNeverContainsSecrets`.

## Out of scope (accepted for a trusted-LAN, self-hosted install)

These are deliberate, not oversights — each is already noted in the
relevant ADR's Consequences section:

- **No malware scanning of uploaded files.** Uploaded bytes are stored
  and served as-is; a team sharing a LAN install is trusted not to
  upload malicious files, the same trust boundary as a shared network
  drive.
- **No `Last-Event-ID` SSE replay** (ADR 0020) — a missed event during
  a brief disconnect requires a client-side refetch, not a security
  concern but noted here since it's adjacent to the transport surface.
- **No per-project ACLs.** Every authenticated Editor can read/write
  every project; access control is install-wide (Viewer/Editor/Admin),
  not project-scoped. Appropriate for a small trusted team, not a
  multi-tenant deployment.
- **Anonymous read is explicitly a deliberate default-on-loopback
  convenience**, not a hardened public-internet posture — the CLI/admin
  docs (Phase 6 Step 9) must warn operators against exposing an
  anonymous-read-enabled instance beyond a trusted network.
