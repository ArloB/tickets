package backup

import (
	"bytes"
	"context"
	"fmt"
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
	ctx := context.Background()

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)

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

	// Close before Backup/Restore below — mirrors the documented
	// precondition ("the server must not be running") and, on Windows,
	// is required: a rename over a still-open file handle is refused
	// there (Access is denied), unlike POSIX, where it silently
	// succeeds against the open handle's now-unlinked inode.
	if err := st.Close(); err != nil {
		t.Fatalf("close store before backup: %v", err)
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
	// Reopened and closed again immediately, for the same reason.
	st2, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen store for post-backup mutation: %v", err)
	}
	svcMutate := service.New(st2, blobs)
	if _, err := svcMutate.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Post-backup ticket",
	}, testActor, "cid-4", "", ""); err != nil {
		t.Fatalf("create post-backup ticket: %v", err)
	}
	if err := st2.Close(); err != nil {
		t.Fatalf("close store after post-backup mutation: %v", err)
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
	ctx := context.Background()

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "backup")
	if _, err := Backup(ctx, dataDir, outputDir); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Close before the restore attempts below: WritePidfile alone is
	// what this test means to simulate — "a stale pidfile left by a
	// crash" (a real crash releases every OS handle the process held,
	// it doesn't leave tickets.db open) — not an actual live store
	// connection racing the restore.
	if err := st.Close(); err != nil {
		t.Fatalf("close store before restore attempts: %v", err)
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
	ctx := context.Background()

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)
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

	// A clean Close() triggers SQLite's own auto-checkpoint-on-last-
	// connection behavior and deletes the -wal file itself — so the
	// stale-WAL scenario this test guards against (an *unclean*
	// shutdown, a crash, leaving -wal behind) can't be reproduced by
	// simply closing normally. Simulate the crash directly: close (so
	// nothing holds an open handle — required for the rename below to
	// succeed on Windows), then recreate a non-empty -wal file by hand,
	// standing in for what a crash would have left on disk.
	if err := st.Close(); err != nil {
		t.Fatalf("close store before restore: %v", err)
	}
	walPath := filepath.Join(dataDir, dbFileName+"-wal")
	if err := os.WriteFile(walPath, []byte("stale WAL bytes a crash left behind"), 0o600); err != nil {
		t.Fatalf("recreate stale -wal file: %v", err)
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

// TestOnlineBackupDuringConcurrentWrites is Phase 6 Step 8's recovery
// drill for §15's "online backup taken during concurrent writes":
// Backup opens its own connection to the live database and runs
// VACUUM INTO while a separate goroutine keeps writing through the
// same service/store the "live server" would use — proving the
// backup doesn't need the writer to pause (busy_timeout(5000), WAL
// mode, ADR 0003) and that whatever snapshot it captures, mid-write or
// not, restores into a consistent, queryable database rather than a
// half-written or corrupted one.
func TestOnlineBackupDuringConcurrentWrites(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	svc := newTestService(t, dataDir)
	ctx := context.Background()

	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	stop := make(chan struct{})
	done := make(chan int)
	go func() {
		created := 0
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- created
				return
			default:
				if _, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
					ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: fmt.Sprintf("Concurrent ticket %d", i),
				}, testActor, "cid-writer", "", ""); err != nil {
					t.Errorf("concurrent CreateTicket: %v", err)
					done <- created
					return
				}
				created++
			}
		}
	}()

	outputDir := filepath.Join(t.TempDir(), "backup")
	manifest, err := Backup(ctx, dataDir, outputDir)
	if err != nil {
		t.Fatalf("Backup during concurrent writes: %v", err)
	}

	close(stop)
	totalCreated := <-done
	if totalCreated == 0 {
		t.Fatal("the concurrent writer never got a single ticket created before the backup finished — test isn't exercising real concurrency")
	}
	if len(manifest.Files) < 1 {
		t.Fatalf("manifest.Files = %d, want at least the database", len(manifest.Files))
	}

	restoreDir := filepath.Join(t.TempDir(), "restored")
	if err := Restore(ctx, restoreDir, outputDir, false); err != nil {
		t.Fatalf("Restore the concurrently-taken backup: %v", err)
	}

	restoredStore, err := store.Open(restoreDir)
	if err != nil {
		t.Fatalf("open restored store: %v", err)
	}
	defer func() { _ = restoredStore.Close() }()
	var integrityOK string
	if err := restoredStore.DB().QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrityOK); err != nil {
		t.Fatalf("PRAGMA integrity_check on restored database: %v", err)
	}
	if integrityOK != "ok" {
		t.Fatalf("PRAGMA integrity_check = %q, want ok — a concurrently-taken backup produced a corrupted database", integrityOK)
	}

	restoredBlobs, err := blobstore.Open(restoreDir)
	if err != nil {
		t.Fatalf("open restored blobstore: %v", err)
	}
	restored := service.New(restoredStore, restoredBlobs)
	result, err := restored.ListTickets(ctx, "ABC", service.TicketListViewPriorityQueue, 100, "")
	if err != nil {
		t.Fatalf("list tickets in restored database: %v", err)
	}
	if len(result.Tickets) > totalCreated {
		t.Errorf("restored ticket count = %d, want at most %d (the writer's total, since the backup could only have captured a prefix of it)", len(result.Tickets), totalCreated)
	}
}
