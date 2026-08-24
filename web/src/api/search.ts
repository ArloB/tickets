import { apiFetch } from './client'
import type { SearchKind, SearchPage } from './types'

export interface SearchOptions {
  project?: string
  kinds?: SearchKind[]
  status?: string
  cursor?: string
}

export async function search(query: string, opts: SearchOptions = {}): Promise<SearchPage> {
  const params = new URLSearchParams()
  params.set('q', query)
  if (opts.project) params.set('project', opts.project)
  if (opts.kinds && opts.kinds.length > 0) params.set('kind', opts.kinds.join(','))
  if (opts.status) params.set('status', opts.status)
  if (opts.cursor) params.set('cursor', opts.cursor)
  return apiFetch<SearchPage>(`/search?${params.toString()}`)
}
