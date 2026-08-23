// Wire types transcribed from the frozen contracts
// (docs/contracts/representations.md, docs/contracts/enums.md) and
// internal/httpapi/wire.go's DTOs — not generated. Every view imports
// its DTOs from here rather than declaring its own shape inline, so a
// future server-side field change has exactly one place to update on
// the client.

// -- Enums (docs/contracts/enums.md — renaming a value is a breaking
// API change, not a refactor) --

export type ProjectStatus = 'active' | 'archived'
export type TicketType = 'task' | 'bug' | 'security' | 'chore'
export type WorkflowStatus =
  | 'backlog'
  | 'ready'
  | 'in_progress'
  | 'blocked'
  | 'review'
  | 'done'
  | 'cancelled'
export type Priority = 'critical' | 'high' | 'medium' | 'low'
export type Severity = 'critical' | 'high' | 'medium' | 'low'
export type DecisionStatus = 'proposed' | 'accepted' | 'rejected' | 'superseded'
export type RelationshipType =
  | 'parent_of'
  | 'child_of'
  | 'blocks'
  | 'blocked_by'
  | 'related_to'
  | 'duplicate_of'
  | 'supersedes'
  | 'superseded_by'

// -- Project --

export interface ProjectCompact {
  key: string
  title: string
  status: ProjectStatus
  version: number
  updated_at: string
}

export interface ProjectDetail {
  key: string
  title: string
  description: string
  status: ProjectStatus
  version: number
  created_at: string
  updated_at: string
}

export interface ProjectsPage {
  projects: ProjectCompact[]
  next_cursor?: string
}

// -- Ticket --
// Note: ticketCompact has no `assignee` field (internal/httpapi/wire.go)
// — list views must not render assignee; it's only available via
// TicketDetail.

export interface TicketCompact {
  ref: string
  title: string
  type: TicketType
  status: WorkflowStatus
  priority: Priority
  severity?: Severity
  updated_at: string
  version: number
}

export interface TicketDetail {
  ref: string
  project: string
  feature: string
  type: TicketType
  title: string
  description: string
  status: WorkflowStatus
  priority: Priority
  severity?: Severity
  assignee?: string
  creator?: string
  version: number
  created_at: string
  updated_at: string
  comments?: CommentDetail[]
  relationships?: RelationshipView[]
}

export interface TicketsPage {
  tickets: TicketCompact[]
  next_cursor?: string
}

// -- Feature --

export interface FeatureCompact {
  ref: string
  title: string
  status: WorkflowStatus
  priority: Priority
  version: number
  updated_at: string
}

export interface FeatureDetail {
  ref: string
  project: string
  title: string
  description: string
  status: WorkflowStatus
  priority: Priority
  version: number
  created_at: string
  updated_at: string
}

export interface FeaturesPage {
  features: FeatureCompact[]
  next_cursor?: string
}

// -- Decision --

export interface DecisionCompact {
  ref: string
  title: string
  status: DecisionStatus
  version: number
  updated_at: string
}

export interface DecisionDetail {
  ref: string
  project: string
  title: string
  context: string
  decision: string
  rationale: string
  status: DecisionStatus
  version: number
  created_at: string
  updated_at: string
}

export interface DecisionsPage {
  decisions: DecisionCompact[]
  next_cursor?: string
}

// -- Comments --
// A comment's `version` is independent of its parent entity's
// (docs/contracts/concurrency.md) — never conflate the two as a
// single If-Match token.

export interface CommentDetail {
  id: number
  author: string
  body: string
  version: number
  created_at: string
  updated_at: string
  deleted_at?: string
}

export interface CommentsPage {
  comments: CommentDetail[]
}

// -- Relationships / associations / links / backlinks --
// (ticket_relationships and entity_associations have no version
// column — add/delete only, per docs/contracts/concurrency.md.)

export interface RelationshipView {
  type: RelationshipType
  other: string
}

export interface RelationshipsPage {
  relationships: RelationshipView[]
}

export interface AssociationsPage {
  associated: string[]
}

/** A link has no version column either (docs/contracts/concurrency.md's
 * Phase 4 addendum) — add/delete only, no in-place edit. */
export interface ExternalLink {
  id: number
  title: string
  url: string
}

export interface LinksPage {
  links: ExternalLink[]
}

/** `comment_id` is present only when the mention came from a comment
 * rather than the source entity's own Markdown body. */
export interface Backlink {
  ref: string
  comment_id?: number
}

export interface BacklinksPage {
  backlinks: Backlink[]
}
