import { apiFetch } from './client'
import type { RelationshipsPage, RelationshipType } from './types'

/** Relationships are ticket-only, directed, typed edges (product spec
 * §5.7) — unlike associations, which span all three entity kinds and
 * carry no type. */
export async function listRelationships(ref: string): Promise<RelationshipsPage> {
  return apiFetch<RelationshipsPage>(`/tickets/${encodeURIComponent(ref)}/relationships`)
}

export async function addRelationship(
  ref: string,
  target: string,
  type: RelationshipType,
): Promise<void> {
  await apiFetch<void>(`/tickets/${encodeURIComponent(ref)}/relationships`, {
    method: 'POST',
    body: { target, type },
  })
}

export async function removeRelationship(
  ref: string,
  type: RelationshipType,
  target: string,
): Promise<void> {
  await apiFetch<void>(
    `/tickets/${encodeURIComponent(ref)}/relationships/${type}/${encodeURIComponent(target)}`,
    { method: 'DELETE' },
  )
}
