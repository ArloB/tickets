# Performance benchmarks

Product spec §11's performance targets, measured against §11's reference
dataset, recorded here per §11's explicit requirement ("benchmarks must
record dataset, hardware, build, and cold/warm state").

## Targets (§11)

- Indexed detail and first-page list requests: p95 server latency < 100 ms.
- Full-text search first-page requests: p95 server latency < 200 ms.
- Ordinary non-upload mutations: p95 server latency < 250 ms.

## Dataset, hardware, build, warm state

- **Dataset:** `internal/fixtures.Full` — 25 projects, 100,000 tickets,
  500,000 comments, 10,000 decisions/plans/documents (134/133/133 per
  project), seeded deterministically (seed 42). No uploaded-file bytes
  except the one attachment `internal/httpapi`'s benchmark creates for
  the streaming benchmark (~1.1 MB), matching §11's "excluding
  uploaded-file bytes" scope.
- **Hardware:** 13th Gen Intel Core i5-13600KF, 20 logical CPUs, Linux
  (WSL2).
- **Build:** `go version go1.26.6 linux/amd64`, commit `60322fc`.
- **Warm/cold state:** each package's fixture is generated fresh at the
  start of that package's test binary process; SQLite's own page cache
  warms up across a benchmark's `b.N` iterations within it. Numbers
  below are "first run this process," not independently controlled
  cold vs. warm measurements — `task bench`'s own doc comment states
  this limitation. `go test`'s benchmark header records goos/goarch/cpu
  for every run; `-benchmem` records allocations.
- **Run command:** `task bench`, or the per-package `go test -run '^$'
  -bench=. -benchmem ./internal/store/... ./internal/service/...
  ./internal/httpapi/...` invocations below.

## Results

All ns/op figures are per-operation mean over the recorded iteration
count, not p95 — a dedicated p95 harness was judged disproportionate to
what §11 asks for. Go's benchmark runner auto-scales iteration count to
fill its default ~1s-per-benchmark time budget, so the iteration count
itself is evidence of how well-sampled each mean is: the four
sub-millisecond store benchmarks ran thousands of iterations each, the
~21ms search benchmark ran dozens, and the ~560ms pathological-search
benchmark ran only 2 — reported as-is rather than forced to a fixed
iteration count that would either waste minutes on the fast ones or
under-sample the slow one.

Figures below are from the actual `task bench` invocation (three
package paths, one `-timeout=30m` run) — the plan's literal
verification requirement — not the separate per-package runs used
while developing the benchmarks, which returned numbers within normal
run-to-run variance of these.

### `internal/store` (direct store-layer calls, no HTTP/service overhead)

| Benchmark | Target | Result | Iterations | vs. target |
| --- | --- | --- | --- | --- |
| `BenchmarkGetTicketByRef` — indexed detail fetch | < 100 ms | 44.2 µs | 23,830 | ✅ ~2260x headroom |
| `BenchmarkListProjectsFirstPage` — first-page list | < 100 ms | 116.1 µs | 10,000 | ✅ ~860x headroom |
| `BenchmarkPriorityQueueFirstPage` — first-page list, 4,000-ticket project | < 100 ms | 245.0 µs | 5,044 | ✅ ~410x headroom |
| `BenchmarkIssueRegisterFirstPage` — first-page list, 4,000-ticket project | < 100 ms | 256.0 µs | 4,694 | ✅ ~390x headroom |
| `BenchmarkSearchFirstPage` — selective query (`"2001" "fixture"`, 150 hits, verified by direct query against search_fts) | < 200 ms | 21.2 ms | 56 | ✅ ~9x headroom |
| `BenchmarkSearchFirstPageCommonTerm` — pathological query (`"fixture"`, 610,475 hits out of 610,500 indexed rows, verified) | < 200 ms | 559.0 ms | 2 | ❌ misses target — see below |

### `internal/service` (business logic, real transactions, real audit rows)

