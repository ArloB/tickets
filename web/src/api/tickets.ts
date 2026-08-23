import { apiFetch } from './client'
import type {
  BacklinksPage,
  CommentsPage,
  LinksPage,
  Priority,
  RelationshipsPage,
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

export async function listTicketComments(ref: string): Promise<CommentsPage> {
  return apiFetch<CommentsPage>(`/tickets/${encodeURIComponent(ref)}/comments`)
}

export async function listTicketRelationships(ref: string): Promise<RelationshipsPage> {
  return apiFetch<RelationshipsPage>(`/tickets/${encodeURIComponent(ref)}/relationships`)
}

export async function listTicketLinks(ref: string): Promise<LinksPage> {
  return apiFetch<LinksPage>(`/tickets/${encodeURIComponent(ref)}/links`)
}

export async function listTicketBacklinks(ref: string): Promise<BacklinksPage> {
  return apiFetch<BacklinksPage>(`/tickets/${encodeURIComponent(ref)}/backlinks`)
}
