# 0001: Project → Feature → Ticket hierarchy with a mandatory General feature

## Context

Product spec §5.1 fixes the canonical hierarchy as `Project → Feature →
Ticket`, with every ticket required to belong to exactly one feature.
Small, ungrouped work is common (personal use is the primary target
per §2), so a strict hierarchy risks friction unless there is always a
feature to fall back to.

## Decision

- Every project has exactly one system-created `General` feature,
  created in the same transaction as the project itself.
- `POST /projects/{key}/tickets` with no `feature` field resolves to
  `General`.
- A ticket can move between features within its project later
  (`PATCH .../tickets/{ref}` with a new `feature_id`), but never
  between projects — cross-project movement is out of scope (§5.1);
  use `supersedes` plus a new ticket instead.
- `General` is an ordinary feature, not a special-cased sentinel: it
  has the same workflow, priority, and position semantics as any
  other feature. Nothing except its creation is automatic.

## Consequences

- No nullable `feature_id` column on `tickets`, and no branching in
  every ticket-reading query to handle "no feature."
- The `General` feature can be renamed, reprioritized, or (in a later
  phase) archived like any other feature — the model does not treat it
  as immutable, only its existence at project-creation time is
  guaranteed.
- Deleting/archiving a project's last feature is out of scope for the
  MVP; `General` is never deleted by ordinary workflows since archival
  is soft (§5.12).
