package store

import (
	"context"
	"fmt"
)

// SearchDocumentFields is UpsertSearchDocument's denormalized payload —
// see migrations/0011_search.sql for what each column means and why
// it's denormalized rather than resolved by a join at query time.
type SearchDocumentFields struct {
	EntityID  int64
	CommentID *int64
	Kind      string
	ProjectID int64
	Ref       string
	Status    string
	Title     string
	Body      string
}

// UpsertSearchDocument writes or refreshes one search_documents row,
// keyed by (sourceKind, sourceID) — 'entity'+entities.id for a
// ticket/feature/decision/plan/document, 'comment'+comments.id for a
// comment. Must run inside the same transaction as the source row's
// own write (the sqlite spike's proven external-content pattern):
// search_documents_ai/au's triggers keep search_fts in lockstep
// automatically, so the caller never touches search_fts directly.
func UpsertSearchDocument(ctx context.Context, q Querier, sourceKind string, sourceID int64, f SearchDocumentFields) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO search_documents(source_kind, source_id, entity_id, comment_id, kind, project_id, ref, status, title, body)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_kind, source_id) DO UPDATE SET
			entity_id = excluded.entity_id,
			comment_id = excluded.comment_id,
			kind = excluded.kind,
			project_id = excluded.project_id,
			ref = excluded.ref,
			status = excluded.status,
			title = excluded.title,
			body = excluded.body`,
		sourceKind, sourceID, f.EntityID, f.CommentID, f.Kind, f.ProjectID, f.Ref, nullableString(f.Status), f.Title, f.Body,
	)
	if err != nil {
		return fmt.Errorf("upsert search document: %w", err)
	}
	return nil
}

// nullableString maps "" to a SQL NULL — search_documents.status is
// genuinely absent (not just empty) for kinds with no status concept
// (plan/document/comment), and a filter matching an empty string
// shouldn't accidentally match those rows.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// DeleteSearchDocumentsForEntity removes an entity's own search row
// plus every comment search row scoped to it (entity_id, not
// source_id — see the migration's doc comment). Called from every
// soft-delete path, including DeleteFeature's cascade-delete-tickets
// loop: a cascade-deleted ticket's comments must leave the index too,
// or a deleted ticket's comments keep surfacing as search hits.
func DeleteSearchDocumentsForEntity(ctx context.Context, q Querier, entityID int64) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM search_documents WHERE source_kind = 'entity' AND source_id = ?`, entityID); err != nil {
		return fmt.Errorf("delete entity search document: %w", err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM search_documents WHERE source_kind = 'comment' AND entity_id = ?`, entityID); err != nil {
		return fmt.Errorf("delete comment search documents for entity: %w", err)
	}
	return nil
}

// DeleteSearchDocumentForComment removes one comment's search row —
// used by DeleteComment, which (unlike a ticket/feature) has no
// restore path, so the row is simply gone rather than filtered.
func DeleteSearchDocumentForComment(ctx context.Context, q Querier, commentID int64) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM search_documents WHERE source_kind = 'comment' AND source_id = ?`, commentID); err != nil {
		return fmt.Errorf("delete comment search document: %w", err)
	}
	return nil
}

// DeleteAllSearchDocuments clears the index — RebuildSearchIndex's
// first step (internal/store/search.go).
func DeleteAllSearchDocuments(ctx context.Context, q Querier) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM search_documents`); err != nil {
		return fmt.Errorf("clear search documents: %w", err)
	}
	return nil
}
