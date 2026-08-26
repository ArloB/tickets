# Row 10's live-agent drill

`docs/mvp-acceptance.md` row 10 (product spec §16 criterion 10) asks
something no Go test can answer: whether a **live LLM agent**, reading
the MCP tool descriptions as they actually ship, picks the right tools
and completes the representative ticket workflow — in both Claude Code
and Codex. `cmd/tickets/exit_criterion_phase3_test.go` proves the
protocol and the schemas; it cannot prove an agent understands them.

`tools/row10drill` closes that gap by automating what the row's manual
runbook described. Run it with:

```
task acceptance:row10                      # 1 run per host
task acceptance:row10 RUNS=5               # a pass rate worth quoting
task acceptance:row10 HOSTS=claude RUNS=3  # one host only
```

It is deliberately **not** part of `task ci` — see "Why this is opt-in".

## What one run does

Each run is hermetic and disposable, so hosts are compared against an
identical pristine fixture:

1. Creates a temp data directory and picks a free loopback port.
2. `tickets setup`, then starts `tickets server` against it.
3. Creates the `drill` agent and issues it a bearer token.
4. Seeds a project and one high-priority task **assigned to
   `agent:drill`**, so there is assigned work to find.
5. Launches the agent host headless, with the Tickets MCP server as its
   **only** MCP server, and the workflow as its prompt.
6. Parses the host's JSONL transcript into a tool-call sequence.
7. Inspects the resulting server state through the CLI's JSON output.
8. Tears the server and data directory down.

Transcripts, host stderr, server logs, and `summary.json` are written to
`artifacts/row10/`.

## Two tiers of assertion

The distinction matters, because only one of them is mechanical enough
to gate on.

**Hard gate — fails the run.** Verified against server state, not the
transcript, because transcript formats drift between CLI versions and
the audit trail does not:

- the ticket ended at status `done`,
- the agent made **at least two** status changes to it. Starting and
  completing are separate steps in §16's workflow, and a final status
  of `done` alone does not distinguish an agent that started the ticket
  from one that jumped `backlog → done`. The activity feed records that
  a status changed but not the target status, so two agent-attributed
  `ticket_status_changed` events is the available proxy for a distinct
  start transition,
- it is still assigned to `agent:drill`,
- at least one comment on it is attributed to `agent:drill`,
- at least one decision exists in the project,
- no MCP tool call was rejected by the **tool surface** — that is, no
  call failed for a reason other than one of the project's own error
  codes (`not_found`, `validation_failed`, `version_conflict`, …).

Domain errors are recorded but do **not** fail a run. An agent that
speculatively reads a record which doesn't exist and gets a clean
`not_found` has used the tool correctly and received a correct answer;
that is normal exploration, not confusion about a description. A call
the surface *rejects* is the opposite: the agent could not form a valid
call from what the schema told it. Only the second is a defect in what
this drill measures.

The classification fails closed: an error is treated as a tool-surface
rejection unless it carries one of the error codes in
`docs/contracts/errors.md`. The host CLIs' error prose can change
without notice, so matching their phrasing to detect schema failures
would let a real rejection reclassify itself as benign; matching *our*
stable codes to detect benign ones cannot fail that way.

One consequence worth stating: since `errMissingProjectKey` returns
`validation_failed`, an agent that omits the project key entirely on
`project_get`/`project_brief` produces a domain error and does not fail
the run. That is deliberate — the bridge's `--project` default makes an
omitted key legitimate (ADR 0022) — but it means the drill no longer
catches an agent that never works out how to name a project, and the
`state` assertions above are what stand in its place.

**Recorded metric — reported, never fails the run.** This is the
evidence a human reads when judging "without the agent needing extra
discovery calls or getting confused by a tool description":

- the full tool-call sequence,
- the total call count,
- which tickets tool was called *first*.

"First call" is recorded rather than asserted on purpose. Both hosts
have opened with `projects_list` before `project_brief` — reasonable
orientation when the prompt names no project key, not obviously a
defect. Turning that into a pass/fail would encode one debatable
judgement as a hard rule; recording it lets a reader see the pattern
across runs and decide.

Host-side tooling is filtered out of the sequence: only
`mcp__tickets__*` calls are counted. Claude Code's own `ToolSearch`
calls, for instance, are an artifact of its deferred tool loading and
say nothing about the Tickets tool descriptions.

## Host flags that matter

These were established empirically; several non-obvious ones cost real
debugging time, so they are recorded rather than left to be rediscovered.

