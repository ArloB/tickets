package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

var testActor = domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}

const testCorrelationID = "test-correlation-id"

func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	return New(st, blobs)
}

// TestReferenceAllocation is Phase 0 verification gate 4's assertion,
// as a test: reference counters are per-project and monotonic (ADR
// 0009). A global-instead-of-per-project counter must fail this.
func TestReferenceAllocation(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project ABC: %v", err)
	}
	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "XYZ", Title: "Second"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project XYZ: %v", err)
	}

	t1, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "Fix the parser"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket 1: %v", err)
	}
	t2, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Second ticket"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket 2: %v", err)
	}
	t3, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "XYZ", Type: domain.TicketTypeTask, Title: "Unrelated"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket 3: %v", err)
	}

	if t1.Ref != "ABC-1" {
		t.Errorf("t1.Ref = %q, want ABC-1", t1.Ref)
	}
	if t2.Ref != "ABC-2" {
		t.Errorf("t2.Ref = %q, want ABC-2", t2.Ref)
	}
	if t3.Ref != "XYZ-1" {
		t.Errorf("t3.Ref = %q, want XYZ-1", t3.Ref)
	}
	if t1.FeatureRef != "ABC-F1" {
		t.Errorf("t1.FeatureRef = %q, want ABC-F1 (General)", t1.FeatureRef)
	}
}

func TestCreateProjectGeneralFeature(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	proj, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example", Description: "desc"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if proj.Key != "ABC" || proj.Title != "Example" || proj.Status != domain.ProjectStatusActive || proj.Version != 1 {
		t.Errorf("unexpected project: %+v", proj)
	}

	got, err := s.GetProject(ctx, "ABC")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Key != proj.Key || got.CreatedAt != proj.CreatedAt {
		t.Errorf("GetProject mismatch: got %+v, want %+v", got, proj)
	}
}

func TestCreateProjectInvalidKey(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.CreateProject(ctx, CreateProjectRequest{Key: "abc", Title: "Example"}, testActor, testCorrelationID, "", "")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "key" {
		t.Fatalf("CreateProject(bad key) error = %v, want validation_failed on field key", err)
	}
}

func TestCreateProjectDuplicateKey(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "First"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Second"}, testActor, testCorrelationID, "", "")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrAlreadyExists {
		t.Fatalf("duplicate key error = %v, want already_exists", err)
	}
}

func TestGetProjectNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.GetProject(ctx, "NOPE")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("GetProject(missing) error = %v, want not_found", err)
	}
}

func TestTicketIdempotentCreateReplay(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	req := CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "Fix the parser"}
	fp, err := Fingerprint("POST", "/api/v1/projects/ABC/tickets", []byte(`{"title":"Fix the parser","type":"bug"}`))
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	first, err := s.CreateTicket(ctx, req, testActor, testCorrelationID, "key-123", fp)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := s.CreateTicket(ctx, req, testActor, testCorrelationID, "key-123", fp)
	if err != nil {
		t.Fatalf("replayed create: %v", err)
	}
	if first.Ref != second.Ref {
		t.Errorf("idempotent replay created a different ticket: first=%s second=%s", first.Ref, second.Ref)
	}

	// A third, non-replayed create must still allocate a new reference —
	// the idempotency cache must not suppress genuinely new requests.
	third, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Different ticket"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("third create: %v", err)
	}
	if third.Ref == first.Ref {
		t.Errorf("distinct request got the same reference as the idempotent replay: %s", third.Ref)
	}
}

// TestIdempotentReplayReturnsFullRecordNotASnapshot guards against the
// idempotency cache silently dropping fields. An earlier design cached
// a JSON-marshaled *domain.Ticket* and decoded it back on replay -
// which silently zeroed UUID (tagged json:"-", so it never round-trips
// through JSON) and would have zeroed any future field a later phase
// adds without a matching json tag update here. The fix re-fetches the
// live record by reference on every replay instead of trusting a
// cached snapshot; this test asserts the replay's UUID is non-empty
// and identical to the original, which only holds if a real re-fetch
// happened.
func TestIdempotentReplayReturnsFullRecordNotASnapshot(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	req := CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeBug, Title: "Fix the parser"}
	fp, err := Fingerprint("POST", "/api/v1/projects/ABC/tickets", []byte(`{"title":"Fix the parser","type":"bug"}`))
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	first, err := s.CreateTicket(ctx, req, testActor, testCorrelationID, "replay-uuid-key", fp)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.UUID == "" {
		t.Fatalf("fresh create returned an empty UUID")
	}

	second, err := s.CreateTicket(ctx, req, testActor, testCorrelationID, "replay-uuid-key", fp)
	if err != nil {
		t.Fatalf("replayed create: %v", err)
	}
	if second.UUID == "" {
		t.Errorf("replayed create returned an empty UUID — idempotency cache is dropping json:\"-\" fields")
	}
	if second.UUID != first.UUID {
		t.Errorf("replayed create UUID = %q, want %q (same underlying ticket)", second.UUID, first.UUID)
	}

	// Stronger proof of the same re-fetch mechanism, extended to the
	// two fields Phase 1 added to domain.Ticket after this test was
	// written (Assignee, DeletedAt): mutate the ticket between the
	// first create and a second replay of the same idempotency key. A
	// cached snapshot from creation time would show no assignee; a real
	// re-fetch reflects the assignment made in between.
	ref, err := domain.Parse(first.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	if _, err := s.AssignTicket(ctx, AssignTicketRequest{Ref: ref, Assignee: &testActor, ExpectedVersion: first.Version}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AssignTicket: %v", err)
	}

	third, err := s.CreateTicket(ctx, req, testActor, testCorrelationID, "replay-uuid-key", fp)
	if err != nil {
		t.Fatalf("second replayed create: %v", err)
	}
	if third.Assignee == nil || *third.Assignee != testActor {
		t.Errorf("second replay's Assignee = %v, want %v — idempotency cache is returning a creation-time snapshot instead of a live re-fetch", third.Assignee, testActor)
	}
}

