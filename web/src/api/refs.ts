// Reference-kind sniffing shared by every cross-entity-kind API
// (associations/links/backlinks route under /tickets|features|decisions
// interchangeably, docs/contracts/references.md's {KEY}-F{seq}/
// {KEY}-D{seq}/{KEY}-{seq} shapes) and by route-building components.

export type EntityKind = 'ticket' | 'feature' | 'decision'

export function entityKindOfRef(ref: string): EntityKind {
  if (/-F\d+$/.test(ref)) return 'feature'
  if (/-D\d+$/.test(ref)) return 'decision'
  return 'ticket'
}

const pathSegment: Record<EntityKind, string> = {
  ticket: 'tickets',
  feature: 'features',
  decision: 'decisions',
}

/** e.g. "tickets" for "ABC-1", "features" for "ABC-F1" — the URL
 * segment addLink/listLinks/backlinks/associations share across all
 * three entity kinds. */
export function entityPathSegment(ref: string): string {
  return pathSegment[entityKindOfRef(ref)]
}

/** Client-side route for a bare reference — used to link relationship/
 * association/backlink targets to the right detail view. */
export function detailRoute(ref: string): string {
  const kind = entityKindOfRef(ref)
  if (kind === 'feature') return `/features/${ref}`
  if (kind === 'decision') return `/decisions/${ref}`
  return `/tickets/${ref}`
}
