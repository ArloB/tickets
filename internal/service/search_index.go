package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// derefStr returns "" for a nil pointer — store.ContentItemFields'
// representation-specific columns are all *string (nullable columns),
// but indexContentItemSearchDoc only ever needs their zero-value-safe
// string form.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// indexTicketSearchDoc, indexFeatureSearchDoc, indexDecisionSearchDoc,
// and indexContentItemSearchDoc each upsert one entity's
// search_documents row from its already-loaded domain struct — called
// from every create/update path that changes a ticket/feature/
// decision/plan/document's indexed title or body text (docs/adr/0018).
// Deliberately not called from assign/move/reorder — none of those
// touch text search indexes on (title, body, status).
func indexTicketSearchDoc(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, t domain.Ticket) error {
	if err := store.UpsertSearchDocument(ctx, tx, "entity", entityID, store.SearchDocumentFields{
		EntityID: entityID, Kind: "ticket", ProjectID: projectEntityID,
		Ref: t.Ref, Status: string(t.Status), Title: t.Title, Body: t.Title + "\n" + t.Description,
	}); err != nil {
		return fmt.Errorf("service: index ticket search document: %w", err)
	}
	return nil
}

func indexFeatureSearchDoc(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, f domain.Feature) error {
	if err := store.UpsertSearchDocument(ctx, tx, "entity", entityID, store.SearchDocumentFields{
		EntityID: entityID, Kind: "feature", ProjectID: projectEntityID,
		Ref: f.Ref, Status: string(f.Status), Title: f.Title, Body: f.Title + "\n" + f.Description,
	}); err != nil {
		return fmt.Errorf("service: index feature search document: %w", err)
	}
	return nil
}

func indexDecisionSearchDoc(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, d domain.Decision) error {
	body := d.Context + "\n" + d.Decision + "\n" + d.Rationale + "\n" + d.Consequences
	if err := store.UpsertSearchDocument(ctx, tx, "entity", entityID, store.SearchDocumentFields{
		EntityID: entityID, Kind: "decision", ProjectID: projectEntityID,
		Ref: d.Ref, Status: string(d.Status), Title: d.Title, Body: body,
	}); err != nil {
		return fmt.Errorf("service: index decision search document: %w", err)
	}
	return nil
}

// contentItemSearchDocBody is the content-item counterpart of
// store.contentItemSearchBody — a markdown item indexes its body;
// the other three representations have no free text of their own, so
// their identifying value (file name/path/URL) is indexed instead,
// otherwise a file/path/url plan or document would never be findable
// by anything but its title.
func contentItemSearchDocBody(ci domain.ContentItem) string {
	switch domain.ContentRepresentation(ci.Representation) {
	case domain.ContentRepresentationFile:
		return ci.Title + "\n" + ci.FileName
	case domain.ContentRepresentationPath:
		return ci.Title + "\n" + ci.PathValue
	case domain.ContentRepresentationURL:
		return ci.Title + "\n" + ci.URLValue
	default:
		return ci.Title + "\n" + ci.Body
	}
}

func indexContentItemSearchDoc(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, ci domain.ContentItem) error {
	if err := store.UpsertSearchDocument(ctx, tx, "entity", entityID, store.SearchDocumentFields{
		EntityID: entityID, Kind: string(ci.Kind), ProjectID: projectEntityID,
		Ref: ci.Ref, Title: ci.Title, Body: contentItemSearchDocBody(ci),
	}); err != nil {
		return fmt.Errorf("service: index content item search document: %w", err)
	}
	return nil
}

// indexCommentSearchDoc upserts a comment's search_documents row.
// entityID/ticketRef/projectEntityID all name the owning ticket, not
// the comment itself — a comment hit renders as "found in <ticket>",
// the same shape Backlink already uses for comment-sourced mentions.
func indexCommentSearchDoc(ctx context.Context, tx *sql.Tx, commentID, entityID, projectEntityID int64, ticketRef, body string) error {
	cid := commentID
	if err := store.UpsertSearchDocument(ctx, tx, "comment", commentID, store.SearchDocumentFields{
		EntityID: entityID, CommentID: &cid, Kind: "comment", ProjectID: projectEntityID,
		Ref: ticketRef, Title: "", Body: body,
	}); err != nil {
		return fmt.Errorf("service: index comment search document: %w", err)
	}
	return nil
}

// removeEntitySearchDocs clears an entity's own search row and its
// comments' search rows together — see store.DeleteSearchDocumentsForEntity's
// doc for why comments are included here rather than left to drift.
func removeEntitySearchDocs(ctx context.Context, tx *sql.Tx, entityID int64) error {
	if err := store.DeleteSearchDocumentsForEntity(ctx, tx, entityID); err != nil {
		return fmt.Errorf("service: remove entity search documents: %w", err)
	}
	return nil
}

// reindexCommentsForEntity re-upserts search_documents rows for every
// live comment on entityID — called after RestoreTicket, since a
// ticket's soft-delete removed its comments' search rows (they are
// never themselves soft-deleted, only excluded from the index while
// their ticket is gone) but restore doesn't otherwise touch comments.
func reindexCommentsForEntity(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, ticketRef string) error {
	rows, err := store.ListCommentsForEntity(ctx, tx, entityID)
	if err != nil {
		return fmt.Errorf("service: list comments to reindex: %w", err)
	}
	for _, row := range rows {
		if row.Entity.DeletedAt != nil {
			continue
		}
		if err := indexCommentSearchDoc(ctx, tx, row.Entity.ID, entityID, projectEntityID, ticketRef, row.Entity.Body); err != nil {
			return err
		}
	}
	return nil
}
