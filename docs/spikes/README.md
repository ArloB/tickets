# Phase 0 spikes

Throwaway scaffolding. Each spike is a small Go program plus a table of
assertions, ending in a written PASS/FAIL report. This entire directory
is deleted once both spikes report PASS (or a fallback is taken and
recorded in the relevant ADR) — see the Phase 0 implementation plan,
Step 2 and verification item 8.

Planned:

- `sqlite/` — `modernc.org/sqlite` WAL/FTS5/foreign-key/concurrency/
  cross-compile assertions.
- `mcp/` — `modelcontextprotocol/go-sdk` shared-tool-registration,
  dual-transport, and bearer-token assertions.
