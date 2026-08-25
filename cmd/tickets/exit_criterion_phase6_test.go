package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path/filepath"
	"testing"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// TestExitCriterionPhase6BackupRestoreDrill is Phase 6's automated
// exit-criterion gate (plan.md's Phase 6 Step 11, mirroring the
// per-phase pattern set by internal/httpapi/exit_criterion_test.go
// and cmd/tickets/exit_criterion_phase3_test.go): seed a reference
// installation touching every kind this criterion names — a ticket
// with version history whose description carries a "#ABC-D1"-style
// derived mention (ADR 0015, so "references" here means the actual
// backlink graph §16 criterion 7 is about, not just the external link
// added alongside it), a decision with version history, an
// attachment, an external link, and a comment (which also produces an
// audit event) — take a backup, mutate the live data directory in a
// way that must NOT survive, restore, and assert records, the
// attachment's bytes, both kinds of reference, version history, audit
// history, and checksums (via `tickets admin integrity`'s own check,
// run for real against the restored data directory) all match what
// was true at backup time.
//
// docs/mvp-acceptance.md row 16 cites internal/backup's own unit tests
// for the underlying backup/restore mechanism; this test is the
// broader, cross-entity drill plan.md's Phase 6 Step 11 asks for
// specifically, run once at the top level the way Phase 3's exit
// criterion is.
func TestExitCriterionPhase6BackupRestoreDrill(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx := context.Background()
	// "local" is the seeded human actor migration 0002 creates in
	// every installation (ADR 0012) — no separate account-creation
	// step needed to attribute this drill's mutations.
	actor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)

	// --- Seed a reference installation ---

	if _, err := svc.CreateProject(ctx, service.CreateProjectRequest{
		Key: "ABC", Title: "Reference Project",
	}, actor, "cid-project", "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	decision, err := svc.CreateDecision(ctx, service.CreateDecisionRequest{
		ProjectKey: "ABC", Title: "Use SQLite", Decision: "v1: use SQLite",
	}, actor, "cid-decision", "", "")
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	decisionRef, err := domain.Parse(decision.Ref)
	if err != nil {
		t.Fatalf("parse decision ref: %v", err)
	}
	if _, err := svc.UpdateDecision(ctx, service.UpdateDecisionRequest{
		Ref: decisionRef, Title: decision.Title, Context: decision.Context,
		Decision: "v2: use SQLite in WAL mode", Rationale: decision.Rationale,
		Consequences: decision.Consequences, Status: decision.Status, ExpectedVersion: decision.Version,
	}, actor, "cid-decision-update"); err != nil {
		t.Fatalf("update decision: %v", err)
	}

	// The ticket's description mentions the decision by reference
	// (ADR 0015's derived-mentions scanner) — this is what makes
	// "references" in this drill mean the #ABC-D1-style backlink graph
	// §16 criterion 7 is about, not just the external link added below.
	ticket, err := svc.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Reference ticket",
		Description: "See #" + decision.Ref + " for prior context.",
	}, actor, "cid-ticket", "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ticketRef, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ticket ref: %v", err)
	}
	updatedTicket, err := svc.UpdateTicketStatus(ctx, service.UpdateTicketStatusRequest{
		Ref: ticketRef, NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: ticket.Version,
	}, actor, "cid-ticket-status")
	if err != nil {
		t.Fatalf("update ticket status: %v", err)
	}

	attachmentContent := []byte("reference attachment bytes for the exit-criterion drill")
	sum := sha256.Sum256(attachmentContent)
	wantAttachmentHash := hex.EncodeToString(sum[:])
	attachment, err := svc.CreateAttachment(ctx, service.CreateAttachmentRequest{
		Ref: ticketRef, Title: "reference.txt", Kind: domain.AttachmentKindUpload,
		Content: bytes.NewReader(attachmentContent), FileName: "reference.txt", MediaType: "text/plain",
	}, actor, "cid-attachment")
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	if _, err := svc.AddExternalLink(ctx, service.AddExternalLinkRequest{
		Ref: ticketRef, Title: "Vendor incident", URL: "https://vendor.example/incident/1",
	}, actor, "cid-link"); err != nil {
		t.Fatalf("add external link: %v", err)
	}

	if _, err := svc.AddComment(ctx, service.AddCommentRequest{
		Ref: ticketRef, Body: "Kicked off the reference drill.",
	}, actor, "cid-comment", "", ""); err != nil {
		t.Fatalf("add comment: %v", err)
	}

	// --- Capture pre-backup state to compare against after restore ---

	preTicket, err := svc.GetTicket(ctx, ticketRef)
	if err != nil {
		t.Fatalf("get ticket before backup: %v", err)
	}
	preDecisionVersions, err := svc.ListDecisionVersions(ctx, decisionRef)
	if err != nil {
		t.Fatalf("list decision versions before backup: %v", err)
	}
	preLinks, err := svc.GetExternalLinks(ctx, ticketRef)
	if err != nil {
		t.Fatalf("get external links before backup: %v", err)
	}
	preComments, err := svc.ListComments(ctx, ticketRef)
	if err != nil {
		t.Fatalf("list comments before backup: %v", err)
	}
	preActivity, err := svc.ListActivity(ctx, "ABC", service.ActivityListFilters{}, 50, "")
	if err != nil {
		t.Fatalf("list activity before backup: %v", err)
	}
	preBacklinks, err := svc.GetBacklinks(ctx, decisionRef)
	if err != nil {
		t.Fatalf("get decision backlinks before backup: %v", err)
	}
	if len(preBacklinks) != 1 || preBacklinks[0].SourceRef != ticket.Ref {
		t.Fatalf("decision backlinks before backup = %+v, want exactly one from %s (the derived mention in its description)", preBacklinks, ticket.Ref)
	}

	// Close before Backup/Restore — the documented "server must not be
	// running" precondition (docs/backup-recovery.md), and required on
	// Windows: a rename over a still-open file handle is refused there
	// (Access is denied), unlike POSIX (Phase 6 Step 8's finding).
	if err := st.Close(); err != nil {
		t.Fatalf("close store before backup: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "backup")
	manifest, err := backup.Backup(ctx, dataDir, outputDir)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if len(manifest.Files) < 2 {
		t.Fatalf("manifest.Files = %d, want at least the database and the attachment blob", len(manifest.Files))
	}

	// --- Mutate the live data directory; this must NOT survive restore ---

	st2, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen store for post-backup mutation: %v", err)
	}
	svcMutate := service.New(st2, blobs)
	if _, err := svcMutate.CreateTicket(ctx, service.CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Post-backup ticket (must not survive)",
	}, actor, "cid-post-backup", "", ""); err != nil {
		t.Fatalf("create post-backup ticket: %v", err)
	}
	if err := st2.Close(); err != nil {
		t.Fatalf("close store after post-backup mutation: %v", err)
	}

	if err := backup.Restore(ctx, dataDir, outputDir, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// --- Checksums: the same integrity check an operator runs for real ---

	stCheck, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store for integrity check: %v", err)
	}
	blobsCheck, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("open blobstore for integrity check: %v", err)
	}
	report, err := buildIntegrityReport(ctx, stCheck, blobsCheck, false)
	if err != nil {
		t.Fatalf("buildIntegrityReport: %v", err)
	}
	if err := stCheck.Close(); err != nil {
		t.Fatalf("close store after integrity check: %v", err)
	}
	if !report.DatabaseOK {
		t.Errorf("restored database failed PRAGMA integrity_check: %v", report.DatabaseMessages)
	}
	if len(report.ForeignKeyViolations) > 0 {
		t.Errorf("restored database has foreign key violations: %+v", report.ForeignKeyViolations)
	}
	if len(report.CorruptedBlobs) > 0 {
		t.Errorf("restored blob store has corrupted blobs (checksum mismatch): %+v", report.CorruptedBlobs)
	}

	// --- Records, references, versions, audit history: reopen and compare ---

	stPost, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen store after restore: %v", err)
	}
	t.Cleanup(func() { _ = stPost.Close() })
	blobsPost, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen blobstore after restore: %v", err)
	}
	svcPost := service.New(stPost, blobsPost)

	tickets, err := svcPost.ListTickets(ctx, "ABC", service.TicketListViewPriorityQueue, 20, "")
	if err != nil {
		t.Fatalf("list tickets after restore: %v", err)
	}
	if len(tickets.Tickets) != 1 {
		t.Fatalf("tickets after restore = %d, want exactly the one ticket present at backup time (the post-backup ticket must not survive)", len(tickets.Tickets))
	}

	postTicket, err := svcPost.GetTicket(ctx, ticketRef)
	if err != nil {
		t.Fatalf("get ticket after restore: %v", err)
	}
	if postTicket.Ref != preTicket.Ref || postTicket.Version != preTicket.Version || postTicket.Status != preTicket.Status {
		t.Errorf("ticket after restore = {ref:%s version:%d status:%s}, want {ref:%s version:%d status:%s}",
			postTicket.Ref, postTicket.Version, postTicket.Status, preTicket.Ref, preTicket.Version, preTicket.Status)
	}
	if postTicket.Version != updatedTicket.Version {
		t.Errorf("restored ticket lost its status-update version: got version %d, want %d (the version UpdateTicketStatus produced)", postTicket.Version, updatedTicket.Version)
	}

	postDecisionVersions, err := svcPost.ListDecisionVersions(ctx, decisionRef)
	if err != nil {
		t.Fatalf("list decision versions after restore: %v", err)
	}
	if len(postDecisionVersions) != len(preDecisionVersions) {
		t.Fatalf("decision version count after restore = %d, want %d", len(postDecisionVersions), len(preDecisionVersions))
	}
	for i := range preDecisionVersions {
		if postDecisionVersions[i].Decision != preDecisionVersions[i].Decision {
			t.Errorf("decision version %d content after restore = %q, want %q", i, postDecisionVersions[i].Decision, preDecisionVersions[i].Decision)
		}
	}

	dl, err := svcPost.DownloadAttachment(ctx, attachment.ID)
	if err != nil {
		t.Fatalf("download attachment after restore: %v", err)
	}
	gotBytes, err := io.ReadAll(dl.Content)
	_ = dl.Content.Close()
	if err != nil {
		t.Fatalf("read attachment bytes after restore: %v", err)
	}
	gotSum := sha256.Sum256(gotBytes)
	if hex.EncodeToString(gotSum[:]) != wantAttachmentHash {
		t.Errorf("attachment content hash after restore = %x, want %s", gotSum, wantAttachmentHash)
	}
	if !bytes.Equal(gotBytes, attachmentContent) {
		t.Errorf("attachment bytes after restore do not match what was uploaded before backup")
	}

	postLinks, err := svcPost.GetExternalLinks(ctx, ticketRef)
	if err != nil {
		t.Fatalf("get external links after restore: %v", err)
	}
	if len(postLinks) != len(preLinks) {
		t.Fatalf("external link count after restore = %d, want %d", len(postLinks), len(preLinks))
	}
	if len(postLinks) > 0 && postLinks[0].URL != preLinks[0].URL {
		t.Errorf("external link URL after restore = %q, want %q", postLinks[0].URL, preLinks[0].URL)
	}

	postComments, err := svcPost.ListComments(ctx, ticketRef)
	if err != nil {
		t.Fatalf("list comments after restore: %v", err)
	}
	if len(postComments) != len(preComments) {
		t.Fatalf("comment count after restore = %d, want %d", len(postComments), len(preComments))
	}
	if len(postComments) > 0 && postComments[0].Body != preComments[0].Body {
		t.Errorf("comment body after restore = %q, want %q", postComments[0].Body, preComments[0].Body)
	}

	postActivity, err := svcPost.ListActivity(ctx, "ABC", service.ActivityListFilters{}, 50, "")
	if err != nil {
		t.Fatalf("list activity after restore: %v", err)
	}
	if len(postActivity.Events) != len(preActivity.Events) {
		t.Fatalf("audit/activity event count after restore = %d, want %d (the post-backup ticket's creation event must not appear)", len(postActivity.Events), len(preActivity.Events))
	}

	postBacklinks, err := svcPost.GetBacklinks(ctx, decisionRef)
	if err != nil {
		t.Fatalf("get decision backlinks after restore: %v", err)
	}
	if len(postBacklinks) != len(preBacklinks) || (len(postBacklinks) > 0 && postBacklinks[0].SourceRef != preBacklinks[0].SourceRef) {
		t.Errorf("decision backlinks after restore = %+v, want %+v (the derived #%s mention in the ticket's description)", postBacklinks, preBacklinks, decision.Ref)
	}
}
