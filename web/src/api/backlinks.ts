import { apiFetch } from './client'
import { entityPathSegment } from './refs'
import type { BacklinksPage } from './types'

export async function listBacklinks(ref: string): Promise<BacklinksPage> {
  return apiFetch<BacklinksPage>(`/${entityPathSegment(ref)}/${encodeURIComponent(ref)}/backlinks`)
}
