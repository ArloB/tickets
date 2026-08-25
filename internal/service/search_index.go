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

// indexProjectSearchDoc indexes a project's own title/description
// (Phase 7 — plan.md §6.3 promises project text is searchable, and
// RebuildSearchIndex's original kind set omitted it). Ref is the
// project's key itself: a project has no seq-numbered public
// reference the way tickets/features/decisions do (product spec
// §5.2's table only defines one for those kinds), and its key already
// serves as its stable, human-facing identity.
func indexProjectSearchDoc(ctx context.Context, tx *sql.Tx, entityID int64, p domain.Project) error {
	if err := store.UpsertSearchDocument(ctx, tx, "entity", entityID, store.SearchDocumentFields{
		EntityID: entityID, Kind: "project", ProjectID: entityID,
		Ref: p.Key, Status: string(p.Status), Title: p.Title, Body: p.Title + "\n" + p.Description,
	}); err != nil {
		return fmt.Errorf("service: index project search document: %w", err)
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

// indexAttachmentSearchDoc upserts one attachment's search_documents
// row, keyed by the attachment's own id — never comment_id, even when
// the attachment is itself attached to a comment: comment_id on a
// search_documents row means "this hit *is* a comment"
// (toSearchHit/internal/httpapi/search.go renders it as "comment
// #N"), and an attachment is never that. ref (the owning principal
// entity's formatted reference, resolved by the caller — the
// attachment's target entity directly, or a comment-attached
// attachment's comment's *parent* entity, the same resolution
// attachmentAuditEntityID already does for its audit event) is the
// whole navigation story here, exactly like a comment hit's own ref.
// Title is the attachment's own title (always present); body folds in
// file_name/path_value so a search matches either — product spec
// §6.3's "attachment names."
func indexAttachmentSearchDoc(ctx context.Context, tx *sql.Tx, attachmentID, entityID, projectEntityID int64, ref string, a domain.Attachment) error {
	body := a.FileName
	if a.PathValue != "" {
		if body != "" {
			body += "\n"
		}
		body += a.PathValue
	}
	if err := store.UpsertSearchDocument(ctx, tx, "attachment", attachmentID, store.SearchDocumentFields{
		EntityID: entityID, Kind: "attachment", ProjectID: projectEntityID,
		Ref: ref, Title: a.Title, Body: body,
	}); err != nil {
		return fmt.Errorf("service: index attachment search document: %w", err)
	}
	return nil
}

// indexAttachmentOwnedByEntity resolves entityID's formatted ref and
// project entity id, then indexes the attachment — the shared
// resolve-then-index step both CreateAttachment and ReplaceAttachment
// need, entityID being whichever principal entity
// attachmentAuditEntityID already resolved (the attachment's own
// target, or a comment-attached attachment's comment's parent).
func indexAttachmentOwnedByEntity(ctx context.Context, tx *sql.Tx, attachmentID, entityID int64, a domain.Attachment) error {
	ref, err := mentionTargetRef(ctx, tx, entityID)
	if err != nil {
		return fmt.Errorf("service: resolve attachment owner ref for indexing: %w", err)
	}
	refStr, err := domain.Format(ref)
	if err != nil {
		return fmt.Errorf("service: format attachment owner ref for indexing: %w", err)
	}
	projectID, err := store.EntityProjectID(ctx, tx, entityID)
	if err != nil {
		return fmt.Errorf("service: resolve attachment owner project for indexing: %w", err)
	}
	return indexAttachmentSearchDoc(ctx, tx, attachmentID, entityID, projectID, refStr, a)
}

// indexLinkSearchDoc upserts one external link's search_documents
// row, keyed by the link's own id. Same comment_id-stays-nil reasoning
// as indexAttachmentSearchDoc. Body is the URL, so both halves of
// product spec §6.3's "link metadata" (title and URL) are searchable,
// not just the title.
func indexLinkSearchDoc(ctx context.Context, tx *sql.Tx, linkID, entityID, projectEntityID int64, ref, title, url string) error {
	if err := store.UpsertSearchDocument(ctx, tx, "link", linkID, store.SearchDocumentFields{
		EntityID: entityID, Kind: "link", ProjectID: projectEntityID,
		Ref: ref, Title: title, Body: url,
	}); err != nil {
		return fmt.Errorf("service: index link search document: %w", err)
	}
	return nil
}

// removeEntitySearchDocs clears an entity's own search row, its
// comments' search rows, and its attachments'/links' search rows
// together — see store.DeleteSearchDocumentsForEntity's doc for why
// each is included here rather than left to drift.
func removeEntitySearchDocs(ctx context.Context, tx *sql.Tx, entityID int64) error {
	if err := store.DeleteSearchDocumentsForEntity(ctx, tx, entityID); err != nil {
		return fmt.Errorf("service: remove entity search documents: %w", err)
	}
	return nil
}

// reindexCommentsForEntity re-upserts search_documents rows for every
// live comment on entityID, and — regardless of a given comment's own
// tombstone state — every one of that comment's still-live
// attachments. Called after RestoreTicket, since a ticket's
// soft-delete removed all of that (comments are never themselves
// soft-deleted by an entity delete, only excluded from the index
// while their ticket is gone; a comment-attached attachment's own
// row was removed by entity_id scope regardless of the comment's own
// deletion state — see removeEntitySearchDocs) but restore doesn't
// otherwise touch comments or their attachments. A tombstoned
// comment's attachments are reindexed here for the same reason
// DeleteSearchDocumentForComment's doc gives for not removing them on
// delete: the attachment record is still live and still reachable via
// GET /comments/{id}/attachments independent of its comment's state.
func reindexCommentsForEntity(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, ticketRef string) error {
	rows, err := store.ListCommentsForEntity(ctx, tx, entityID)
	if err != nil {
		return fmt.Errorf("service: list comments to reindex: %w", err)
	}
	for _, row := range rows {
		if row.Entity.DeletedAt == nil {
			if err := indexCommentSearchDoc(ctx, tx, row.Entity.ID, entityID, projectEntityID, ticketRef, row.Entity.Body); err != nil {
				return err
			}
		}
		attachments, err := store.ListAttachmentsForComment(ctx, tx, row.Entity.ID)
		if err != nil {
			return fmt.Errorf("service: list comment attachments to reindex: %w", err)
		}
		for _, a := range attachments {
			if err := indexAttachmentSearchDoc(ctx, tx, a.ID, entityID, projectEntityID, ticketRef, a.Entity); err != nil {
				return err
			}
		}
	}
	return nil
}

// reindexAttachmentsForEntity re-upserts search_documents rows for
// every live attachment directly owned by entityID (not a comment's —
// see reindexCommentsForEntity for those) — the same restore-time gap
// reindexCommentsForEntity fills, for attachments.
func reindexAttachmentsForEntity(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, ref string) error {
	rows, err := store.ListAttachmentsForEntity(ctx, tx, entityID)
	if err != nil {
		return fmt.Errorf("service: list attachments to reindex: %w", err)
	}
	for _, row := range rows {
		if err := indexAttachmentSearchDoc(ctx, tx, row.ID, entityID, projectEntityID, ref, row.Entity); err != nil {
			return err
		}
	}
	return nil
}

// reindexLinksForEntity re-upserts search_documents rows for every
// external link owned by entityID — same restore-time reasoning as
// reindexAttachmentsForEntity.
func reindexLinksForEntity(ctx context.Context, tx *sql.Tx, entityID, projectEntityID int64, ref string) error {
	rows, err := store.ListExternalLinks(ctx, tx, entityID)
	if err != nil {
		return fmt.Errorf("service: list external links to reindex: %w", err)
	}
	for _, row := range rows {
		if err := indexLinkSearchDoc(ctx, tx, row.ID, entityID, projectEntityID, ref, row.Title, row.URL); err != nil {
			return err
		}
	}
	return nil
}
