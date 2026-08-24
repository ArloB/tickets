package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// TestUpdateDecisionArchivesPriorVersion mirrors
// TestEditCommentArchivesPriorBody: every edit snapshots the pre-update
// state into decision_versions (§5.8: "every version remains
// visible"), and the archived entry's own version number is the one
// *before* the edit, not the one it produced.
func TestUpdateDecisionArchivesPriorVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	d, err := s.CreateDecision(ctx, CreateDecisionRequest{
		ProjectKey: "ABC", Title: "Use SQLite", Context: "v1 context", Consequences: "v1 consequences",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	ref, _ := domain.Parse(d.Ref)

	if _, err := s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: ref, Title: "Use SQLite (v2)", Context: "v2 context", Consequences: "v2 consequences",
		Status: domain.DecisionStatusAccepted, ExpectedVersion: d.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}

	versions, err := s.ListDecisionVersions(ctx, ref)
	if err != nil {
		t.Fatalf("ListDecisionVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %+v, want exactly 1 archived version", versions)
	}
	if versions[0].Version != d.Version {
		t.Errorf("archived version number = %d, want %d (the pre-edit version)", versions[0].Version, d.Version)
	}
	if versions[0].Title != "Use SQLite" || versions[0].Context != "v1 context" || versions[0].Consequences != "v1 consequences" {
		t.Errorf("archived version = %+v, want the pre-edit field values", versions[0])
	}
	if versions[0].Status != domain.DecisionStatusProposed {
		t.Errorf("archived version status = %q, want proposed (the pre-edit status)", versions[0].Status)
	}
	if versions[0].EditedBy != testActor {
		t.Errorf("archived version EditedBy = %v, want %v", versions[0].EditedBy, testActor)
	}
}

// TestUpdateDecisionMultipleVersionsAccumulate proves a second edit
// produces a second archived entry without disturbing the first.
func TestUpdateDecisionMultipleVersionsAccumulate(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	d, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "v1"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	ref, _ := domain.Parse(d.Ref)

	v2, err := s.UpdateDecision(ctx, UpdateDecisionRequest{Ref: ref, Title: "v2", Status: domain.DecisionStatusProposed, ExpectedVersion: d.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("update to v2: %v", err)
	}
	if _, err := s.UpdateDecision(ctx, UpdateDecisionRequest{Ref: ref, Title: "v3", Status: domain.DecisionStatusProposed, ExpectedVersion: v2.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("update to v3: %v", err)
	}

	versions, err := s.ListDecisionVersions(ctx, ref)
	if err != nil {
		t.Fatalf("ListDecisionVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %+v, want 2 archived entries", versions)
	}
	if versions[0].Title != "v1" || versions[1].Title != "v2" {
		t.Errorf("versions = [%q, %q], want [v1, v2] oldest first", versions[0].Title, versions[1].Title)
	}
}

// TestDecisionConsequencesFieldRoundTrips proves the field §5.8 always
// named but Phase 3 never stored is now wired all the way through
// create/get/update.
func TestDecisionConsequencesFieldRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	d, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "T", Consequences: "Things will happen"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	if d.Consequences != "Things will happen" {
		t.Errorf("Consequences = %q, want %q", d.Consequences, "Things will happen")
	}

	fetched, err := s.GetDecision(ctx, mustParseRef(t, d.Ref))
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if fetched.Consequences != "Things will happen" {
		t.Errorf("fetched Consequences = %q, want %q", fetched.Consequences, "Things will happen")
	}
}

// TestUpdateDecisionSupersession proves setting superseded_by links the
// old decision to the new one, that it's rejected for a wrong-project
// or self reference, and that it round-trips through GetDecision.
func TestUpdateDecisionSupersession(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	old, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "Old decision"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create old: %v", err)
	}
	replacement, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "New decision"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create replacement: %v", err)
	}
	oldRef, _ := domain.Parse(old.Ref)

	// Self-reference is rejected.
	_, err = s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: oldRef, Title: "Old decision", Status: domain.DecisionStatusSuperseded, SupersededBy: old.Ref, ExpectedVersion: old.Version,
	}, testActor, testCorrelationID)
	if !isValidationError(err, "superseded_by") {
		t.Errorf("self-superseding = %v, want validation_failed on superseded_by", err)
	}

	// Cross-project reference is rejected.
	mustCreateProject(t, s, "XYZ")
	other, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "XYZ", Title: "Other project decision"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create decision in other project: %v", err)
	}
	_, err = s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: oldRef, Title: "Old decision", Status: domain.DecisionStatusSuperseded, SupersededBy: other.Ref, ExpectedVersion: old.Version,
	}, testActor, testCorrelationID)
	if !isValidationError(err, "superseded_by") {
		t.Errorf("cross-project superseded_by = %v, want validation_failed on superseded_by", err)
	}

	// A real, valid supersession link round-trips.
	updated, err := s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: oldRef, Title: "Old decision", Status: domain.DecisionStatusSuperseded, SupersededBy: replacement.Ref, ExpectedVersion: old.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("valid supersession: %v", err)
	}
	if updated.SupersededBy == nil || *updated.SupersededBy != replacement.Ref {
		t.Errorf("updated.SupersededBy = %v, want %q", updated.SupersededBy, replacement.Ref)
	}

	fetched, err := s.GetDecision(ctx, oldRef)
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if fetched.SupersededBy == nil || *fetched.SupersededBy != replacement.Ref {
		t.Errorf("fetched.SupersededBy = %v, want %q", fetched.SupersededBy, replacement.Ref)
	}
}

