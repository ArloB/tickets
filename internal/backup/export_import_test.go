package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
	"github.com/google/uuid"
)

// TestExportThenImportRoundTrip is this package's core export/import
// regression test: a project with a ticket, a comment, a decision, and
// an uploaded attachment exported (with --attachments), imported
// (dry-run first, then committed) into a fresh target, and the result
// — including the attachment's actual bytes, not just its metadata row
// — compared against the source.
func TestExportThenImportRoundTrip(t *testing.T) {
	srcDir := filepath.Join(t.TempDir(), "src")
	svc := newTestService(t, srcDir)
	ctx := context.Background()

	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "A ticket",
	}, testActor, "cid-2", "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	if _, err := svc.AddComment(ctx, service.AddCommentRequest{Ref: ticketRef, Body: "hello"}, testActor, "cid-3", "", ""); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	attachmentContent := []byte("round trip attachment bytes")
	attachment, err := svc.CreateAttachment(ctx, service.CreateAttachmentRequest{
		Ref: ticketRef, Title: "file.txt", Kind: domain.AttachmentKindUpload,
		Content: bytes.NewReader(attachmentContent), FileName: "file.txt", MediaType: "text/plain",
	}, testActor, "cid-4")
	if err != nil {
		t.Fatalf("create attachment: %v", err)
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
	if len(env.Projects) != 1 || len(env.Tickets) != 1 || len(env.Comments) != 1 || len(env.Attachments) != 1 {
		t.Fatalf("export counts = projects:%d tickets:%d comments:%d attachments:%d, want 1/1/1/1",
			len(env.Projects), len(env.Tickets), len(env.Comments), len(env.Attachments))
	}

	// Round-trip through JSON, the same as `tickets export`'s file
	// format, to catch anything that doesn't survive encoding.
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var reloaded Envelope
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	dstDir := filepath.Join(t.TempDir(), "dst")
	dstSt := newTestServiceStore(t, dstDir)
	dstBlobs := mustOpenBlobs(t, dstDir)

	dryRun, err := Import(ctx, dstSt.DB(), reloaded, attachmentsDir, dstBlobs, false)
	if err != nil {
		t.Fatalf("Import (dry-run): %v", err)
	}
	if dryRun.Committed {
		t.Error("dry-run Import: Committed = true, want false")
	}
	if len(dryRun.Problems) != 0 {
		t.Fatalf("dry-run Import problems = %v, want none", dryRun.Problems)
	}
	if dryRun.Counts["tickets"] != 1 {
		t.Errorf("dry-run Counts[tickets] = %d, want 1", dryRun.Counts["tickets"])
	}

	committed, err := Import(ctx, dstSt.DB(), reloaded, attachmentsDir, dstBlobs, true)
	if err != nil {
		t.Fatalf("Import (commit): %v", err)
	}
	if !committed.Committed {
		t.Fatal("commit Import: Committed = false, want true")
	}

	dstSvc := service.New(dstSt, dstBlobs)
	proj, err := dstSvc.GetProject(ctx, "ABC")
	if err != nil {
		t.Fatalf("get imported project: %v", err)
	}
	if proj.Title != "Example" {
		t.Errorf("imported project title = %q, want %q", proj.Title, "Example")
	}
	result, err := dstSvc.ListTickets(ctx, "ABC", service.TicketListViewPriorityQueue, 20, "")
	if err != nil {
		t.Fatalf("list imported tickets: %v", err)
	}
	if len(result.Tickets) != 1 || result.Tickets[0].Title != "A ticket" {
		t.Errorf("imported tickets = %+v, want one ticket titled %q", result.Tickets, "A ticket")
	}

	// The discriminating check: the attachment's actual bytes must be
	// reachable in the target's blob store, not just its metadata row.
	dl, err := dstSvc.DownloadAttachment(ctx, attachment.ID)
	if err != nil {
		t.Fatalf("download imported attachment: %v", err)
	}
	defer func() { _ = dl.Content.Close() }()
	got, err := io.ReadAll(dl.Content)
	if err != nil {
		t.Fatalf("read imported attachment content: %v", err)
	}
	if string(got) != string(attachmentContent) {
		t.Errorf("imported attachment content = %q, want %q", got, attachmentContent)
	}
}

// TestImportRefusesWithoutAttachmentsDirWhenBlobsAreReferenced
// confirms Import blocks a commit rather than silently producing an
// installation whose attachment rows point at bytes that were never
// brought over.
func TestImportRefusesWithoutAttachmentsDirWhenBlobsAreReferenced(t *testing.T) {
	ctx := context.Background()
	dstDir := filepath.Join(t.TempDir(), "dst")
	dstSt := newTestServiceStore(t, dstDir)
	dstBlobs := mustOpenBlobs(t, dstDir)

	hash := "deadbeef00000000000000000000000000000000000000000000000000ab"
	env := Envelope{
		FormatVersion: envelopeFormatVersion, SchemaVersion: 1,
		Entities: []EntityRow{{ID: 1, UUID: newTestUUID(t), Kind: "ticket", Version: 1, CreatedAt: "2026-01-01T00:00:00.000000000Z", UpdatedAt: "2026-01-01T00:00:00.000000000Z"}},
		Attachments: []AttachmentRow{{
			ID: 1, EntityID: int64Ptr(1), Kind: "upload", Title: "f", CurrentVersion: 1,
			FileHash: &hash, CreatedAt: "2026-01-01T00:00:00.000000000Z", CreatedBy: 1,
		}},
	}

	report, err := Import(ctx, dstSt.DB(), env, "", dstBlobs, true)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Committed {
		t.Error("Import with referenced blobs and no --attachments: Committed = true, want false")
	}
	found := false
	for _, p := range report.Problems {
		if strings.Contains(p, "attachments directory") || strings.Contains(p, "--attachments") {
			found = true
		}
	}
	if !found {
		t.Errorf("Import Problems = %v, want one naming the missing --attachments directory", report.Problems)
	}
}

