package domain

import "testing"

func TestTicketTypeAllowsSeverity(t *testing.T) {
	cases := map[TicketType]bool{
		TicketTypeTask:     false,
		TicketTypeBug:      true,
		TicketTypeSecurity: true,
		TicketTypeChore:    false,
	}
	for tt, want := range cases {
		if got := tt.AllowsSeverity(); got != want {
			t.Errorf("%s.AllowsSeverity() = %v, want %v", tt, got, want)
		}
	}
}

func TestWorkflowStatusValid(t *testing.T) {
	valid := []WorkflowStatus{
		WorkflowStatusBacklog, WorkflowStatusReady, WorkflowStatusInProgress,
		WorkflowStatusBlocked, WorkflowStatusReview, WorkflowStatusDone, WorkflowStatusCancelled,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("%s.Valid() = false, want true", s)
		}
	}
	if WorkflowStatus("bogus").Valid() {
		t.Error(`WorkflowStatus("bogus").Valid() = true, want false`)
	}
}

func TestRelationshipInverse(t *testing.T) {
	cases := []struct {
		r    RelationshipType
		want RelationshipType
	}{
		{RelationshipParentOf, RelationshipChildOf},
		{RelationshipChildOf, RelationshipParentOf},
		{RelationshipBlocks, RelationshipBlockedBy},
		{RelationshipBlockedBy, RelationshipBlocks},
		{RelationshipSupersedes, RelationshipSupersededBy},
		{RelationshipSupersededBy, RelationshipSupersedes},
		{RelationshipRelatedTo, RelationshipRelatedTo}, // self-inverse
	}
	for _, c := range cases {
		got, ok := c.r.Inverse()
		if !ok {
			t.Errorf("%s.Inverse() ok = false, want true", c.r)
			continue
		}
		if got != c.want {
			t.Errorf("%s.Inverse() = %s, want %s", c.r, got, c.want)
		}
	}

	// Inverse of an inverse must round-trip for every known type.
	for r := range relationshipInverse {
		inv, _ := r.Inverse()
		back, ok := inv.Inverse()
		if !ok || back != r {
			t.Errorf("inverse round trip failed for %s: -> %s -> %s", r, inv, back)
		}
	}

	if _, ok := RelationshipType("bogus").Inverse(); ok {
		t.Error(`RelationshipType("bogus").Inverse() ok = true, want false`)
	}
}

// TestDuplicateOfHasNoInverse is the regression for the enums.md
// correction: duplicate_of is directional ("A duplicate_of B" means B
// is canonical), and §5.7 defines no "duplicated_by" counterpart the
// way it does for parent_of/blocks, so it must be a recognized
// relationship type with no Inverse() at all — not a spurious
// self-inverse, which would let A-duplicates-B and B-duplicates-A both
// be true simultaneously.
func TestDuplicateOfHasNoInverse(t *testing.T) {
	if !RelationshipDuplicateOf.Valid() {
		t.Error("RelationshipDuplicateOf.Valid() = false, want true")
	}
	if _, ok := RelationshipDuplicateOf.Inverse(); ok {
		t.Error("RelationshipDuplicateOf.Inverse() ok = true, want false (directional, no defined inverse)")
	}
}

func TestSeverityValid(t *testing.T) {
	if !SeverityCritical.Valid() {
		t.Error("SeverityCritical.Valid() = false, want true")
	}
	if Severity("bogus").Valid() {
		t.Error(`Severity("bogus").Valid() = true, want false`)
	}
}
