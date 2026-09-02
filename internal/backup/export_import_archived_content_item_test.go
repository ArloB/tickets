package backup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// TestExportThenImportPreservesArchivedContentItemStatus mirrors
// TestExportThenImportPreservesArchivedProjectStatus (ADR 0028): a
// plan/document's status is a field on the same content_items row that
// test already covers for projects, but nothing previously exercised
// an archived value specifically — the same easy-to-drop risk, since
// "active" is also the zero-ish default a forgotten mapping would
// produce.
func TestExportThenImportPreservesArchivedContentItemStatus(t *testing.T) {
	ctx := context.Background()
	srcDir := filepath.Join(t.TempDir(), "src")
	srcSvc := newTestService(t, srcDir)

	if _, err := srcSvc.CreateProject(ctx, service.CreateProjectRequest{Key: "ARC", Title: "Archived"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	plan, err := srcSvc.CreateContentItem(ctx, service.CreateContentItemRequest{
		ProjectKey: "ARC", Kind: domain.KindPlan, Title: "Old plan", Representation: domain.ContentRepresentationMarkdown, Body: "stale",
	}, testActor, "cid-2", "", "")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	ref, err := domain.Parse(plan.Ref)
	if err != nil {
		t.Fatalf("parse plan ref: %v", err)
	}
	if _, err := srcSvc.SetContentItemStatus(ctx, service.SetContentItemStatusRequest{
		Ref: ref, NewStatus: domain.ContentItemStatusArchived, ExpectedVersion: plan.Version,
	}, testActor, "cid-3"); err != nil {
		t.Fatalf("archive plan: %v", err)
	}

	srcSt, err := store.Open(srcDir)
	if err != nil {
		t.Fatalf("reopen source store: %v", err)
	}
	defer func() { _ = srcSt.Close() }()
	srcBlobs := mustOpenBlobs(t, srcDir)

	attachmentsDir := filepath.Join(t.TempDir(), "attachments")
	env, err := Export(ctx, srcSt.DB(), srcBlobs, attachmentsDir)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(env.ContentItems) != 1 || env.ContentItems[0].Status != string(domain.ContentItemStatusArchived) {
		t.Fatalf("exported content items = %+v, want one item with status=archived", env.ContentItems)
	}

	dstDir := filepath.Join(t.TempDir(), "dst")
	dstSt := newTestServiceStore(t, dstDir)
	dstBlobs := mustOpenBlobs(t, dstDir)

	if _, err := Import(ctx, dstSt.DB(), env, attachmentsDir, dstBlobs, true); err != nil {
		t.Fatalf("Import (commit): %v", err)
	}

	dstSvc := service.New(dstSt, dstBlobs)
	item, err := dstSvc.GetContentItem(ctx, ref)
	if err != nil {
		t.Fatalf("get imported plan: %v", err)
	}
	if item.Status != domain.ContentItemStatusArchived {
		t.Errorf("imported plan status = %q, want archived — archive state must survive export/import, not silently revert to active", item.Status)
	}
}
