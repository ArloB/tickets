# Tickets

Self-hosted issue tracker for humans and coding agents. Full design in
`plan.md`; layer boundaries and non-obvious decisions in `docs/adr/`.

## Using this project as an MCP agent

If you're connected to a running Tickets server over MCP (`tickets mcp`
or the `/mcp` endpoint), read `docs/mcp-agent-guide.md` first — it
covers the reference grammar, the compact/detail response contract,
and the representative ticket workflow. The server's own
`initialize.instructions` field carries the same essential guidance at
connect time.

## Working on this codebase

- `docs/contracts/` — the wire/behavior contracts every layer must
  honor: reference format, enums, error envelope, compact/detail
  representations, concurrency (optimistic versioning + idempotency
  keys), and the CLI's own contract (exit codes, config precedence,
  token handling).
- `docs/adr/` — architecture decisions with their rationale, not just
  the outcome.
- Build/test: `task ci` (fmt, lint, test, OpenAPI lint, build) is the
  full local gate; `task test` alone for a faster inner loop.
- Do not make **ANY** comments in or around code. If a decision or finding must be documented include it in `docs/` or as a comment/decision/issue/etc. in tickets

No application logic lives in this file — it's a pointer, not a guide.
