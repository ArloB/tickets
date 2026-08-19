package domain

import "testing"

func TestValidateRelationship(t *testing.T) {
	if err := ValidateRelationship(RelationshipBlocks, KindTicket, KindTicket, "uuid-a", "uuid-b"); err != nil {
		t.Errorf("valid ticket-ticket relationship rejected: %v", err)
	}
}

func TestValidateRelationshipRejectsInvalidType(t *testing.T) {
	if err := ValidateRelationship(RelationshipType("bogus"), KindTicket, KindTicket, "uuid-a", "uuid-b"); err == nil {
		t.Error("expected error for unrecognized relationship type")
	}
}

func TestValidateRelationshipRejectsNonTicketEndpoints(t *testing.T) {
	cases := []struct {
		name               string
		sourceKind, target EntityKind
	}{
		{"source is a feature", KindFeature, KindTicket},
		{"target is a decision", KindTicket, KindDecision},
		{"both non-ticket", KindFeature, KindPlan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRelationship(RelationshipBlocks, tc.sourceKind, tc.target, "uuid-a", "uuid-b"); err == nil {
				t.Error("expected error for non-ticket endpoint")
			}
		})
	}
}

func TestValidateRelationshipRejectsSelfLink(t *testing.T) {
	if err := ValidateRelationship(RelationshipBlocks, KindTicket, KindTicket, "uuid-same", "uuid-same"); err == nil {
		t.Error("expected error for self-link")
	}
}

func TestCanonicalRelationship(t *testing.T) {
	cases := []struct {
		name             string
		relType          RelationshipType
		source, target   string
		wantType         RelationshipType
		wantSrc, wantTgt string
	}{
		{"parent_of passes through", RelationshipParentOf, "A", "B", RelationshipParentOf, "A", "B"},
		{"child_of flips to parent_of", RelationshipChildOf, "A", "B", RelationshipParentOf, "B", "A"},
		{"blocks passes through", RelationshipBlocks, "A", "B", RelationshipBlocks, "A", "B"},
		{"blocked_by flips to blocks", RelationshipBlockedBy, "A", "B", RelationshipBlocks, "B", "A"},
		{"supersedes passes through", RelationshipSupersedes, "A", "B", RelationshipSupersedes, "A", "B"},
		{"superseded_by flips to supersedes", RelationshipSupersededBy, "A", "B", RelationshipSupersedes, "B", "A"},
		{"duplicate_of always passes through as given", RelationshipDuplicateOf, "A", "B", RelationshipDuplicateOf, "A", "B"},
		{"duplicate_of passes through even when source > target", RelationshipDuplicateOf, "B", "A", RelationshipDuplicateOf, "B", "A"},
		{"related_to orders ascending (already ascending)", RelationshipRelatedTo, "A", "B", RelationshipRelatedTo, "A", "B"},
		{"related_to orders ascending (was descending)", RelationshipRelatedTo, "B", "A", RelationshipRelatedTo, "A", "B"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotSrc, gotTgt := CanonicalRelationship(tc.relType, tc.source, tc.target)
			if gotType != tc.wantType || gotSrc != tc.wantSrc || gotTgt != tc.wantTgt {
				t.Errorf("CanonicalRelationship(%s, %q, %q) = (%s, %q, %q), want (%s, %q, %q)",
					tc.relType, tc.source, tc.target, gotType, gotSrc, gotTgt, tc.wantType, tc.wantSrc, tc.wantTgt)
			}
		})
	}
}

// TestCanonicalRelationshipInverseInputsAgree is the property
// CanonicalRelationship exists for: describing the same real-world
// edge from either end (parent_of from the parent, or child_of from
// the child) must produce the identical stored triple, or a cycle
// expressed by mixing the two input directions would slip past a
// cycle check that only walks one stored type.
func TestCanonicalRelationshipInverseInputsAgree(t *testing.T) {
	pairs := []struct {
		fwd, inv RelationshipType
	}{
		{RelationshipParentOf, RelationshipChildOf},
		{RelationshipBlocks, RelationshipBlockedBy},
		{RelationshipSupersedes, RelationshipSupersededBy},
	}
	for _, p := range pairs {
		fwdType, fwdSrc, fwdTgt := CanonicalRelationship(p.fwd, "A", "B")
		invType, invSrc, invTgt := CanonicalRelationship(p.inv, "B", "A")
		if fwdType != invType || fwdSrc != invSrc || fwdTgt != invTgt {
			t.Errorf("%s(A,B) canonicalized to (%s,%q,%q) but %s(B,A) canonicalized to (%s,%q,%q); want identical",
				p.fwd, fwdType, fwdSrc, fwdTgt, p.inv, invType, invSrc, invTgt)
		}
	}
}

func TestValidAssociationKind(t *testing.T) {
	for _, k := range []EntityKind{KindTicket, KindFeature, KindDecision, KindPlan, KindDocument} {
		if !ValidAssociationKind(k) {
			t.Errorf("ValidAssociationKind(%s) = false, want true", k)
		}
	}
	for _, k := range []EntityKind{KindProject, EntityKind("comment")} {
		if ValidAssociationKind(k) {
			t.Errorf("ValidAssociationKind(%s) = true, want false", k)
		}
	}
}

func TestValidateAssociation(t *testing.T) {
	if err := ValidateAssociation(KindTicket, KindDecision, "uuid-a", "uuid-b"); err != nil {
		t.Errorf("valid association rejected: %v", err)
	}
	if err := ValidateAssociation(KindProject, KindTicket, "uuid-a", "uuid-b"); err == nil {
		t.Error("expected error: project cannot participate in an association")
	}
	if err := ValidateAssociation(KindTicket, KindTicket, "uuid-same", "uuid-same"); err == nil {
		t.Error("expected error for self-association")
	}
}
