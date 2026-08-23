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
  return apiFetch<TicketsPage>(`/projects/${encodeURIComponent(projectKey)}/tickets?${params}`)
}

/** `include` adds `comments`/`relationships` arrays to the detail
 * response — both keys are absent entirely unless requested
 * (docs/contracts/representations.md). */
export async function getTicket(
  ref: string,
  include: Array<'comments' | 'relationships'> = [],
): Promise<TicketDetail> {
  const params = include.length ? `?include=${include.join(',')}` : ''
  return apiFetch<TicketDetail>(`/tickets/${encodeURIComponent(ref)}${params}`)
}

export interface CreateTicketInput {
  type: TicketType
  title: string
  description: string
  priority: Priority
  severity?: Severity | null
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
