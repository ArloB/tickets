package service

import (
	"context"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// TestReadPathsSoftDeleteFiltering is the Phase 1 plan's Risks-table
// mitigation for "a read path forgets its deleted_at filter": a
// single checklist of every exported internal/store read function
// touched by Phase 1, with an explicit hidesDeleted intent, so the
// next one added has to make the same decision on purpose instead of
// by accident. Functions that already have a dedicated regression
// test elsewhere are listed here for completeness but not
// re-exercised — this file adds coverage only for the ones that had
// none.
//
// Add a row here (and, if untested elsewhere, a case) for every new
// exported read function this package's tables gain.
//
// Step 9 added a creator join (LEFT JOIN actors ca ON ca.id =
// e.created_by) to every row below marked hidesDeleted=yes except
// ListAuditEvents. It must stay a LEFT JOIN, never an INNER JOIN: rows
// created before Phase 2's identity work backfilled entities.created_by
// to the system actor via migration 0002_core_domain.sql, but the
// column is still schema-nullable (queries.go's InsertEntity doc
// explains why), so an INNER JOIN would silently drop any pre-Phase-2
// row from every one of these read paths instead of just returning a
// nil Creator for it.
//
//	Function                         hidesDeleted  Covered by
//	GetProjectByKey                  yes           (query filters; no dedicated test — low risk, single-row lookup mirrors GetTicketByRef)
//	ListProjects                     yes           this file
//	GetTicketByRef                   yes           soft_delete_test.go (TestDeleteTicketVanishesFromReadPaths)
//	GetTicketByRefAnyDeletion        NO (by design) soft_delete_test.go (restore tests)
//	PriorityQueue                    yes           this file
//	IssueRegister                    yes           (same WHERE clause as PriorityQueue; not independently re-tested)
//	GetFeatureByRef                  yes           soft_delete_test.go (TestDeleteFeatureCascadeDeletesTicketsToo)
//	GetFeatureByRefAnyDeletion       NO (by design) soft_delete_test.go (restore tests)
//	ListFeaturesForProject           yes           this file
//	ListRelationshipsForEntity       yes           relationship_test.go (TestGetTicketRelationshipsHidesSoftDeletedEndpoint)
//	ListAssociatedEntityIDs          yes           association_test.go (TestGetAssociationsHidesSoftDeletedEndpoint)
//	ListMentionTargetsFromSource     yes           this file
//	GetComment                       NO (by design) comment_test.go (TestDeleteCommentTombstoneStaysVisible)
//	ListCommentsForEntity            NO (by design) comment_test.go (TestDeleteCommentTombstoneStaysVisible)
//	ListCommentVersions              NO (by design) — history predates any deletion state, nothing to hide
//	ListAuditEvents                  NO (by design) this file — the trail must survive deletion (§5.12: append-only)
func TestReadPathsSoftDeleteFiltering(t *testing.T) {
	t.Run("ListProjects hides a deleted project's tickets' project — n/a, projects have no delete path in Phase 1", func(t *testing.T) {
		// Projects aren't soft-deletable in Phase 1 (no DeleteProject
		// method exists) — nothing to test here yet. Left as an explicit
		// no-op case rather than an absent row, so the table above stays
		// the single source of truth for every function it names.
	})

	t.Run("PriorityQueue hides a deleted ticket", func(t *testing.T) {
		ctx := context.Background()
		s := newTestService(t)
		mustCreateProject(t, s, "ABC")
		ticket := mustCreateTicket(t, s, "ABC", "T")
		ref, err := domain.Parse(ticket.Ref)
		if err != nil {
			t.Fatalf("parse ref: %v", err)
		}
		if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
			t.Fatalf("DeleteTicket: %v", err)
		}

		proj, err := store.GetProjectByKey(ctx, s.store.DB(), "ABC")
		if err != nil {
			t.Fatalf("GetProjectByKey: %v", err)
		}
		page, err := store.PriorityQueue(ctx, s.store.DB(), proj.ID, store.TicketFilters{}, 100, 0, 0, "", 0)
		if err != nil {
			t.Fatalf("PriorityQueue: %v", err)
		}
		for _, tk := range page.Tickets {
			if tk.Entity.Ref == ticket.Ref {
				t.Fatalf("PriorityQueue still includes deleted ticket %q", ticket.Ref)
			}
		}
	})

	t.Run("ListFeaturesForProject hides a deleted feature", func(t *testing.T) {
		ctx := context.Background()
		s := newTestService(t)
		mustCreateProject(t, s, "ABC")
		feature, err := s.CreateFeature(ctx, CreateFeatureRequest{ProjectKey: "ABC", Title: "Payments", Priority: domain.PriorityMedium}, testActor, testCorrelationID)
		if err != nil {
			t.Fatalf("CreateFeature: %v", err)
		}
		featureRef, err := domain.Parse(feature.Ref)
		if err != nil {
			t.Fatalf("parse ref: %v", err)
		}
		if _, err := s.DeleteFeature(ctx, DeleteFeatureRequest{Ref: featureRef, ExpectedVersion: feature.Version}, testActor, testCorrelationID); err != nil {
			t.Fatalf("DeleteFeature: %v", err)
		}

		result, err := s.ListFeatures(ctx, "ABC", 0, "")
		if err != nil {
			t.Fatalf("ListFeatures: %v", err)
		}
		for _, f := range result.Features {
			if f.Ref == feature.Ref {
				t.Fatalf("ListFeatures still includes deleted feature %q", feature.Ref)
			}
		}
	})

	t.Run("ListMentionTargetsFromSource hides a deleted mention target", func(t *testing.T) {
		ctx := context.Background()
		s := newTestService(t)
		mustCreateProject(t, s, "ABC")
		target := mustCreateTicket(t, s, "ABC", "Target")
		targetRef, err := domain.Parse(target.Ref)
		if err != nil {
			t.Fatalf("parse target ref: %v", err)
		}
		host, err := s.CreateTicket(ctx, CreateTicketRequest{
			ProjectKey: "ABC", Type: domain.TicketTypeTask, Title: "Host", Description: "mentions " + target.Ref,
		}, testActor, testCorrelationID, "", "")
		if err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		hostRef, err := domain.Parse(host.Ref)
		if err != nil {
			t.Fatalf("parse host ref: %v", err)
		}
		if mentions, err := s.GetTicketMentions(ctx, hostRef); err != nil || len(mentions) != 1 {
			t.Fatalf("mentions before delete = %+v, err=%v, want exactly one", mentions, err)
		}

		if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: targetRef, ExpectedVersion: target.Version}, testActor, testCorrelationID); err != nil {
			t.Fatalf("DeleteTicket(target): %v", err)
		}

		mentions, err := s.GetTicketMentions(ctx, hostRef)
		if err != nil {
			t.Fatalf("GetTicketMentions after target deleted: %v", err)
		}
		if len(mentions) != 0 {
			t.Fatalf("mentions after target deleted = %+v, want none", mentions)
		}
	})

	t.Run("ListAuditEvents does not hide a deleted ticket's trail", func(t *testing.T) {
		ctx := context.Background()
		s := newTestService(t)
		mustCreateProject(t, s, "ABC")
		ticket := mustCreateTicket(t, s, "ABC", "T")
		ref, err := domain.Parse(ticket.Ref)
		if err != nil {
			t.Fatalf("parse ref: %v", err)
		}
		if _, err := s.DeleteTicket(ctx, DeleteTicketRequest{Ref: ref, ExpectedVersion: ticket.Version}, testActor, testCorrelationID); err != nil {
			t.Fatalf("DeleteTicket: %v", err)
		}

		entityID := mustEntityIDByUUID(t, s, ticket.UUID)
		events, err := store.ListAuditEvents(ctx, s.store.DB(), entityID)
		if err != nil {
			t.Fatalf("ListAuditEvents on a deleted ticket: %v", err)
		}
		if len(events) != 2 { // ticket_created, ticket_deleted
			t.Fatalf("audit trail for deleted ticket = %+v, want 2 events (created + deleted) still visible", events)
		}
	})
}
