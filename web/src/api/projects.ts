import { apiFetch } from './client'
import type { ProjectDetail, ProjectsPage } from './types'

export async function listProjects(cursor?: string): Promise<ProjectsPage> {
  const params = cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''
  return apiFetch<ProjectsPage>(`/projects${params}`)
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
