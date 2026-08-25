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
- **Build:** `go version go1.26.6 linux/amd64`, commit `c88f678`
  (working tree at Phase 7 Step 3, immediately before this document's
  own commit).
- **Warm/cold state, defined precisely (Phase 7):** the previous
  wording here — "first run this process," not independently
  controlled cold vs. warm — was itself the gap `plan.md:501`'s
  "benchmarks must record ... cold/warm state" was asking to close, not
  a description of having closed it. Fixed by defining the two terms
  this project can actually measure and stating plainly what's still
  uncontrolled, rather than promising a step that isn't portable
  (dropping the OS page cache needs root on Linux and has no Windows
  equivalent, and this project targets both — ADR 0003):
  - **Cold**, in the sense this document can actually claim: a fresh
    process, against a database generated immediately beforehand and
    never previously read in-process — true of every benchmark's
    *first* `go test -bench=.` invocation against a freshly-built
    fixture, which is what every run recorded here is (the fixture is
    built once per package process; nothing warms it across separate
    `go test` invocations).
  - **Warm:** a repeat call in the same process, against the same
    connection, after SQLite's own page cache and prepared-statement
    cache have had a chance to populate — what the `ns/op` mean and
    p95 columns below measure, since both average/rank over `b.N`
    iterations within one already-running process.
  - **Not controlled either way:** the OS filesystem page cache
    underneath SQLite's own, which this benchmark run doesn't reset
    between packages. A dedicated single-process-per-run harness would
    be needed to control it, and isn't warranted unless a real
    regression is suspected.
  - **What `benchP95`'s "first-iter-ms/op" is NOT**: an attempt at a
    true cold-start metric. Go's benchmark runner re-invokes the whole
    `Benchmark` function — calibration passes included — before
    settling on the final `b.N`, so the first individual iteration
    it reports is preceded by every one of those calibration passes;
    it is not the process's actual first call. Measured across several
    runs it moved by up to ~4x and was sometimes *faster* than the
    reported p95 (a true cold start, being slower than steady state by
    definition, never would be) — direct evidence it's one noisy
    sample of steady-state variance, not a cold measurement. It's kept
    in the tables below as a single-sample sanity check only; do not
    read it as "the cold number."
- **Run command:** `task bench`, or the per-package `go test -run '^$'
  -bench=. -benchmem ./internal/store/... ./internal/service/...
  ./internal/httpapi/...` invocations below.

## Results

Two figures per benchmark below: **mean** (`ns/op`, Go's built-in
metric — every iteration averaged, i.e. the warm-state number per the
definitions above) and, for the three benchmarks backing §11's three
numeric targets, **p95** (a custom `p95-ms/op` metric from `benchP95`,
`internal/store/bench_test.go` — duplicated verbatim in
`internal/service/bench_test.go` per this codebase's stated preference
for duplication over cross-package sharing, ADR 0011). §11 states its
targets as p95, not mean; the previous version of this document
reported means only and called a dedicated p95 harness
"disproportionate" — Phase 7 closed that gap since it's a small,
targeted addition, not the parallel harness that judgment call was
declining. The other ten benchmarks still report mean only: a full p95
harness for all thirteen remains disproportionate to what §11 asks
for, since §11 names only three target categories. Those same three
benchmarks also report a "1st iter" column — see the warm/cold section
above for exactly what it is (a noisy single sample) and is not (a
cold-start measurement).

Go's benchmark runner auto-scales iteration count to fill its default
~1s-per-benchmark time budget, so the iteration count itself is
evidence of how well-sampled each mean is: the sub-millisecond store
benchmarks ran thousands of iterations each, the ~21ms search
benchmark ran dozens, and the ~550ms pathological-search benchmark ran
only 2 — reported as-is rather than forced to a fixed iteration count
that would either waste minutes on the fast ones or under-sample the
slow one.

Figures below are from an actual `go test -bench=. -benchmem
-timeout=30m` run across all three package paths (Phase 7), not the
separate per-package runs used while developing the benchmarks, which
returned numbers within normal run-to-run variance of these.

### `internal/store` (direct store-layer calls, no HTTP/service overhead)