// TestGetDecisionDiffAcrossVersions proves the diff endpoint compares
// two named versions — including the live version — and reflects real
// field-level changes.
func TestGetDecisionDiffAcrossVersions(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	d, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "T", Context: "line one\nline two"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	ref, _ := domain.Parse(d.Ref)

	updated, err := s.UpdateDecision(ctx, UpdateDecisionRequest{
		Ref: ref, Title: "T", Context: "line one\nline three", Status: domain.DecisionStatusAccepted, ExpectedVersion: d.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}

	diff, err := s.GetDecisionDiff(ctx, ref, d.Version, updated.Version)
	if err != nil {
		t.Fatalf("GetDecisionDiff: %v", err)
	}
	if diff.StatusFrom != domain.DecisionStatusProposed || diff.StatusTo != domain.DecisionStatusAccepted {
		t.Errorf("diff status = %q -> %q, want proposed -> accepted", diff.StatusFrom, diff.StatusTo)
	}
	wantContext := []domain.DiffLine{
		{Op: domain.DiffEqual, Text: "line one"},
		{Op: domain.DiffRemove, Text: "line two"},
		{Op: domain.DiffAdd, Text: "line three"},
	}
	if len(diff.Fields.Context) != len(wantContext) {
		t.Fatalf("diff.Fields.Context = %+v, want %+v", diff.Fields.Context, wantContext)
	}
	for i, line := range wantContext {
		if diff.Fields.Context[i] != line {
			t.Errorf("diff.Fields.Context[%d] = %+v, want %+v", i, diff.Fields.Context[i], line)
		}
	}
}

// TestGetDecisionDiffRejectsUnknownVersion proves an out-of-range
// version number is a clean validation error, not a panic or an
// internal_error.
func TestGetDecisionDiffRejectsUnknownVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	d, err := s.CreateDecision(ctx, CreateDecisionRequest{ProjectKey: "ABC", Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	ref, _ := domain.Parse(d.Ref)

	_, err = s.GetDecisionDiff(ctx, ref, 1, 99)
	if !isValidationError(err, "version") {
		t.Errorf("diff against version 99 = %v, want validation_failed on field version", err)
	}
}

func mustParseRef(t *testing.T, ref string) domain.Reference {
	t.Helper()
	parsed, err := domain.Parse(ref)
	if err != nil {
		t.Fatalf("parse ref %q: %v", ref, err)
	}
	return parsed
}
