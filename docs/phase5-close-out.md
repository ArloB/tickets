# Phase 5 close-out punch list

Items surfaced during Phase 5 Steps 1–6 that don't map cleanly to any
later step (7–9) and have no other durable home — `plan.md` has no
committed Phase 5 execution plan (only the high-level bullet list in
§14), so anything discovered mid-implementation that isn't purely a
single ADR's own concern needs to land somewhere a close-out pass will
actually look. This file is that place. Step 9 (Phase 5 close-out)
should read this list, resolve or consciously re-defer each item, and
then this file can go away.

## 1. Attachment file names and external link titles/URLs are not indexed by search

**Where:** `docs/adr/0018-unified-search-index.md`, Consequences.

Search (Step 6) indexes ticket/feature/decision/plan/document/comment
text only. An attachment's file name and an external link's title/URL
are not folded in, incrementally or by `RebuildSearchIndex` — a
deliberate scope cut made during Step 6, not an oversight, but never
weighed against Phase 5's actual exit criterion ("search finds all
indexed kinds," `plan.md` §14). Decide before close-out: build it, or
explicitly accept the gap in the exit-criterion checklist rather than
letting it fall out silently.

## 2. "Project-brief view" has no owning step

**Where:** `docs/mcp-agent-guide.md`, line ~148 ("A project-brief
view — deferred to Phase 5 (a useful brief wants decisions/plans in a
more complete form than Phase 3's minimal decisions slice provides).")

This note was written during Phase 3. Checked against both `plan.md`
§14's Phase 5 bullet list and the 9-step Phase 5 plan (Steps 1–9:
activity feed, decision versioning, content_items, attachments,
representations, search, subscriptions/notifications, SSE, close-out)
— it appears in neither. Unlike the backup-tooling and `@actor`-mention
deferrals (ADR 0011, ADR 0015), which each resolved into a real later
phase/step, this one has nothing to land in. At close-out: either
scope it into a step (most naturally Step 7, alongside the activity/
notification work) or update the mcp-agent-guide.md note to say it's
still unscheduled rather than "deferred to Phase 5."

## 3. General feature deletion/archival is a permanent non-goal not listed in §3.2

**Where:** `docs/adr/0001-hierarchy-and-general-feature.md`,
Consequences ("Deleting/archiving a project's last feature is out of
scope for the MVP").

Low risk — phrased as permanent, not phase-deferred, and `General`
being undeletable is enforced in code (`DeleteFeature`'s check). But
it isn't listed alongside `plan.md` §3.2's other explicit MVP
non-goals, so a reader auditing scope from §3.2 alone would miss it.
Worth a one-line addition to §3.2 at close-out for consistency, not
urgent.
