import { apiFetch, ifMatchHeader } from './client'
import type { FeatureDetail, FeaturesPage, Priority, WorkflowStatus } from './types'

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

export interface CreateFeatureInput {
  title: string
  description: string
  priority: Priority
}

export async function createFeature(
  projectKey: string,
  input: CreateFeatureInput,
): Promise<FeatureDetail> {
  return apiFetch<FeatureDetail>(`/projects/${encodeURIComponent(projectKey)}/features`, {
    method: 'POST',
    body: input,
  })
}

export interface UpdateFeatureInput {
  title: string
  description: string
  priority: Priority
}

/** PATCH /features/{ref} is a full-representation update despite the
 * verb (internal/httpapi/features.go's updateFeatureRequest has no
 * partial-update semantics) — resend every field. There is no
 * feature-status endpoint; features have no independent status
 * transition in this API. */
export async function updateFeature(
  ref: string,
  input: UpdateFeatureInput,
  expectedVersion: number,
): Promise<FeatureDetail> {
  return apiFetch<FeatureDetail>(`/features/${encodeURIComponent(ref)}`, {
    method: 'PATCH',
    body: input,
    headers: ifMatchHeader(expectedVersion),
  })
}
