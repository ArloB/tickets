package domain

// Enum wire values. See docs/contracts/enums.md — these are the frozen
// strings reused by the JSON API, CLI flags, MCP tool schemas, and the
// web UI. Renaming a value here is a breaking API change.

type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusArchived ProjectStatus = "archived"
)

func (s ProjectStatus) Valid() bool {
	switch s {
	case ProjectStatusActive, ProjectStatusArchived:
		return true
	}
	return false
}

// ContentItemStatus mirrors ProjectStatus exactly (ADR 0028) — same
// values, same "visibility only, not a workflow" semantics, applied to
// a plan or document instead of a project.
type ContentItemStatus string

const (
	ContentItemStatusActive   ContentItemStatus = "active"
	ContentItemStatusArchived ContentItemStatus = "archived"
)

func (s ContentItemStatus) Valid() bool {
	switch s {
	case ContentItemStatusActive, ContentItemStatusArchived:
		return true
	}
	return false
}

type TicketType string

const (
	TicketTypeTask     TicketType = "task"
	TicketTypeBug      TicketType = "bug"
	TicketTypeSecurity TicketType = "security"
	TicketTypeChore    TicketType = "chore"
)

func (t TicketType) Valid() bool {
	switch t {
	case TicketTypeTask, TicketTypeBug, TicketTypeSecurity, TicketTypeChore:
		return true
	}
	return false
}

// AllowsSeverity reports whether this ticket type may carry a Severity
// (docs/contracts/enums.md: only bug and security tickets).
func (t TicketType) AllowsSeverity() bool {
	return t == TicketTypeBug || t == TicketTypeSecurity
}

// WorkflowStatus is shared by tickets and features (product spec §5.4:
// features use the same initial workflow as tickets). One type, one
// enum, no parallel copies.
type WorkflowStatus string

const (
	WorkflowStatusBacklog    WorkflowStatus = "backlog"
	WorkflowStatusReady      WorkflowStatus = "ready"
	WorkflowStatusInProgress WorkflowStatus = "in_progress"
	WorkflowStatusBlocked    WorkflowStatus = "blocked"
	WorkflowStatusReview     WorkflowStatus = "review"
	WorkflowStatusDone       WorkflowStatus = "done"
	WorkflowStatusCancelled  WorkflowStatus = "cancelled"
)

func (s WorkflowStatus) Valid() bool {
	switch s {
	case WorkflowStatusBacklog, WorkflowStatusReady, WorkflowStatusInProgress,
		WorkflowStatusBlocked, WorkflowStatusReview, WorkflowStatusDone, WorkflowStatusCancelled:
		return true
	}
	return false
}

type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow:
		return true
	}
	return false
}

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow:
		return true
	}
	return false
}

type DecisionStatus string

const (
	DecisionStatusProposed   DecisionStatus = "proposed"
	DecisionStatusAccepted   DecisionStatus = "accepted"
	DecisionStatusRejected   DecisionStatus = "rejected"
	DecisionStatusSuperseded DecisionStatus = "superseded"
)

func (s DecisionStatus) Valid() bool {
	switch s {
	case DecisionStatusProposed, DecisionStatusAccepted, DecisionStatusRejected, DecisionStatusSuperseded:
		return true
	}
	return false
}

// RelationshipType values are stored as directed pairs except
// RelationshipRelatedTo, which is genuinely symmetric — its own
// inverse. RelationshipDuplicateOf is directional like parent_of/blocks
// ("A duplicate_of B" means B is canonical, not the reverse) but §5.7
// defines no "duplicated_by" counterpart the way it does for parent_of
// and blocks, so it has no Inverse() at all: a stored edge is only ever
// read from its source end. (An earlier version of
// docs/contracts/enums.md called duplicate_of "its own inverse" — that
// was wrong; treating it that way would let "A duplicate_of B" and "B
// duplicate_of A" both be true, which is a contradiction, not a
// relationship.) Full cycle detection (product spec §5.7) for
// parent_of/blocks is graph-traversal behavior implemented in Phase 1's
// internal/service, not here.
type RelationshipType string

const (
	RelationshipParentOf     RelationshipType = "parent_of"
	RelationshipChildOf      RelationshipType = "child_of"
	RelationshipBlocks       RelationshipType = "blocks"
	RelationshipBlockedBy    RelationshipType = "blocked_by"
	RelationshipRelatedTo    RelationshipType = "related_to"
	RelationshipDuplicateOf  RelationshipType = "duplicate_of"
	RelationshipSupersedes   RelationshipType = "supersedes"
	RelationshipSupersededBy RelationshipType = "superseded_by"
)

var validRelationshipTypes = map[RelationshipType]bool{
	RelationshipParentOf:     true,
	RelationshipChildOf:      true,
	RelationshipBlocks:       true,
	RelationshipBlockedBy:    true,
	RelationshipRelatedTo:    true,
	RelationshipDuplicateOf:  true,
	RelationshipSupersedes:   true,
	RelationshipSupersededBy: true,
}

func (r RelationshipType) Valid() bool {
	return validRelationshipTypes[r]
}

var relationshipInverse = map[RelationshipType]RelationshipType{
	RelationshipParentOf:     RelationshipChildOf,
	RelationshipChildOf:      RelationshipParentOf,
	RelationshipBlocks:       RelationshipBlockedBy,
	RelationshipBlockedBy:    RelationshipBlocks,
	RelationshipRelatedTo:    RelationshipRelatedTo,
	RelationshipSupersedes:   RelationshipSupersededBy,
	RelationshipSupersededBy: RelationshipSupersedes,
	// RelationshipDuplicateOf intentionally absent — see the type doc.
}

// Inverse returns the relationship type seen from the other end of the
// edge, and whether r has one. A false result means either r is not a
// recognized relationship type at all, or it is recognized but
// directional-only (RelationshipDuplicateOf) — callers that need to
// distinguish those two cases use Valid() first.
func (r RelationshipType) Inverse() (RelationshipType, bool) {
	inv, ok := relationshipInverse[r]
	return inv, ok
}

// AssociationType is the single looser, non-directional link kind used
// where a dependency relationship would not make sense (product spec
// §5.7): decisions, plans, documents, features, and tickets.
type AssociationType string

const AssociationAssociatedWith AssociationType = "associated_with"

// AttachmentKind distinguishes an uploaded blob from a path reference
// (§5.11). There is deliberately no `url` kind here — external_links
// stays the one mechanism for URL-shaped links (Phase 5 plan's
// confirmed decision).
type AttachmentKind string

const (
	AttachmentKindUpload AttachmentKind = "upload"
	AttachmentKindPath   AttachmentKind = "path"
)

func (k AttachmentKind) Valid() bool {
	switch k {
	case AttachmentKindUpload, AttachmentKindPath:
		return true
	}
	return false
}

// ContentRepresentation is how a content_items row (a plan or
// document, §5.9) stores its content — immutable after creation
// (Phase 5 plan's confirmed decision): switching representations
// means creating a new item, not converting an existing one.
type ContentRepresentation string

const (
	ContentRepresentationMarkdown ContentRepresentation = "markdown"
	ContentRepresentationFile     ContentRepresentation = "file"
	ContentRepresentationPath     ContentRepresentation = "path"
	ContentRepresentationURL      ContentRepresentation = "url"
)

func (r ContentRepresentation) Valid() bool {
	switch r {
	case ContentRepresentationMarkdown, ContentRepresentationFile, ContentRepresentationPath, ContentRepresentationURL:
		return true
	}
	return false
}

func (a AssociationType) Valid() bool {
	return a == AssociationAssociatedWith
}
