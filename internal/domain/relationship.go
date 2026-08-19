package domain

import "fmt"

// ValidateRelationship checks the parts of a ticket_relationships edge
// (product spec §5.7) that don't require a database lookup: the type
// is recognized, both endpoints are tickets (relationships are
// ticket-only — the looser associated_with link below is what applies
// to decisions/plans/documents/features/tickets), and the edge isn't a
// self-link. sourceUUID/targetUUID are entities.uuid values (ADR
// 0002's canonical identity), not internal surrogate ids — this
// package never deals in those. It does not check for a cycle: that
// requires walking the existing edges in internal/store and is
// service-layer work (ADR 0014's recursive-CTE check), scoped to
// RelationshipBlocks and RelationshipParentOf per §5.7.
func ValidateRelationship(relType RelationshipType, sourceKind, targetKind EntityKind, sourceUUID, targetUUID string) error {
	if !relType.Valid() {
		return fmt.Errorf("domain: invalid relationship type %q", relType)
	}
	if sourceKind != KindTicket || targetKind != KindTicket {
		return fmt.Errorf("domain: relationships are ticket-only, got source kind %q, target kind %q", sourceKind, targetKind)
	}
	if sourceUUID == targetUUID {
		return fmt.Errorf("domain: a ticket cannot have a relationship to itself")
	}
	return nil
}

// CanonicalRelationship maps a caller-supplied (type, source, target)
// triple to the single canonical triple internal/store actually stores
// (ADR 0014): one row per logical edge, never a pair kept in sync.
// child_of/blocked_by/superseded_by are valid input but never a stored
// type — they flip to their partner with endpoints swapped (e.g. "A
// child_of B" stores as parent_of with source=B, target=A).
// related_to is genuinely symmetric and canonicalizes by UUID
// ordering, since there's no inherent direction to prefer between two
// equal partners. duplicate_of has no inverse-named counterpart to
// flip to (RelationshipType's doc explains why) and is always stored
// exactly as given — canonicalizing it by UUID order the way
// related_to is would silently discard which side is the duplicate.
//
// The caller must have already validated the input via
// ValidateRelationship; this function does not repeat those checks,
// and its output is what the recursive-CTE cycle check (ADR 0014)
// walks — a cycle expressed by mixing parent_of and child_of input
// must still be caught, which only works if both get canonicalized to
// the same stored type first.
func CanonicalRelationship(relType RelationshipType, sourceUUID, targetUUID string) (canonType RelationshipType, canonSource, canonTarget string) {
	switch relType {
	case RelationshipChildOf:
		return RelationshipParentOf, targetUUID, sourceUUID
	case RelationshipBlockedBy:
		return RelationshipBlocks, targetUUID, sourceUUID
	case RelationshipSupersededBy:
		return RelationshipSupersedes, targetUUID, sourceUUID
	case RelationshipRelatedTo:
		if sourceUUID > targetUUID {
			return RelationshipRelatedTo, targetUUID, sourceUUID
		}
		return RelationshipRelatedTo, sourceUUID, targetUUID
	default:
		// RelationshipParentOf, RelationshipBlocks,
		// RelationshipSupersedes, RelationshipDuplicateOf: already the
		// canonical stored type, stored exactly as given.
		return relType, sourceUUID, targetUUID
	}
}

// ValidAssociationKind reports whether kind may participate in an
// associated_with link (§5.7): decisions, plans, documents, features,
// and tickets — not projects (an association is project content, not
// a property of the project as a whole) and not comments (comments
// aren't principal entities under ADR 0002's 1:1 entities-registry
// extension pattern).
func ValidAssociationKind(kind EntityKind) bool {
	switch kind {
	case KindTicket, KindFeature, KindDecision, KindPlan, KindDocument:
		return true
	}
	return false
}

// ValidateAssociation is ValidateRelationship's counterpart for the
// single, symmetric associated_with link type.
func ValidateAssociation(sourceKind, targetKind EntityKind, sourceUUID, targetUUID string) error {
	if !ValidAssociationKind(sourceKind) {
		return fmt.Errorf("domain: entity kind %q cannot participate in an association", sourceKind)
	}
	if !ValidAssociationKind(targetKind) {
		return fmt.Errorf("domain: entity kind %q cannot participate in an association", targetKind)
	}
	if sourceUUID == targetUUID {
		return fmt.Errorf("domain: an entity cannot be associated with itself")
	}
	return nil
}
