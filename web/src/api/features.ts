import { apiFetch } from './client'
import type {
  BacklinksPage,
  FeatureDetail,
  FeaturesPage,
  LinksPage,
  Priority,
  WorkflowStatus,
} from './types'

export interface FeatureListFilters {
  status?: WorkflowStatus
  priority?: Priority
  creator?: string
  updatedSince?: string
}

export async function listFeatures(
  projectKey: string,
  filters: FeatureListFilters = {},
  cursor?: string,
): Promise<FeaturesPage> {
  const params = new URLSearchParams()
  if (filters.status) params.set('status', filters.status)
  if (filters.priority) params.set('priority', filters.priority)
  if (filters.creator) params.set('creator', filters.creator)
  if (filters.updatedSince) params.set('updated_since', filters.updatedSince)
  if (cursor) params.set('cursor', cursor)
  const query = params.toString()
  return apiFetch<FeaturesPage>(
    `/projects/${encodeURIComponent(projectKey)}/features${query ? `?${query}` : ''}`,
  )
}

export async function getFeature(ref: string): Promise<FeatureDetail> {
  return apiFetch<FeatureDetail>(`/features/${encodeURIComponent(ref)}`)
}

export async function listFeatureLinks(ref: string): Promise<LinksPage> {
  return apiFetch<LinksPage>(`/features/${encodeURIComponent(ref)}/links`)
}

export async function listFeatureBacklinks(ref: string): Promise<BacklinksPage> {
  return apiFetch<BacklinksPage>(`/features/${encodeURIComponent(ref)}/backlinks`)
}
