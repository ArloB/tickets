import { apiFetch } from './client'
import { entityPathSegment } from './refs'
import type { ExternalLink, LinksPage } from './types'

/** Links span tickets, features, and decisions and have no version
 * column (docs/contracts/concurrency.md's Phase 4 addendum) — add and
 * delete only, no in-place edit; change title/URL by removing and
 * re-adding. */
export async function listLinks(ref: string): Promise<LinksPage> {
  return apiFetch<LinksPage>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/links`)
}

export async function addLink(ref: string, title: string, url: string): Promise<ExternalLink> {
  return apiFetch<ExternalLink>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/links`, {
    method: 'POST',
    body: { title, url },
  })
}

export async function removeLink(ref: string, linkId: number): Promise<void> {
  await apiFetch<void>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/links/${linkId}`, {
    method: 'DELETE',
  })
}
