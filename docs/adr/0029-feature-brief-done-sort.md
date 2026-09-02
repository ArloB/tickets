# 0029: project_brief's Features section sorts done/cancelled last

## Context

A follow-on review of ADR 0028's discoverability fix (content item
archive status) asked whether the same failure shape existed anywhere
else in `project_brief`. It does, in `Service.briefFeatures`
(`internal/service/project_brief.go`): `store.ListFeaturesForProject`
had no status filter or sort at all, ordering strictly by
`(priority_rank, position)` and letting `briefFeatures` truncate
whatever came back to `briefSectionLimit` (20). A project with more
than 20 features where some historical batch of done/cancelled ones
outranks the still-active ones by priority — plausible after the same
kind of bulk migration that motivated ADR 0028 — would fill the entire
Features section with finished work and push active features out
entirely. Unlike tickets (`briefTickets` explicitly page-walks past
done/cancelled) or decisions (`RecentAcceptedDecisions` filters to
`status = 'accepted'`), the Features section had no such protection.

## Decision

**Sort, not exclude.** `ListFeaturesForProject`'s query gained a
leading `ORDER BY (f.status IN ('done', 'cancelled')) ASC` key ahead of
its existing `priority_rank, position` ordering — done/cancelled
features still appear, just after every active one, so a done feature
only occupies a brief slot once there's no active feature left to fill
it. This is deliberately different from ADR 0028's fix (which excludes
archived content items from `RecentContentItems` outright) and from
`briefTickets`' own done/cancelled exclusion:

- Features have no archive flag to filter by in the first place — done
  is itself meaningful, current-ish status for what §5.4 calls a
  small, bounded grouping, not a 33-item migration backlog of
  superseded design docs.
- A project's feature count is small (unpaginated by design — see
  `ListFeaturesForProject`'s own doc comment), so there's much less
  risk of a done feature meaningfully crowding out an active one the
  way old plans did — sorting last is enough; full exclusion would
  hide real, current-ish state (`TicketsTotal`/`TicketsDone` progress)
  for no real benefit.

`ListFeaturesForProject` is called from exactly one place
(`briefFeatures`), so changing its ordering has no other caller to
consider.

## Consequences

- No migration: this is a `SELECT`'s `ORDER BY`, not a schema change.
- `internal/store/features_test.go` gained
  `TestListFeaturesForProjectSortsDoneAndCancelledLast`;
  `internal/service/project_brief_test.go` gained
  `TestProjectBriefFeaturesSurviveManyDoneFeatures`, mirroring
  `TestProjectBriefInProgressSurvivesManyDoneTickets`'s shape for
  tickets.
- `GET /projects/{key}/features` and `feature_list`/`records`-style
  paginated listings are unaffected — they go through
  `ListFeaturesForProjectPage`, a separate query this ADR didn't touch.
