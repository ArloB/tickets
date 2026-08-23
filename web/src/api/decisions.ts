import { apiFetch, ifMatchHeader } from './client'
import type { DecisionDetail, DecisionsPage, DecisionStatus } from './types'

export async function listDecisions(projectKey: string, cursor?: string): Promise<DecisionsPage> {
  const params = cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''
  return apiFetch<DecisionsPage>(`/projects/${encodeURIComponent(projectKey)}/decisions${params}`)
}

export async function getDecision(ref: string): Promise<DecisionDetail> {
  return apiFetch<DecisionDetail>(`/decisions/${encodeURIComponent(ref)}`)
}

export interface CreateDecisionInput {
  title: string
  context: string
  decision: string
  rationale: string
}

export async function createDecision(
  projectKey: string,
  input: CreateDecisionInput,
): Promise<DecisionDetail> {
  return apiFetch<DecisionDetail>(`/projects/${encodeURIComponent(projectKey)}/decisions`, {
    method: 'POST',
    body: input,
  })
}

export interface UpdateDecisionInput {
  title: string
  context: string
  decision: string
  rationale: string
  status: DecisionStatus
}

/** PATCH /decisions/{ref} is a full-representation update (like
 * ticket PUT, despite the HTTP verb) — every field, not just what
 * changed. Requires If-Match; a 409 carries the live record's
 * current_version (docs/contracts/errors.md). */
export async function updateDecision(
  ref: string,
  input: UpdateDecisionInput,
  expectedVersion: number,
): Promise<DecisionDetail> {
  return apiFetch<DecisionDetail>(`/decisions/${encodeURIComponent(ref)}`, {
    method: 'PATCH',
    body: input,
    headers: ifMatchHeader(expectedVersion),
  })
}
