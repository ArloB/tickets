package store

import (
	"context"
	"strconv"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

// testProjectWithFeatures creates a project (no General feature — the
// tests below want full control over every feature's position) and n
// features, all at the same priority so position is the only ordering
// signal, in insertion order — insertion order and id order therefore
// coincide, which the assertions below rely on.
func testProjectWithFeatures(t *testing.T, db Querier, key string, positions []int64) (projID int64, featureIDs []int64) {
	t.Helper()
	ctx := context.Background()
	sysID := mustSystemActorID(t, db)

	projID, _, err := InsertEntity(ctx, db, nil, domain.KindProject, sysID, Now())
	if err != nil {
		t.Fatalf("InsertEntity project: %v", err)
	}
	if err := InsertProject(ctx, db, projID, key, "Example", ""); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}

	for i, pos := range positions {
		featID, _, err := InsertEntity(ctx, db, &projID, domain.KindFeature, sysID, Now())
		if err != nil {
			t.Fatalf("InsertEntity feature %d: %v", i, err)
		}
		if err := InsertFeature(ctx, db, featID, projID, int64(i+1), "Feature", "", "medium", pos); err != nil {
			t.Fatalf("InsertFeature %d: %v", i, err)
		}
		featureIDs = append(featureIDs, featID)
	}
	return projID, featureIDs
}

// decodeFeatureCursorForTest is a minimal local decode — this package
// can't import internal/service's decodeFeatureCursor (that would be a
// circular import), and a store-level test only needs to prove the
// opaque cursor this package encoded is the one it also accepts back,
// not exercise internal/service's validation layer (covered by
// internal/service/feature_test.go instead).
func decodeFeatureCursorForTest(t *testing.T, cursor string) (rank, position, id int64) {
	t.Helper()
	parts, err := DecodeCursor(cursor, 3)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	vals := make([]int64, 3)
	for i, p := range parts {
		if p == "" {
			continue
		}
		n, perr := strconv.ParseInt(p, 10, 64)
		if perr != nil {
			t.Fatalf("parse cursor component %q: %v", p, perr)
		}
		vals[i] = n
	}
	return vals[0], vals[1], vals[2]
}

// TestListFeaturesForProjectPagePaginatesAcrossBoundary proves a
// limit smaller than the feature count returns exactly limit rows plus
// a usable NextCursor, and replaying that cursor picks up exactly
// where the first page left off with no gap or overlap.
func TestListFeaturesForProjectPagePaginatesAcrossBoundary(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	db := s.DB()

	projID, featureIDs := testProjectWithFeatures(t, db, "ABC", []int64{1000, 2000, 3000})

	page1, err := ListFeaturesForProjectPage(context.Background(), db, projID, FeatureFilters{}, 2, 0, 0, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Features) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 features and a non-empty cursor", page1)
	}
	if page1.Features[0].ID != featureIDs[0] || page1.Features[1].ID != featureIDs[1] {
		t.Errorf("page1 ids = [%d, %d], want [%d, %d]", page1.Features[0].ID, page1.Features[1].ID, featureIDs[0], featureIDs[1])
	}

	rank, position, id := decodeFeatureCursorForTest(t, page1.NextCursor)
	page2, err := ListFeaturesForProjectPage(context.Background(), db, projID, FeatureFilters{}, 2, rank, position, id)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Features) != 1 || page2.NextCursor != "" {
		t.Fatalf("page2 = %+v, want 1 feature and no next cursor (last page)", page2)
	}
	if page2.Features[0].ID != featureIDs[2] {
		t.Errorf("page2 id = %d, want %d", page2.Features[0].ID, featureIDs[2])
	}
}