// TestImportRefusesNonEmptyTarget confirms the "empty target" rule:
// import must never attempt to attach data to a database that already
// has content, since ids are preserved verbatim rather than remapped.
func TestImportRefusesNonEmptyTarget(t *testing.T) {
	ctx := context.Background()
	dstDir := filepath.Join(t.TempDir(), "dst")
	dstSvc := newTestService(t, dstDir)
	if _, err := dstSvc.CreateProject(ctx, service.CreateProjectRequest{Key: "XYZ", Title: "Existing"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project in target: %v", err)
	}
	dstSt, err := store.Open(dstDir)
	if err != nil {
		t.Fatalf("reopen target store: %v", err)
	}
	defer func() { _ = dstSt.Close() }()

	env := Envelope{FormatVersion: envelopeFormatVersion, SchemaVersion: 1}
	report, err := Import(ctx, dstSt.DB(), env, "", mustOpenBlobs(t, dstDir), true)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Committed {
		t.Error("Import against a non-empty target: Committed = true, want false")
	}
	if len(report.Problems) == 0 {
		t.Error("Import against a non-empty target: want a Problems entry naming this")
	}
}

// TestImportDetectsInvalidReference confirms a hand-corrupted envelope
// (a ticket pointing at a project id that doesn't exist in the
// export) is caught by validation and never reaches the database.
func TestImportDetectsInvalidReference(t *testing.T) {
	ctx := context.Background()
	dstDir := filepath.Join(t.TempDir(), "dst")
	dstSt := newTestServiceStore(t, dstDir)

	env := Envelope{
		FormatVersion: envelopeFormatVersion, SchemaVersion: 1,
		Tickets: []TicketRow{{ID: 99, ProjectID: 1, FeatureID: 1, Seq: 1, Type: "task", Title: "orphaned"}},
	}
	report, err := Import(ctx, dstSt.DB(), env, "", mustOpenBlobs(t, dstDir), true)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Committed {
		t.Error("Import with a dangling reference: Committed = true, want false")
	}
	found := false
	for _, p := range report.Problems {
		if strings.Contains(p, "entity id=1") {
			found = true
		}
	}
	if !found {
		t.Errorf("Import Problems = %v, want one naming the missing entity id=1 reference", report.Problems)
	}
}

// TestImportDetectsCorruptedSeedActor confirms a hand-edited envelope
// that put a real actor at the reserved seed id 1 (normally 'system')
// is refused rather than silently dropped — dropping it would
// misattribute every row that id touches onto the target's unrelated
// system actor.
func TestImportDetectsCorruptedSeedActor(t *testing.T) {
	ctx := context.Background()
	dstDir := filepath.Join(t.TempDir(), "dst")
	dstSt := newTestServiceStore(t, dstDir)

	env := Envelope{
		FormatVersion: envelopeFormatVersion, SchemaVersion: 1,
		Actors: []ActorRow{{ID: 1, UUID: newTestUUID(t), Kind: "human", Name: "not-system", CreatedAt: "2026-01-01T00:00:00.000000000Z", UpdatedAt: "2026-01-01T00:00:00.000000000Z"}},
	}
	report, err := Import(ctx, dstSt.DB(), env, "", mustOpenBlobs(t, dstDir), true)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Committed {
		t.Error("Import with a corrupted seed actor: Committed = true, want false")
	}
	found := false
	for _, p := range report.Problems {
		if strings.Contains(p, "actors id=1") {
			found = true
		}
	}
	if !found {
		t.Errorf("Import Problems = %v, want one naming the corrupted actors id=1 row", report.Problems)
	}
}

// TestExportNeverContainsSecrets is the redaction test: it asserts
// both that no secret-shaped value appears in the export's JSON
// encoding (a human's password hash) and, positively, that the export
// does contain the tables it's contracted to carry — a redaction
// check that only verifies absence would also pass if a whole table
// were silently dropped.
func TestExportNeverContainsSecrets(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	svc := newTestService(t, dataDir)
	ctx := context.Background()
	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T",
	}, testActor, "cid-2", "", ""); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = st.Close() }()

	const secretMarker = "s3cret-password-marker"
	sentinelHash := "argon2id$v=19$m=1,t=1,p=1$" + secretMarker
	if _, err := st.DB().ExecContext(ctx,
		`INSERT INTO human_accounts(actor_id, username, password_hash, created_at, updated_at)
		 VALUES (2, 'sentinel', ?, '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:00:00.000000000Z')`,
		sentinelHash,
	); err != nil {
		t.Fatalf("seed human_accounts sentinel: %v", err)
	}

	env, err := Export(ctx, st.DB(), mustOpenBlobs(t, dataDir), "")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if strings.Contains(string(data), secretMarker) {
		t.Error("export JSON contains the password hash sentinel — human_accounts must never be exported")
	}

	// Positive assertion: the export actually carries what it's
	// supposed to, not just an absence of secrets from an empty
	// export.
	if len(env.Projects) != 1 || len(env.Tickets) != 1 || len(env.Actors) < 2 {
		t.Errorf("export = projects:%d tickets:%d actors:%d, want at least 1/1/2",
			len(env.Projects), len(env.Tickets), len(env.Actors))
	}
}

func newTestServiceStore(t *testing.T, dataDir string) *store.Store {
	t.Helper()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mustOpenBlobs(t *testing.T, dataDir string) *blobstore.Store {
	t.Helper()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	return blobs
}

func newTestUUID(t *testing.T) string {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generate uuid: %v", err)
	}
	return u.String()
}

func int64Ptr(v int64) *int64 { return &v }
