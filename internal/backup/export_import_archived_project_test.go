package backup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// TestExportThenImportPreservesArchivedProjectStatus is Phase 7's
// narrow addition to TestExportThenImportRoundTrip's coverage: a
// project's status (ADR 0021) is a field on the same projects row that
// test already exports and imports, but nothing previously exercised
// an archived value specifically — an easy thing for an export/import
// mapping to silently drop since "active" is also the zero-ish
// default a forgotten field would produce.
func TestExportThenImportPreservesArchivedProjectStatus(t *testing.T) {
	ctx := context.Background()
	srcDir := filepath.Join(t.TempDir(), "src")
	srcSvc := newTestService(t, srcDir)

	if _, err := srcSvc.CreateProject(ctx, service.CreateProjectRequest{Key: "ARC", Title: "Archived"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := srcSvc.SetProjectStatus(ctx, service.SetProjectStatusRequest{
		Key: "ARC", NewStatus: domain.ProjectStatusArchived, ExpectedVersion: 1,
	}, testActor, "cid-2"); err != nil {
		t.Fatalf("archive project: %v", err)
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
	if len(env.Projects) != 1 || env.Projects[0].Status != string(domain.ProjectStatusArchived) {
		t.Fatalf("exported project = %+v, want one project with status=archived", env.Projects)
	}

	dstDir := filepath.Join(t.TempDir(), "dst")
	dstSt := newTestServiceStore(t, dstDir)
	dstBlobs := mustOpenBlobs(t, dstDir)

	if _, err := Import(ctx, dstSt.DB(), env, attachmentsDir, dstBlobs, true); err != nil {
		t.Fatalf("Import (commit): %v", err)
	}

	dstSvc := service.New(dstSt, dstBlobs)
	proj, err := dstSvc.GetProject(ctx, "ARC")
	if err != nil {
		t.Fatalf("get imported project: %v", err)
	}
	if proj.Status != domain.ProjectStatusArchived {
		t.Errorf("imported project status = %q, want archived — archive state must survive export/import, not silently revert to active", proj.Status)
	}
}
