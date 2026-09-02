package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateContentItemAllocatesReference(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Rollout plan", Body: "Do the thing",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(plan): %v", err)
	}
	if plan.Ref != "ABC-P1" {
		t.Errorf("plan.Ref = %q, want ABC-P1", plan.Ref)
	}
	if plan.Kind != domain.KindPlan {
		t.Errorf("plan.Kind = %q, want plan", plan.Kind)
	}
	if plan.Representation != "markdown" {
		t.Errorf("plan.Representation = %q, want markdown", plan.Representation)
	}
	if plan.Body != "Do the thing" {
		t.Errorf("plan.Body = %q, want %q", plan.Body, "Do the thing")
	}
	if plan.Version != 1 {
		t.Errorf("plan.Version = %d, want 1", plan.Version)
	}

	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Reference notes", Body: "Some notes",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(document): %v", err)
	}
	if doc.Ref != "ABC-DOC1" {
		t.Errorf("doc.Ref = %q, want ABC-DOC1", doc.Ref)
	}

	// Plans and documents number independently (ADR 0009) — a second
	// plan should be P2, not affected by the document created above.
	plan2, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Second plan",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(second plan): %v", err)
	}
	if plan2.Ref != "ABC-P2" {
		t.Errorf("plan2.Ref = %q, want ABC-P2", plan2.Ref)
	}
}

func TestCreateContentItemRequiresTitle(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan}, testActor, testCorrelationID, "", "")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "title" {
		t.Fatalf("CreateContentItem with no title = %v, want validation_failed on field title", err)
	}
}

func TestCreateContentItemRejectsInvalidKind(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	_, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindDecision, Title: "T"}, testActor, testCorrelationID, "", "")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "kind" {
		t.Fatalf("CreateContentItem with kind=decision = %v, want validation_failed on field kind", err)
	}
}

func TestCreateContentItemIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	fp, err := Fingerprint("POST", "/projects/ABC/plans", []byte(`{"title":"Rollout plan"}`))
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	first, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Rollout plan"}, testActor, testCorrelationID, "retry-key", fp)
	if err != nil {
		t.Fatalf("first CreateContentItem: %v", err)
	}
	second, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Rollout plan"}, testActor, testCorrelationID, "retry-key", fp)
	if err != nil {
		t.Fatalf("second CreateContentItem: %v", err)
	}
	if first.Ref != second.Ref {
		t.Errorf("idempotent replay created two content items: %v vs %v", first.Ref, second.Ref)
	}

	page, err := s.ListContentItems(ctx, "ABC", domain.KindPlan, 10, "", false)
	if err != nil {
		t.Fatalf("ListContentItems: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("plans after replay = %d, want exactly 1", len(page.Items))
	}
}

func TestUpdateContentItemConditionalVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Notes", Body: "v1"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, err := domain.Parse(doc.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	updated, err := s.UpdateContentItem(ctx, UpdateContentItemRequest{
		Ref: ref, Title: "Notes (revised)", Body: "v2", ExpectedVersion: doc.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateContentItem: %v", err)
	}
	if updated.Title != "Notes (revised)" || updated.Body != "v2" || updated.Version != 2 {
		t.Errorf("updated content item = %+v, want title/body updated, version=2", updated)
	}

	_, err = s.UpdateContentItem(ctx, UpdateContentItemRequest{
		Ref: ref, Title: "Stale write", Body: "v3", ExpectedVersion: doc.Version,
	}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("UpdateContentItem with a stale version = %v, want version_conflict", err)
	}
}

func TestUpdateContentItemArchivesPriorVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	item, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "v1 title", Body: "v1 body"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, _ := domain.Parse(item.Ref)

	if _, err := s.UpdateContentItem(ctx, UpdateContentItemRequest{
		Ref: ref, Title: "v2 title", Body: "v2 body", ExpectedVersion: item.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("UpdateContentItem: %v", err)
	}

	versions, err := s.ListContentItemVersions(ctx, ref)
	if err != nil {
		t.Fatalf("ListContentItemVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %+v, want exactly 1 archived version", versions)
	}
	if versions[0].Title != "v1 title" || versions[0].Body != "v1 body" || versions[0].Version != 1 {
		t.Errorf("archived version = %+v, want the pre-update v1 state", versions[0])
	}
}