| Benchmark | Target | Mean | 1st iter (noisy, not "cold") | p95 | Iterations | vs. target |
| --- | --- | --- | --- | --- | --- | --- |
| `BenchmarkGetTicketByRef` — indexed detail fetch | p95 < 100 ms | 43.3 µs | 0.138 ms | 0.054 ms | 27,828 | ✅ ~1850x headroom (p95) |
| `BenchmarkListProjectsFirstPage` — first-page list | < 100 ms | 116.1 µs | — | — | 10,000 | ✅ ~860x headroom (mean) |
| `BenchmarkPriorityQueueFirstPage` — first-page list, 4,000-ticket project | < 100 ms | 258.2 µs | — | — | 4,639 | ✅ ~390x headroom (mean) |
| `BenchmarkPriorityQueueFilteredByAssignee` — selective `assignee` filter, same 4,000-ticket project (Phase 7 — the benchmark `docs/contracts/list-filters.md`'s "Index coverage" section said was needed before assuming a filtered query meets target) | p95 < 100 ms | 1.15 ms | 1.14 ms | 1.37 ms | 1,014 | ✅ ~73x headroom (p95) — the scan-and-discard pattern that section describes is real (~5x slower than the unfiltered priority queue's p95) but comfortably inside target; no covering index added |
| `BenchmarkIssueRegisterFirstPage` — first-page list, 4,000-ticket project | < 100 ms | 251.4 µs | — | — | 4,819 | ✅ ~400x headroom (mean) |
| `BenchmarkSearchFirstPage` — selective query (`"2001" "fixture"`, 150 hits, verified by direct query against search_fts) | p95 < 200 ms | 20.8 ms | 21.71 ms | 22.22 ms | 55 | ✅ ~9x headroom (p95) |
| `BenchmarkSearchFirstPageCommonTerm` — pathological query (`"fixture"`, 610,475 hits out of 610,500 indexed rows, verified) | < 200 ms | 553.8 ms | — | — | 2 | ❌ misses target — see below |

### `internal/service` (business logic, real transactions, real audit rows)

| Benchmark | Target | Mean | 1st iter (noisy, not "cold") | p95 | vs. target |
| --- | --- | --- | --- | --- | --- |
| `BenchmarkCreateTicket` — ordinary non-upload mutation, against the full reference dataset | p95 < 250 ms | 4.0 ms | 4.81 ms | 7.33 ms | ✅ ~34x headroom (p95) |
| `BenchmarkListActivityFirstPage` — first-page list, sample project's activity feed | < 100 ms | 30.0 ms | — | — | ✅ ~3x headroom (mean) |
| `BenchmarkConcurrentReadWrite` — `GetTicket` reads via `b.RunParallel` while a dedicated goroutine continuously runs `CreateTicket` | (informational — §11's "concurrent readers/writers" requirement, no numeric target stated) | 62.2 µs/op | reads stay fast under write contention; SQLite's single-writer WAL model (ADR 0003) doesn't starve readers here |
| `BenchmarkConcurrentTicketCreate` — many concurrent writers via `b.RunParallel`, an empty store, not the full reference dataset (pre-existing since Phase 1, `internal/service/concurrency_test.go`; included here because `task bench` runs it and §11's "concurrent readers/writers" requirement covers the write side too) | (informational) | 13.5 ms/op | well under the 250 ms mutation target even while every writer serializes on SQLite's single-writer lock (ADR 0003) |

Rows with no 1st-iter/p95 columns filled in are the ten benchmarks
`benchP95` wasn't applied to (see "Results" above for why); their
"vs. target" is judged against the mean, same as before Phase 7. Where
p95 is available, "vs. target" is judged against p95, matching what
§11 actually specifies. The mean-vs-p95 headroom relationship isn't
consistent across these three — `GetTicketByRef`'s p95 is *below* its
mean (a right-skewed distribution where most calls are faster than
average, pulled up by rare slow ones outside the 95th percentile),
while `CreateTicket`'s p95 is well above its mean — so read each
column as its own number, not as a scaled version of the others.

### `internal/httpapi` (real HTTP round trip: routing, auth resolution, JSON encoding)

| Benchmark | Target | Result | vs. target |
| --- | --- | --- | --- |
| `BenchmarkHTTPGetTicket` — indexed detail fetch, HTTP layer | < 100 ms | 180.0 µs | ✅ ~560x headroom |
| `BenchmarkHTTPListTicketsFirstPage` — first-page list, HTTP layer | < 100 ms | 498.4 µs | ✅ ~200x headroom |
| `BenchmarkHTTPAttachmentDownload` — streamed ~1.1 MB upload attachment through the real download route and blobstore | < 100 ms | 301.3 µs | ✅ ~330x headroom |

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
