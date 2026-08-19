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
// RelationshipRelatedTo and RelationshipDuplicateOf, which are their
// own inverse (docs/contracts/enums.md). Inverse() gives the value
// mapping; full cycle detection (product spec §5.7) is graph-traversal
// behavior implemented in Phase 1, not here.
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

func (r RelationshipType) Valid() bool {
	_, ok := relationshipInverse[r]
	return ok
}

var relationshipInverse = map[RelationshipType]RelationshipType{
	RelationshipParentOf:     RelationshipChildOf,
	RelationshipChildOf:      RelationshipParentOf,
	RelationshipBlocks:       RelationshipBlockedBy,
	RelationshipBlockedBy:    RelationshipBlocks,
	RelationshipRelatedTo:    RelationshipRelatedTo,
	RelationshipDuplicateOf:  RelationshipDuplicateOf,
	RelationshipSupersedes:   RelationshipSupersededBy,
	RelationshipSupersededBy: RelationshipSupersedes,
}

// Inverse returns the relationship type seen from the other end of the
// edge, and whether r is a recognized relationship type at all.
func (r RelationshipType) Inverse() (RelationshipType, bool) {
	inv, ok := relationshipInverse[r]
	return inv, ok
}

// AssociationType is the single looser, non-directional link kind used
// where a dependency relationship would not make sense (product spec
// §5.7): decisions, plans, documents, features, and tickets.
type AssociationType string

const AssociationAssociatedWith AssociationType = "associated_with"

func (a AssociationType) Valid() bool {
	return a == AssociationAssociatedWith
}
