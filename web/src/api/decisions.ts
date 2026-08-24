import { apiFetch, ifMatchHeader } from './client'
import type {
  DecisionDetail,
  DecisionDiff,
  DecisionsPage,
  DecisionStatus,
  DecisionVersionsPage,
} from './types'

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
  consequences: string
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
  consequences: string
  status: DecisionStatus
  superseded_by: string
}

/** PATCH /decisions/{ref} is a full-representation update (like
 * ticket PUT, despite the HTTP verb) — every field, not just what
 * changed. Requires If-Match; a 409 carries the live record's
 * current_version (docs/contracts/errors.md). superseded_by "" clears
 * an existing supersession link. */
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

/** GET /decisions/{ref}/versions — archived prior states, oldest
 * first. Does not include the live state. */
export async function listDecisionVersions(ref: string): Promise<DecisionVersionsPage> {
  return apiFetch<DecisionVersionsPage>(`/decisions/${encodeURIComponent(ref)}/versions`)
}

/** GET /decisions/{ref}/diff?from=&to= — a per-field line diff between
 * two named versions, either of which may be the live version. */
export async function getDecisionDiff(
  ref: string,
  from: number,
  to: number,
): Promise<DecisionDiff> {
  return apiFetch<DecisionDiff>(
    `/decisions/${encodeURIComponent(ref)}/diff?from=${from}&to=${to}`,
  )
}
