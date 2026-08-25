import { apiFetch, ifMatchHeader } from './client'
import type { ProjectDetail, ProjectsPage, ProjectStatus } from './types'

export async function listProjects(
  cursor?: string,
  includeArchived = false,
): Promise<ProjectsPage> {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  if (includeArchived) params.set('include_archived', 'true')
  const query = params.toString()
  return apiFetch<ProjectsPage>(`/projects${query ? `?${query}` : ''}`)
}

export async function getProject(key: string): Promise<ProjectDetail> {
  return apiFetch<ProjectDetail>(`/projects/${encodeURIComponent(key)}`)
}

export interface CreateProjectInput {
  key: string
  title: string
  description: string
}

export async function createProject(input: CreateProjectInput): Promise<ProjectDetail> {
  return apiFetch<ProjectDetail>('/projects', { method: 'POST', body: input })
}

export interface UpdateProjectInput {
  title: string
  description: string
}

/** PATCH /projects/{key} — title/description only; see
 * updateProjectStatus for archive/unarchive (ADR 0021). Same
 * full-representation-update contract as updateFeature. */
export async function updateProject(
  key: string,
  input: UpdateProjectInput,
  expectedVersion: number,
): Promise<ProjectDetail> {
  return apiFetch<ProjectDetail>(`/projects/${encodeURIComponent(key)}`, {
    method: 'PATCH',
    body: input,
    headers: ifMatchHeader(expectedVersion),
  })
}

/** POST /projects/{key}/status — archive or unarchive. Archiving is
 * visibility only: the project drops out of the default project list
 * and search results, but its tickets/features/knowledge records stay
 * fully readable and writable (ADR 0021). */
export async function updateProjectStatus(
  key: string,
  status: ProjectStatus,
  expectedVersion: number,
): Promise<ProjectDetail> {
  return apiFetch<ProjectDetail>(`/projects/${encodeURIComponent(key)}/status`, {
    method: 'POST',
    body: { status },
    headers: ifMatchHeader(expectedVersion),
  })
}
