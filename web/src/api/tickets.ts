import { apiFetch, ifMatchHeader } from './client'
import type {
  Priority,
  Severity,
  TicketDetail,
  TicketsPage,
  TicketType,
  WorkflowStatus,
} from './types'

/** `view` selects the base ordering (§5.5/§5.6) — Milestone 2's
 * backlog view reuses priority_queue's order with filters layered on
 * (plan.md's Phase 4 plan, §4's ticket-list note). */
export type TicketListView = 'priority_queue' | 'issue_register'

export interface TicketListFilters {
  status?: WorkflowStatus
  type?: TicketType
  severity?: Severity
  priority?: Priority
  featureRef?: string
  assignee?: string
  creator?: string
  updatedSince?: string
}

export async function listTickets(
  projectKey: string,
  view: TicketListView,
  filters: TicketListFilters = {},
  cursor?: string,
  limit?: number,
): Promise<TicketsPage> {
  const params = new URLSearchParams({ view })
  if (filters.status) params.set('status', filters.status)
  if (filters.type) params.set('type', filters.type)
  if (filters.severity) params.set('severity', filters.severity)
  if (filters.priority) params.set('priority', filters.priority)
  if (filters.featureRef) params.set('feature_ref', filters.featureRef)
  if (filters.assignee) params.set('assignee', filters.assignee)
  if (filters.creator) params.set('creator', filters.creator)
  if (filters.updatedSince) params.set('updated_since', filters.updatedSince)
  if (cursor) params.set('cursor', cursor)
  if (limit) params.set('limit', String(limit))
  return apiFetch<TicketsPage>(`/projects/${encodeURIComponent(projectKey)}/tickets?${params}`)
}

/** `include` adds `comments`/`relationships` arrays to the detail
 * response — both keys are absent entirely unless requested
 * (docs/contracts/representations.md). */
export async function getTicket(
  ref: string,
  include: Array<'comments' | 'relationships'> = [],
  includeDeleted = false,
): Promise<TicketDetail> {
  const params = new URLSearchParams()
  if (include.length) params.set('include', include.join(','))
  if (includeDeleted) params.set('include_deleted', 'true')
  const query = params.toString()
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}${query ? `?${query}` : ''}`)
}

/** feature is the destination feature ref (e.g. "ABC-F2") — required by
 * the server, which no longer defaults to General (ADR 0023). The web
 * UI always sends a concrete ref rather than the server's `general`
 * shorthand, since it already has the full feature list to pick from. */
export interface CreateTicketInput {
  type: TicketType
  title: string
  description: string
  priority: Priority
  severity?: Severity | null
  feature: string
}

export async function createTicket(
  projectKey: string,
  input: CreateTicketInput,
): Promise<TicketDetail> {
  return apiFetch<TicketDetail>(`/projects/${encodeURIComponent(projectKey)}/tickets`, {
    method: 'POST',
    body: input,
  })
}

export interface UpdateTicketFieldsInput {
  type: TicketType
  title: string
  description: string
  priority: Priority
  severity?: Severity | null
}

/** PUT /tickets/{ref} — full-representation update of every mutable
 * field except status; omitting a field clobbers it
 * (internal/httpapi/tickets.go's updateTicketFields doc comment). The
 * caller must resend every field from its base snapshot, not just the
 * ones the user touched. */
export async function updateTicketFields(
  ref: string,
  input: UpdateTicketFieldsInput,
  expectedVersion: number,
): Promise<TicketDetail> {
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}`, {
    method: 'PUT',
    body: input,
    headers: ifMatchHeader(expectedVersion),
  })
}

/** PATCH /tickets/{ref} — status only, a separate flow from PUT above
 * with its own If-Match/version-conflict handling. */
export async function updateTicketStatus(
  ref: string,
  status: WorkflowStatus,
  expectedVersion: number,
): Promise<TicketDetail> {
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}`, {
    method: 'PATCH',
    body: { status },
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function assignTicket(
  ref: string,
  assignee: string | null,
  expectedVersion: number,
): Promise<TicketDetail> {
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}/assign`, {
    method: 'POST',
    body: { assignee },
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function moveTicketFeature(
  ref: string,
  featureRef: string,
  expectedVersion: number,
): Promise<TicketDetail> {
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}/move`, {
    method: 'POST',
    body: { feature: featureRef },
    headers: ifMatchHeader(expectedVersion),
  })
}

/** POST /tickets/{ref}/reorder — move within a priority group.
 * `afterRef` must name a ticket in the *same* priority band
 * (api/openapi.yaml's ReorderRequest doc comment) — null/undefined
 * moves to the head of the group. A group renumber (when the gap
 * between neighbors is exhausted) bumps no sibling versions and emits
 * no audit event (ADR 0011), so callers never need to invalidate
 * other cached rows' versions after a successful reorder — only the
 * moved ticket's own response matters. */
export async function reorderTicket(
  ref: string,
  afterRef: string | null,
  expectedVersion: number,
): Promise<TicketDetail> {
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}/reorder`, {
    method: 'POST',
    body: { after_ref: afterRef },
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function deleteTicket(ref: string, expectedVersion: number): Promise<{ version: number }> {
  return apiFetch<{ version: number }>(`/tickets/${encodeURIComponent(ref)}`, {
    method: 'DELETE',
    headers: ifMatchHeader(expectedVersion),
  })
}

export async function restoreTicket(ref: string, expectedVersion: number): Promise<TicketDetail> {
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}/restore`, {
    method: 'POST',
    headers: ifMatchHeader(expectedVersion),
  })
}
