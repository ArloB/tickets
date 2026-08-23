import { apiFetch } from './client'
import { entityPathSegment } from './refs'
import type { AssociationsPage } from './types'

/** Associations span tickets, features, and decisions (product spec
 * §5.7) — one non-directional edge type, `associated_with`, unlike
 * relationships (ticket-only, directed, typed). */
export async function listAssociations(ref: string): Promise<AssociationsPage> {
  return apiFetch<AssociationsPage>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/associations`)
}

export async function addAssociation(ref: string, target: string): Promise<void> {
  await apiFetch<void>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/associations`, {
    method: 'POST',
    body: { target },
  })
}

export async function removeAssociation(ref: string, target: string): Promise<void> {
  await apiFetch<void>(
    `/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/associations/${encodeURIComponent(target)}`,
    { method: 'DELETE' },
  )
}