**Claude Code** (`tools/row10drill/main.go`'s `claudeCommand`):

- `--strict-mcp-config` is a **measurement-validity** requirement, not
  hygiene. Without it the subprocess inherits whatever MCP servers the
  caller has configured, and the drill is no longer measuring whether an
  agent can navigate the Tickets tool surface alone.
- `--allowed-tools mcp__tickets` permits the MCP tools without an
  interactive prompt. This is preferred over blanket permission
  bypassing: it is both safer and a tighter measurement.
- `--output-format stream-json` requires `--verbose` to emit the
  per-message events the parser reads.

**Codex** (`codexCommand`):

- **stdin must be redirected from `/dev/null`.** Otherwise `codex exec`
  blocks indefinitely waiting for input it was never going to get, and
  the run dies on the timeout with an empty transcript.
- `--approve-for-me` is what actually lets MCP tool calls through.
  Two plausible-looking alternatives do **not** work: `-c
  approval_policy="never"` and `--disable guardian_approval` both leave
  every call failing with `user cancelled MCP tool call`.
- `--ignore-user-config` is Codex's equivalent of `--strict-mcp-config`;
  authentication still resolves through `CODEX_HOME`.
- `--ephemeral` keeps drill sessions out of the caller's session history.
- A `codex_models_manager: failed to load models cache` line on stderr is
  benign and does not affect the run.

The transcript parsers are the most fragile part of the harness, since
both event schemas belong to third-party CLIs. They are therefore
isolated in `transcript.go` and unit-tested against recorded fixtures
(`transcript_test.go`, `testdata/`) — if either schema drifts, a test
fails instead of the call count silently reading zero.

## Why this is opt-in

Three reasons it must never join `task ci`:

- it drives real LLM hosts, so it needs the caller's own `claude` and
  `codex` credentials and consumes tokens;
- it takes minutes per run;
- **it is stochastic.** Agent behaviour varies run to run. A single
  green run is evidence, not proof, and reporting one as "covered"
  would be exactly the kind of overclaim `docs/mvp-acceptance.md` has
  otherwise avoided. Quote a pass rate over `RUNS=5` or more, and state
  it as a pass rate.

One honest caveat on the method: these are non-interactive (`-p` /
`exec`) sessions. The system prompts differ slightly from interactive
mode, so this measures the tool descriptions under headless conditions —
which is the same surface an agent host sees either way, but not
literally an interactive session.

## Findings

**Fixed: the tool surface used three names for the project key.**
The drill's first finding, and the reason a Codex run failed the hard
gate. The agent called `project_brief {"project_key": …}`, got
`-32602: invalid arguments`, and retried as `{"key": …}` — in 2 of 3
Codex sessions; Claude Code never hit it.

`internal/mcpsrv/tools.go` spelled the project key three ways: `key`
where the project was the tool's subject (`project_brief`,
`project_get`, `project_create`, `project_update`), `project` on
`search`, and `project_key` on the tools where it scoped the call
(`tickets_list`, `features_list`, `record_create`, and friends).
`docs/mcp-agent-guide.md` documented only `project_key`, so an agent
following the project's own guidance produced exactly the failing call.

Every tool now spells it `project_key` — see ADR 0022 for the decision,
including why no `key` alias was added.
`internal/mcpsrv.TestEveryToolNamesTheProjectKeyIdentically` guards it
by listing the live tool surface and rejecting any `key`/`project`
input property; it was confirmed to fail when the old name is restored.

This is the finding worth generalizing: **no Go test could have caught
it.** A test calls a tool with the argument names the code declares, so
it can never discover that a reader of the surface would guess a
different one. Tests and tool schemas share an author; live agents do
not.

## Results so far

Harness runs only — the two earlier hand-run sessions used to develop
the host flags are excluded, since they used different prompts:

Against the tool surface as it stands after ADR 0022, with `RUNS=2`:

| Host | Runs | Passed | Notes |
| --- | --- | --- | --- |
| Claude Code | 2 | 2 | 17 tool calls each, no errors of any kind |
| Codex | 2 | 2 | 12-19 tool calls, no errors of any kind |

Before the fix, Codex failed 1 of 2 runs on the `project_key`
rejection, and an earlier Claude run failed on a speculative
`record_get` returning `not_found` — the run that showed the gate
itself was miscategorising domain errors. Both classes are gone: the
first because the defect is fixed, the second because a `not_found` on
a record that was never created is no longer treated as a failure.

Both hosts completed §16's workflow with correct server state in every
run. These counts are still small — re-run with `RUNS=5` before quoting
a rate as settled.
