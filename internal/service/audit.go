package service

import "encoding/json"

// Event type constants for audit_events.event_type (product spec
// §5.12). Not a domain.go-style closed enum yet — Phase 1 only emits
// these three; more are added alongside the operations that need them
// (comments, relationships, positions) rather than pre-declared here.
const (
	eventProjectCreated      = "project_created"
	eventTicketCreated       = "ticket_created"
	eventTicketStatusChanged = "ticket_status_changed"
	eventRelationshipAdded   = "relationship_added"
	eventRelationshipRemoved = "relationship_removed"
	eventFeatureCreated      = "feature_created"
	eventFeatureUpdated      = "feature_updated"
	eventTicketUpdated       = "ticket_updated"
	eventTicketAssigned      = "ticket_assigned"
	eventTicketMoved         = "ticket_moved"
	eventCommentAdded        = "comment_added"
	eventCommentEdited       = "comment_edited"
	eventCommentDeleted      = "comment_deleted"
	eventTicketReordered     = "ticket_reordered"
	eventFeatureReordered    = "feature_reordered"
	eventTicketDeleted       = "ticket_deleted"
	eventTicketRestored      = "ticket_restored"
	eventFeatureDeleted      = "feature_deleted"
	eventFeatureRestored     = "feature_restored"
	eventAssociationAdded    = "association_added"
	eventAssociationRemoved  = "association_removed"
	eventDecisionCreated     = "decision_created"
	eventDecisionUpdated     = "decision_updated"
	eventExternalLinkAdded   = "external_link_added"
	eventExternalLinkRemoved = "external_link_removed"
)

// auditChanges marshals a small before/after or field-summary map into
// the JSON fragment audit_events.changes stores (§5.12: "safe
// before/after values or a structured patch where appropriate"). v is
// always a plain map of strings built by the caller, so marshaling
// cannot realistically fail; a failure falls back to an empty object
// rather than losing the whole audit write over a formatting problem.
func auditChanges(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