// TestSetContentItemStatusArchiveIsVisibilityOnly mirrors
// TestSetProjectStatusArchiveIsVisibilityOnly (ADR 0028): archiving a
// plan hides it from the default ListContentItems page and
// RecentContentItems (project_brief's recent_plans) but leaves it
// fully readable, and — unlike a field edit — does not write a
// content_versions snapshot.
func TestSetContentItemStatusArchiveIsVisibilityOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	item, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Old plan", Body: "stale"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, _ := domain.Parse(item.Ref)

	archived, err := s.SetContentItemStatus(ctx, SetContentItemStatusRequest{
		Ref: ref, NewStatus: domain.ContentItemStatusArchived, ExpectedVersion: item.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("SetContentItemStatus archive: %v", err)
	}
	if archived.Status != domain.ContentItemStatusArchived {
		t.Errorf("Status = %q, want archived", archived.Status)
	}
	if archived.Version != item.Version+1 {
		t.Errorf("Version = %d, want %d", archived.Version, item.Version+1)
	}

	versions, err := s.ListContentItemVersions(ctx, ref)
	if err != nil {
		t.Fatalf("ListContentItemVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("versions = %+v, want none — archiving is a lifecycle flag, not a content edit", versions)
	}

	defaultPage, err := s.ListContentItems(ctx, "ABC", domain.KindPlan, 10, "", false)
	if err != nil {
		t.Fatalf("ListContentItems (default): %v", err)
	}
	if len(defaultPage.Items) != 0 {
		t.Errorf("ListContentItems default page = %+v, want the archived plan excluded", defaultPage.Items)
	}

	allPage, err := s.ListContentItems(ctx, "ABC", domain.KindPlan, 10, "", true)
	if err != nil {
		t.Fatalf("ListContentItems (includeArchived): %v", err)
	}
	if len(allPage.Items) != 1 || allPage.Items[0].Ref != item.Ref {
		t.Errorf("ListContentItems includeArchived=true = %+v, want the archived plan still present", allPage.Items)
	}

	fetched, err := s.GetContentItem(ctx, ref)
	if err != nil {
		t.Fatalf("GetContentItem (archived): %v", err)
	}
	if fetched.Status != domain.ContentItemStatusArchived {
		t.Errorf("GetContentItem status = %q, want archived — get stays status-blind", fetched.Status)
	}

	brief, err := s.ProjectBrief(ctx, "ABC")
	if err != nil {
		t.Fatalf("ProjectBrief: %v", err)
	}
	for _, p := range brief.RecentPlans {
		if p.Ref == item.Ref {
			t.Errorf("ProjectBrief.RecentPlans = %+v, want the archived plan excluded", brief.RecentPlans)
		}
	}

	unarchived, err := s.SetContentItemStatus(ctx, SetContentItemStatusRequest{
		Ref: ref, NewStatus: domain.ContentItemStatusActive, ExpectedVersion: archived.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("SetContentItemStatus unarchive: %v", err)
	}
	if unarchived.Status != domain.ContentItemStatusActive {
		t.Errorf("Status = %q, want active", unarchived.Status)
	}
}

func TestSetContentItemStatusRejectsStaleVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	item, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "P"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, _ := domain.Parse(item.Ref)

	_, err = s.SetContentItemStatus(ctx, SetContentItemStatusRequest{
		Ref: ref, NewStatus: domain.ContentItemStatusArchived, ExpectedVersion: item.Version + 1,
	}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrVersionConflict {
		t.Fatalf("SetContentItemStatus with a stale version = %v, want version_conflict", err)
	}
}

func TestListContentItemsFiltersByKind(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	if _, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "P"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindDocument, Title: "D"}, testActor, testCorrelationID, "", ""); err != nil {
		t.Fatalf("create document: %v", err)
	}

	plans, err := s.ListContentItems(ctx, "ABC", domain.KindPlan, 10, "", false)
	if err != nil {
		t.Fatalf("ListContentItems(plan): %v", err)
	}
	if len(plans.Items) != 1 || plans.Items[0].Title != "P" {
		t.Errorf("plans = %+v, want exactly the one plan", plans.Items)
	}

	docs, err := s.ListContentItems(ctx, "ABC", domain.KindDocument, 10, "", false)
	if err != nil {
		t.Fatalf("ListContentItems(document): %v", err)
	}
	if len(docs.Items) != 1 || docs.Items[0].Title != "D" {
		t.Errorf("documents = %+v, want exactly the one document", docs.Items)
	}
}

