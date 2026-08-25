package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

var testActor = domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}

func newTestService(t *testing.T, dataDir string) *service.Service {
	t.Helper()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	return service.New(st, blobs)
}

// TestBackupThenRestoreReproducesState is this package's core
// regression test: a project, a ticket, and an uploaded attachment
// backed up, then a further mutation applied, then restored — the
// restore must reproduce the state as of the backup, not the mutation
// made after it.
func TestBackupThenRestoreReproducesState(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	svc := newTestService(t, dataDir)
	ctx := context.Background()

	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Original ticket",
	}, testActor, "cid-2", "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	content := []byte("attachment bytes")
	if _, err := svc.CreateAttachment(ctx, service.CreateAttachmentRequest{
		Ref: ticketRef, Title: "file.txt", Kind: domain.AttachmentKindUpload,
		Content: bytes.NewReader(content), FileName: "file.txt", MediaType: "text/plain",
	}, testActor, "cid-3"); err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "backup")
	manifest, err := Backup(ctx, dataDir, outputDir)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if len(manifest.Files) < 2 {
		t.Fatalf("manifest.Files = %d, want at least the database and one blob", len(manifest.Files))
	}

	// Mutate after the backup: this must NOT survive the restore below.
	if _, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Post-backup ticket",
	}, testActor, "cid-4", "", ""); err != nil {
		t.Fatalf("create post-backup ticket: %v", err)
	}

	if err := Restore(ctx, dataDir, outputDir, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	svc2 := newTestService(t, dataDir)
	result, err := svc2.ListTickets(ctx, "ABC", service.TicketListViewPriorityQueue, 20, "")
	if err != nil {
		t.Fatalf("list tickets after restore: %v", err)
	}
	if len(result.Tickets) != 1 {
		t.Fatalf("tickets after restore = %d, want exactly the one ticket present at backup time", len(result.Tickets))
	}
	if result.Tickets[0].Title != "Original ticket" {
		t.Errorf("surviving ticket title = %q, want %q", result.Tickets[0].Title, "Original ticket")
	}
}

func TestBackupRefusesExistingOutputDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	newTestService(t, dataDir)

	outputDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("premake output dir: %v", err)
	}

	if _, err := Backup(context.Background(), dataDir, outputDir); err == nil {
		t.Fatal("Backup into an existing directory: want an error, got nil")
	}
}

// TestRestoreRefusesCorruptedChecksumAndLeavesDataDirUntouched
// confirms restore verifies every manifest checksum before touching
// active state: a tampered backup file must be refused, and the
// original project must still be readable afterward.
func TestRestoreRefusesCorruptedChecksumAndLeavesDataDirUntouched(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	svc := newTestService(t, dataDir)
	ctx := context.Background()
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "backup")
	if _, err := Backup(ctx, dataDir, outputDir); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, dbFileName), []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper with backup db: %v", err)
	}

	if err := Restore(ctx, dataDir, outputDir, false); err == nil {
		t.Fatal("Restore with a tampered backup: want an error, got nil")
	}

	svc2 := newTestService(t, dataDir)
	if _, err := svc2.GetProject(ctx, "ABC"); err != nil {
		t.Fatalf("original project unreadable after refused restore: %v", err)
	}
}

func TestRestoreRefusesWhileServerRunningUnlessForced(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	svc := newTestService(t, dataDir)
	ctx := context.Background()
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "backup")
	if _, err := Backup(ctx, dataDir, outputDir); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if err := store.WritePidfile(dataDir); err != nil {
		t.Fatalf("WritePidfile: %v", err)
	}
	t.Cleanup(func() { _ = store.RemovePidfile(dataDir) })

	if err := Restore(ctx, dataDir, outputDir, false); err == nil {
		t.Fatal("Restore with a pidfile present and force=false: want an error, got nil")
	}
	if err := Restore(ctx, dataDir, outputDir, true); err != nil {
		t.Fatalf("Restore with force=true: %v", err)
	}
}

// TestRestoreRemovesStaleWAL confirms restore clears a pre-existing
// -wal/-shm pair rather than leaving SQLite to try to replay the *old*
// database's WAL against the newly restored file — VACUUM INTO's
// output is self-contained and has no WAL of its own.
func TestRestoreRemovesStaleWAL(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	svc := newTestService(t, dataDir)
	ctx := context.Background()
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "backup")
	if _, err := Backup(ctx, dataDir, outputDir); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Write more so the WAL file is non-empty at restore time — a
	// fresh t.TempDir() never exercises this path (no WAL exists yet
	// on first Open), so this write is what actually reproduces it.
	if _, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Forces a WAL",
	}, testActor, "cid-2", "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, dbFileName+"-wal")); err != nil {
		t.Skipf("no -wal file present to test against (driver/pragma behavior): %v", err)
	}

	if err := Restore(ctx, dataDir, outputDir, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, dbFileName+"-wal")); !os.IsNotExist(err) {
		t.Errorf("-wal file still present after restore: %v", err)
	}

	svc2 := newTestService(t, dataDir)
	result, err := svc2.ListTickets(ctx, "ABC", service.TicketListViewPriorityQueue, 20, "")
	if err != nil {
		t.Fatalf("list tickets after restore: %v", err)
	}
	if len(result.Tickets) != 0 {
		t.Errorf("tickets after restore = %d, want 0 (backup predates the ticket)", len(result.Tickets))
	}
}
