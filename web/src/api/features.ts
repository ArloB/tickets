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
  limit?: number,
): Promise<FeaturesPage> {
  const params = new URLSearchParams()
  if (filters.status) params.set('status', filters.status)
  if (filters.priority) params.set('priority', filters.priority)
  if (filters.creator) params.set('creator', filters.creator)
  if (filters.updatedSince) params.set('updated_since', filters.updatedSince)
  if (cursor) params.set('cursor', cursor)
  if (limit) params.set('limit', String(limit))
  const query = params.toString()
  return apiFetch<FeaturesPage>(
    `/projects/${encodeURIComponent(projectKey)}/features${query ? `?${query}` : ''}`,
  )
}

export async function getFeature(ref: string, includeDeleted = false): Promise<FeatureDetail> {
  const query = includeDeleted ? '?include_deleted=true' : ''
  return apiFetch<FeatureDetail>(`/features/${encodeURIComponent(ref)}${query}`)
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
 * partial-update semantics) — resend every field. */
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

/** POST /features/{ref}/status — single-field status mutation, its
 * own endpoint (Phase 4 addition) rather than a field on
 * UpdateFeatureInput, mirroring updateTicketStatus's split from
 * updateTicketFields. */
export async function updateFeatureStatus(
  ref: string,
  status: WorkflowStatus,
  expectedVersion: number,
): Promise<FeatureDetail> {
  return apiFetch<FeatureDetail>(`/features/${encodeURIComponent(ref)}/status`, {
    method: 'POST',
    body: { status },
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function reorderFeature(
  ref: string,
  afterRef: string | null,
  expectedVersion: number,
): Promise<FeatureDetail> {
  return apiFetch<FeatureDetail>(`/features/${encodeURIComponent(ref)}/reorder`, {
    method: 'POST',
    body: { after_ref: afterRef },
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function deleteFeature(
  ref: string,
  expectedVersion: number,
  cascade = false,
): Promise<{ version: number }> {
  const query = cascade ? '?cascade=true' : ''
  return apiFetch<{ version: number }>(`/features/${encodeURIComponent(ref)}${query}`, {
    method: 'DELETE',
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function restoreFeature(ref: string, expectedVersion: number): Promise<FeatureDetail> {
  return apiFetch<FeatureDetail>(`/features/${encodeURIComponent(ref)}/restore`, {
    method: 'POST',
    headers: ifMatchHeader(expectedVersion),
  })
}
