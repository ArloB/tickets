import { apiFetch } from './client'
import type { ProjectBrief } from './types'

export async function getProjectBrief(key: string): Promise<ProjectBrief> {
  return apiFetch<ProjectBrief>(`/projects/${encodeURIComponent(key)}/brief`)
}
