# Phase 0 spikes

Both spikes are complete: **PASS**. Their throwaway *code* has been
deleted (verification gate 8) — but the reports below are kept
permanently, not deleted with the rest of the scaffolding, because
[ADR 0003](../adr/0003-sqlite-wal-fts5.md) and
[ADR 0006](../adr/0006-mcp-transports.md) cite them by path as
evidence. Deleting the evidence a committed architecture decision
points to would break that decision's traceability for no benefit —
the code (which no longer does anything once its SDK is adopted into
real packages) is what "scaffolding" refers to.

- [`sqlite/REPORT.md`](sqlite/REPORT.md) — `modernc.org/sqlite`
  WAL/FTS5/foreign-key/concurrency/cross-compile assertions. 7/7 PASS.
- [`mcp/REPORT.md`](mcp/REPORT.md) — `modelcontextprotocol/go-sdk`
  shared-tool-registration, dual-transport, and bearer-token
  assertions. 5/5 PASS.

The spike source (`main.go`/`helpers.go`/`build.sh`/`build.ps1`) still
exists in git history if it's ever needed again — `git log --all
--full-history -- docs/spikes/sqlite/main.go` (or the `mcp/` path)
finds the commit that removed it. It's just not part of the working
tree going forward.