func TestTicketIdempotencyKeyReusedWithDifferentBody(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	fp1, _ := Fingerprint("POST", "/api/v1/projects/ABC/tickets", []byte(`{"title":"A"}`))
	fp2, _ := Fingerprint("POST", "/api/v1/projects/ABC/tickets", []byte(`{"title":"B"}`))

	if _, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "A"}, testActor, testCorrelationID, "dup-key", fp1); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "B"}, testActor, testCorrelationID, "dup-key", fp2)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrIdempotencyKeyReused {
		t.Fatalf("reused key with different body error = %v, want idempotency_key_reused", err)
	}
}

func TestUpdateTicketStatusVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	ref, err := domain.Parse(ticket.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	// Correct version succeeds and bumps the version.
	updated, err := s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{Ref: ref, NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("update with correct version: %v", err)
	}
	if updated.Status != domain.WorkflowStatusInProgress || updated.Version != ticket.Version+1 {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	// Stale version (the original, now-superseded one) must 409 and
	// report the current version.
	_, err = s.UpdateTicketStatus(ctx, UpdateTicketStatusRequest{Ref: ref, NewStatus: domain.WorkflowStatusDone, ExpectedVersion: ticket.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("stale update error = %v, want version_conflict", err)
	}
	if svcErr.CurrentVersion == nil || *svcErr.CurrentVersion != updated.Version {
		t.Fatalf("version_conflict CurrentVersion = %v, want %d", svcErr.CurrentVersion, updated.Version)
	}
}

func TestUpdateFeatureStatusVersionConflict(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "F", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create feature: %v", err)
	}
	if feature.Status != domain.WorkflowStatusBacklog {
		t.Fatalf("new feature status = %q, want backlog", feature.Status)
	}
	ref, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	// Correct version succeeds and bumps the version.
	updated, err := s.UpdateFeatureStatus(ctx, UpdateFeatureStatusRequest{Ref: ref, NewStatus: domain.WorkflowStatusInProgress, ExpectedVersion: feature.Version}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("update with correct version: %v", err)
	}
	if updated.Status != domain.WorkflowStatusInProgress || updated.Version != feature.Version+1 {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	// Stale version (the original, now-superseded one) must 409 and
	// report the current version.
	_, err = s.UpdateFeatureStatus(ctx, UpdateFeatureStatusRequest{Ref: ref, NewStatus: domain.WorkflowStatusDone, ExpectedVersion: feature.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("stale update error = %v, want version_conflict", err)
	}
	if svcErr.CurrentVersion == nil || *svcErr.CurrentVersion != updated.Version {
		t.Fatalf("version_conflict CurrentVersion = %v, want %d", svcErr.CurrentVersion, updated.Version)
	}
}

func TestUpdateFeatureStatusInvalidStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: "ABC", Title: "Example"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}
	feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "F", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("create feature: %v", err)
	}
	ref, err := domain.Parse(feature.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	_, err = s.UpdateFeatureStatus(ctx, UpdateFeatureStatusRequest{Ref: ref, NewStatus: domain.WorkflowStatus("bogus"), ExpectedVersion: feature.Version}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("invalid status error = %v, want validation_failed", err)
	}
}

func TestListProjectsCursorPagination(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	for _, key := range []string{"AAA", "BBB", "CCC"} {
		if _, err := s.CreateProject(ctx, CreateProjectRequest{Key: key, Title: key}, testActor, testCorrelationID, "", ""); err != nil {
			t.Fatalf("create %s: %v", key, err)
		}
	}

	page1, err := s.ListProjects(ctx, 2, "", false)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Projects) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 projects and a non-empty cursor", page1)
	}
	if page1.Projects[0].Key != "AAA" || page1.Projects[1].Key != "BBB" {
		t.Fatalf("page1 order = %v, want [AAA BBB]", []string{page1.Projects[0].Key, page1.Projects[1].Key})
	}

	page2, err := s.ListProjects(ctx, 2, page1.NextCursor, false)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Projects) != 1 || page2.Projects[0].Key != "CCC" {
		t.Fatalf("page2 = %+v, want [CCC]", page2)
	}
	if page2.NextCursor != "" {
		t.Errorf("page2.NextCursor = %q, want empty (last page)", page2.NextCursor)
	}
}
