import { apiFetch } from './client'

// Reference-kind sniffing shared by every cross-entity-kind API
// (associations/links/backlinks route under /tickets|features|decisions
// interchangeably, docs/contracts/references.md's {KEY}-F{seq}/
// {KEY}-D{seq}/{KEY}-{seq} shapes) and by route-building components.

export type EntityKind = 'ticket' | 'feature' | 'decision' | 'plan' | 'document' | 'project'

// A project has no seq-numbered reference token (domain.Format rejects
// KindProject server-side) — it's named by its bare key instead, e.g.
// "ABC". Mirrors internal/domain/reference.go's projectKeyPattern.
const projectKeyPattern = /^[A-Z][A-Z0-9]{1,9}$/

export function entityKindOfRef(ref: string): EntityKind {
  // DOC is tried before F/D so "ABC-DOC9" doesn't spuriously match the
  // "D" branch below, mirroring internal/domain/reference.go's
  // longest-first kind-code alternation.
  if (/-DOC\d+$/.test(ref)) return 'document'
  if (/-F\d+$/.test(ref)) return 'feature'
  if (/-D\d+$/.test(ref)) return 'decision'
  if (/-P\d+$/.test(ref)) return 'plan'
  if (projectKeyPattern.test(ref)) return 'project'
  return 'ticket'
}

/** The project key a reference belongs to — the whole token for a
 * bare project key, everything before the first '-' otherwise. This is
 * the scope the ticket-only short form (#123) resolves against, the
 * client-side counterpart of the scopeProjectKey internal/service
 * passes to domain.ScanReferences for a body it is about to store. */
export function projectKeyOfRef(ref: string): string {
  const dash = ref.indexOf('-')
  return dash === -1 ? ref : ref.slice(0, dash)
}

const pathSegment: Record<EntityKind, string> = {
  ticket: 'tickets',
  feature: 'features',
  decision: 'decisions',
  plan: 'plans',
  document: 'documents',
  project: 'projects',
}

/** e.g. "tickets" for "ABC-1", "features" for "ABC-F1" — the URL
 * segment addLink/listLinks/backlinks/associations/comments share
 * across every entity kind. */
export function entityPathSegment(ref: string): string {
  return pathSegment[entityKindOfRef(ref)]
}

/** Client-side route for a bare reference — used to link relationship/
 * association/backlink targets to the right detail view. */
export function detailRoute(ref: string): string {
  const kind = entityKindOfRef(ref)
  if (kind === 'feature') return `/features/${ref}`
  if (kind === 'decision') return `/decisions/${ref}`
  if (kind === 'plan') return `/plans/${ref}`
  if (kind === 'document') return `/documents/${ref}`
  if (kind === 'project') return `/projects/${ref}`
  return `/tickets/${ref}`
}

/** One reference token's resolution, as returned by
 * GET /refs/resolve — the existence check behind rendering a
 * reference in Markdown prose as a hyperlink (ADR 0025). kind/title/
 * status are present only when exists is true. */
export interface ResolvedRef {
  ref: string
  exists: boolean
  kind?: EntityKind
  title?: string
  status?: string
}

// Mirrors internal/service's maxResolveRefs — the server rejects a
// larger batch outright, so the client chunks rather than discovering
// the cap as a 400.
const resolveBatchSize = 50

/** Resolves reference tokens to whether each names a live record,
 * chunked to the server's per-request cap. Tokens are passed through
 * verbatim; the caller expands the project-scoped short form (#123)
 * itself, since only it knows the surrounding project scope. */
export async function resolveRefs(refs: string[]): Promise<ResolvedRef[]> {
  const out: ResolvedRef[] = []
  for (let i = 0; i < refs.length; i += resolveBatchSize) {
    const batch = refs.slice(i, i + resolveBatchSize)
    const page = await apiFetch<{ refs: ResolvedRef[] }>(
      `/refs/resolve?refs=${encodeURIComponent(batch.join(','))}`,
    )
    out.push(...page.refs)
  }
  return out
}