func TestGetContentItemDiffAcrossVersions(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	item, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindDocument, Title: "v1", Body: "line one"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, _ := domain.Parse(item.Ref)
	if _, err := s.UpdateContentItem(ctx, UpdateContentItemRequest{
		Ref: ref, Title: "v2", Body: "line one\nline two", ExpectedVersion: item.Version,
	}, testActor, testCorrelationID); err != nil {
		t.Fatalf("UpdateContentItem: %v", err)
	}

	diff, err := s.GetContentItemDiff(ctx, ref, 1, 2)
	if err != nil {
		t.Fatalf("GetContentItemDiff: %v", err)
	}
	if len(diff.Body) == 0 {
		t.Fatal("diff.Body is empty, want at least the added line")
	}
	foundAdd := false
	for _, l := range diff.Body {
		if l.Op == domain.DiffAdd && l.Text == "line two" {
			foundAdd = true
		}
	}
	if !foundAdd {
		t.Errorf("diff.Body = %+v, want an add line for %q", diff.Body, "line two")
	}
}

func TestGetContentItemDiffRejectsUnknownVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	item, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "v1"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, _ := domain.Parse(item.Ref)

	_, err = s.GetContentItemDiff(ctx, ref, 1, 99)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed || svcErr.Field != "version" {
		t.Fatalf("GetContentItemDiff with unknown version = %v, want validation_failed on field version", err)
	}
}

// TestTicketCanAssociateWithPlan proves the resolveAssociationEndpoint
// extension works for content items too, mirroring
// TestTicketCanAssociateWithDecision.
func TestTicketCanAssociateWithPlan(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Plan"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ticketRef, _ := domain.Parse(ticket.Ref)
	planRef, _ := domain.Parse(plan.Ref)

	if err := s.AddAssociation(ctx, AddAssociationRequest{SourceRef: ticketRef, TargetRef: planRef}, testActor, testCorrelationID); err != nil {
		t.Fatalf("AddAssociation ticket->plan: %v", err)
	}

	associated, err := s.GetAssociations(ctx, ticketRef)
	if err != nil {
		t.Fatalf("GetAssociations: %v", err)
	}
	found := false
	for _, a := range associated {
		if formatted, ferr := domain.Format(a); ferr == nil && formatted == plan.Ref {
			found = true
		}
	}
	if !found {
		t.Errorf("ticket associations = %+v, want it to include %s", associated, plan.Ref)
	}
}

// TestTicketDescriptionMentionsDocument proves the mentions.go
// extension: a #ABC-DOC1-style reference resolves to the document.
func TestTicketDescriptionMentionsDocument(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Doc"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ticket, err := s.CreateTicket(ctx, CreateTicketRequest{
		ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "T", Description: "See #" + doc.Ref,
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ticketRef, _ := domain.Parse(ticket.Ref)

	mentions, err := s.GetTicketMentions(ctx, ticketRef)
	if err != nil {
		t.Fatalf("GetTicketMentions: %v", err)
	}
	found := false
	for _, m := range mentions {
		if formatted, ferr := domain.Format(m); ferr == nil && formatted == doc.Ref {
			found = true
		}
	}
	if !found {
		t.Errorf("ticket mentions = %+v, want it to include %s", mentions, doc.Ref)
	}
}

// TestContentItemActivityAppearsInFeed proves the activity.go extension
// (eventContentItemCreated/eventContentItemUpdated + activityEntityRef's
// KindPlan/KindDocument case) actually resolves.
func TestContentItemActivityAppearsInFeed(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")
	item, err := s.CreateContentItem(ctx, CreateContentItemRequest{ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Plan"}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}

	result, err := s.ListActivity(ctx, "ABC", ActivityListFilters{}, 20, "")
	if err != nil {
		t.Fatalf("ListActivity: %v", err)
	}
	found := false
	for _, e := range result.Events {
		if e.EventType == eventContentItemCreated && e.EntityRef == item.Ref {
			found = true
		}
	}
	if !found {
		t.Errorf("activity events = %+v, want a content_item_created event for %s", result.Events, item.Ref)
	}
}
