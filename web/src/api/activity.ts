import { apiFetch } from './client'
import type { ActivityPage } from './types'

export interface ListActivityOptions {
  actor?: string
  entityKind?: string
  eventType?: string
  cursor?: string
}

export async function listActivity(
  projectKey: string,
  opts: ListActivityOptions = {},
): Promise<ActivityPage> {
  const params = new URLSearchParams()
  if (opts.actor) params.set('actor', opts.actor)
  if (opts.entityKind) params.set('entity_kind', opts.entityKind)
  if (opts.eventType) params.set('event_type', opts.eventType)
  if (opts.cursor) params.set('cursor', opts.cursor)
  const query = params.toString() ? `?${params.toString()}` : ''
  return apiFetch<ActivityPage>(`/projects/${encodeURIComponent(projectKey)}/activity${query}`)
}
