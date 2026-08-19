# SQLite driver spike — result report

**Candidate:** `modernc.org/sqlite` v1.56.0 (pure Go, CGO-free, SQLite 3.51.x)
**Verdict: PASS.** All 7 assertions pass. ADR 0003 adopts this driver;
no fallback needed.

Run with `go run ./docs/spikes/sqlite` from the module root.

| # | Assertion | Result |
| --- | --- | --- |
| 1 | `PRAGMA journal_mode=WAL` returns `wal` | PASS |
| 2 | `PRAGMA foreign_keys=ON` rejects a violating insert | PASS |
| 3 | `CREATE VIRTUAL TABLE … USING fts5(…)` succeeds | PASS |
| 4 | `MATCH` query returns rows; `snippet()` and `bm25()` available | PASS |
| 5 | External-content FTS5 table + sync triggers work inside one transaction | PASS |
| 6 | Concurrent writer + reader under WAL with `busy_timeout` set: no `SQLITE_BUSY` (200 writes + 200 reads) | PASS |
| 7 | `CGO_ENABLED=0` cross-compile for `windows/amd64` and `linux/amd64` from one machine; both binaries run natively | PASS |

## Evidence

- Runtime assertions 1–6: executed on Windows (`go run`) and, via the
  cross-compiled binary, on WSL Ubuntu 22.04 — identical PASS results
  on both.
- Assertion 7: built both targets with `CGO_ENABLED=0` from Windows in
  one invocation each; ran `spike-windows.exe` natively on Windows and
  `spike-linux` natively inside WSL. Both exited 0 with all 6 runtime
  assertions passing.

## Notes for ADR 0003

- `busy_timeout` is set via the DSN (`?_pragma=busy_timeout(5000)`),
  not a separate `PRAGMA` call after open — the driver's connection
  pooling can otherwise apply pragmas to a connection that isn't the
  one used by a later statement. Carry this DSN pattern into
  `internal/store`.
- External-content FTS5 (assertion 5) is the shape the real schema
  will use: a `content='items', content_rowid='id'` FTS5 table kept in
  sync by `AFTER INSERT`/`AFTER DELETE` triggers, all inside the same
  transaction as the source-row change — this is what product spec
  §6.3's "transactionally with source changes" requires.
- No CGO toolchain was needed at any point, on either platform,
  confirming the "small native executables" requirement (§8.2) holds
  with this driver.