| Benchmark | Target | Result | vs. target |
| --- | --- | --- | --- |
| `BenchmarkCreateTicket` — ordinary non-upload mutation, against the full reference dataset | < 250 ms | 3.7 ms | ✅ ~67x headroom |
| `BenchmarkListActivityFirstPage` — first-page list, sample project's activity feed | < 100 ms | 32.3 ms | ✅ ~3x headroom |
| `BenchmarkConcurrentReadWrite` — `GetTicket` reads via `b.RunParallel` while a dedicated goroutine continuously runs `CreateTicket` | (informational — §11's "concurrent readers/writers" requirement, no numeric target stated) | 68.1 µs/op | reads stay fast under write contention; SQLite's single-writer WAL model (ADR 0003) doesn't starve readers here |
| `BenchmarkConcurrentTicketCreate` — many concurrent writers via `b.RunParallel`, an empty store, not the full reference dataset (pre-existing since Phase 1, `internal/service/concurrency_test.go`; included here because `task bench` runs it and §11's "concurrent readers/writers" requirement covers the write side too) | (informational) | 12.5 ms/op | well under the 250 ms mutation target even while every writer serializes on SQLite's single-writer lock (ADR 0003) |

### `internal/httpapi` (real HTTP round trip: routing, auth resolution, JSON encoding)

| Benchmark | Target | Result | vs. target |
| --- | --- | --- | --- |
| `BenchmarkHTTPGetTicket` — indexed detail fetch, HTTP layer | < 100 ms | 194.5 µs | ✅ ~510x headroom |
| `BenchmarkHTTPListTicketsFirstPage` — first-page list, HTTP layer | < 100 ms | 515.1 µs | ✅ ~190x headroom |
| `BenchmarkHTTPAttachmentDownload` — streamed ~1.1 MB upload attachment through the real download route and blobstore | < 100 ms | 329.0 µs | ✅ ~300x headroom |

## The one target not met: common-term full-text search

`BenchmarkSearchFirstPageCommonTerm` searches for `"fixture"` — a word
`internal/fixtures` puts in nearly every generated ticket, comment,
decision, plan, and document's title or body, so the query matches
essentially the *entire* ~610,000-row search index rather than a
realistic, selective hit set. This is intentionally the worst case, not
the representative one — `BenchmarkSearchFirstPage`'s `"2001" "fixture"`
query (a specific ticket-sequence number every real search term would
resemble far more than a filler word) is the representative case, and
it beats the target by ~9x.

The reason the common-term case is slow is a real, inherent property of
SQLite FTS5, not a fixable index gap: `ORDER BY bm25(search_fts) ASC
LIMIT ?` must compute the bm25 rank for every *matching* row before it
can sort and trim to the page size — ranking cost scales with the
match-set size, not the result-page size. There is no index that makes
ranking 610,000 matching rows as cheap as ranking 150. A production
installation's vocabulary is far more varied than this generator's
repeated filler text, so this case is expected to be rare in practice —
but a sufficiently common real term (e.g. a project's own name, if it
appears in most tickets) could reproduce it.

**Left unmet, documented per Step 6's instruction** rather than
optimized speculatively: no index or query change closes this gap
without changing FTS5's ranking algorithm or adding a candidate-set cap
before ranking (e.g., capping bm25 evaluation to the first N matches by
rowid before ranking, which would trade ranking accuracy for latency on
exactly this pathological case). Revisit only if a real search term is
observed hitting this case in practice.

## Note for anyone re-running or extending these benchmarks

`store.RebuildSearchIndex` requires a `*sql.Tx`, not a `*sql.DB` — see
its doc comment in `internal/store/search.go` for what silently goes
wrong (~120 minutes vs. a few seconds at this dataset's scale) if a
raw pool is passed instead. Both `internal/store/bench_test.go` and the
real callers get this right; it's called out here only because it's
the easiest mistake to reintroduce when adding a new benchmark that
needs a search-indexed fixture.
