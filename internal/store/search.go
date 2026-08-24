package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
)

// maxSearchOffset caps how deep a search result page can page — bm25
// rank has no stable seekable key the way (created_at, id) does, so
// paging works by literal OFFSET; an unbounded offset would mean an
// O(n) table scan per page against the tail of a large result set.
// Product spec §5.12 treats search as "find it in the first page or
// two," not a browsable full listing, so a page beyond this is a
// validation error rather than silently truncated results.
const maxSearchOffset = 500

// EncodeSearchOffsetCursor and DecodeSearchOffsetCursor are search's
// own cursor shape — a plain offset, not the (created_at, id) tuple
// every other list endpoint uses (docs/contracts/list-filters.md).
// bm25 rank isn't a stable sort key to seek from, so offset pagination
// is the only option; capping it at maxSearchOffset keeps that
// O(offset) cost bounded.
func EncodeSearchOffsetCursor(offset int) string {
	if offset == 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func DecodeSearchOffsetCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode search cursor: %w", err)
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 || offset > maxSearchOffset {
		return 0, fmt.Errorf("invalid search cursor")
	}
	return offset, nil
}

// SearchFilters holds optional, AND-composed narrowing predicates for
// Search — ProjectID 0 and an empty Kinds/Status mean "no filter on
// that dimension," the same zero-value convention TicketFilters uses.
type SearchFilters struct {
	ProjectID int64
	Kinds     []string
	Status    string
}

// SearchHit is one ranked row of a search result page. CommentID is
// set only when the hit is a comment (Backlink's ref+comment_id
// shape); Ref always names the owning record either way, so a client
// can link to a hit without a second lookup.
type SearchHit struct {
	Kind      string
	Ref       string
	CommentID *int64
	Title     string
	Snippet   string
}

// SearchPage is Search's cursor-paginated result.
type SearchPage struct {
	Hits       []SearchHit
	NextCursor string
}

// Search runs ftsQuery (already sanitized — see domain.SanitizeFTSQuery)
// against search_fts, ranked by bm25 (more negative = more relevant),
// joined back to search_documents for the denormalized fields filters
// narrow on and a hit needs to render. limit/offset are the caller's
// already-validated page window; offset is capped at maxSearchOffset
// by DecodeSearchOffsetCursor before it ever reaches here.
func Search(ctx context.Context, q Querier, ftsQuery string, filters SearchFilters, limit, offset int) (SearchPage, error) {
	var b strings.Builder
	b.WriteString(`
		SELECT sd.kind, sd.ref, sd.comment_id, sd.title,
		       snippet(search_fts, 1, '', '', '…', 10) AS snippet
		FROM search_fts
		JOIN search_documents sd ON sd.id = search_fts.rowid
		WHERE search_fts MATCH ?`)
	args := []any{ftsQuery}
	if filters.ProjectID != 0 {
		b.WriteString(" AND sd.project_id = ?")
		args = append(args, filters.ProjectID)
	}
	if len(filters.Kinds) > 0 {
		placeholders := make([]string, len(filters.Kinds))
		for i, k := range filters.Kinds {
			placeholders[i] = "?"
			args = append(args, k)
		}
		b.WriteString(" AND sd.kind IN (" + strings.Join(placeholders, ",") + ")")
	}
	if filters.Status != "" {
		b.WriteString(" AND sd.status = ?")
		args = append(args, filters.Status)
	}
	b.WriteString(" ORDER BY bm25(search_fts) ASC LIMIT ? OFFSET ?")
	args = append(args, limit+1, offset)

	rows, err := q.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return SearchPage{}, fmt.Errorf("search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var (
			h         SearchHit
			commentID sql.NullInt64
		)
		if err := rows.Scan(&h.Kind, &h.Ref, &commentID, &h.Title, &h.Snippet); err != nil {
			return SearchPage{}, fmt.Errorf("scan search hit: %w", err)
		}
		if commentID.Valid {
			id := commentID.Int64
			h.CommentID = &id
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return SearchPage{}, fmt.Errorf("iterate search hits: %w", err)
	}

	nextCursor := ""
	if len(hits) > limit {
		hits = hits[:limit]
		nextOffset := offset + limit
		if nextOffset <= maxSearchOffset {
			nextCursor = EncodeSearchOffsetCursor(nextOffset)
		}
	}
	return SearchPage{Hits: hits, NextCursor: nextCursor}, nil
}

// rebuildOwnerRef is one live principal entity's formatted reference
// and owning project entity id, keyed by its own entity id — captured
// while RebuildSearchIndex's ticket/feature/decision/content-item
// passes already load exactly this, then reused by the
// attachment/link passes below instead of a second per-kind lookup.
// Only ever populated for entities that passed the "e.deleted_at IS
// NULL" filter each of those passes already applies, so an
// attachment/link whose owner isn't in this map has a deleted owner —
// skipping it there is how the owner's own deleted_at is honored
// without a repeated join.
type rebuildOwnerRef struct {
	Ref       string
	ProjectID int64
}

// RebuildSearchIndex clears search_documents and reindexes every
// non-deleted ticket/feature/decision/plan/document, every
// non-tombstoned comment, every non-deleted attachment (uploaded or
// path, whether on a principal entity or on a comment — even a
// tombstoned one; see store.DeleteSearchDocumentForComment's doc for
// why a comment's own soft-delete doesn't remove its attachments'
// search rows), and every external link, from scratch, in one
// transaction (atomicity — never a half-rebuilt index — outweighs
// lock duration for an offline admin command). It is the documented
// recovery path for anything the incremental UpsertSearchDocument
// call sites miss or get wrong.
func RebuildSearchIndex(ctx context.Context, q Querier) (int, error) {
	if err := DeleteAllSearchDocuments(ctx, q); err != nil {
		return 0, err
	}
	count := 0
	owners := make(map[int64]rebuildOwnerRef)

	tRows, err := q.QueryContext(ctx, `SELECT`+ticketSelectColumns+`
		FROM tickets t
		JOIN entities e ON e.id = t.id
		JOIN projects p ON p.id = t.project_id
		JOIN features f ON f.id = t.feature_id
		LEFT JOIN actors a ON a.id = t.assignee_id
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE e.deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("rebuild: list tickets: %w", err)
	}
	for tRows.Next() {
		row, err := scanTicketRow(tRows.Scan)
		if err != nil {
			_ = tRows.Close()
			return 0, fmt.Errorf("rebuild: scan ticket: %w", err)
		}
		body := row.Entity.Title + "\n" + row.Entity.Description
		if err := UpsertSearchDocument(ctx, q, "entity", row.ID, SearchDocumentFields{
			EntityID: row.ID, Kind: "ticket", ProjectID: row.ProjectEntityID,
			Ref: row.Entity.Ref, Status: string(row.Entity.Status), Title: row.Entity.Title, Body: body,
		}); err != nil {
			_ = tRows.Close()
			return 0, fmt.Errorf("rebuild: index ticket %s: %w", row.Entity.Ref, err)
		}
		owners[row.ID] = rebuildOwnerRef{Ref: row.Entity.Ref, ProjectID: row.ProjectEntityID}
		count++
	}
	tErr := tRows.Err()
	_ = tRows.Close()
	if tErr != nil {
		return 0, fmt.Errorf("rebuild: iterate tickets: %w", tErr)
	}

	fRows, err := q.QueryContext(ctx, `SELECT`+featureSelectColumns+`
		FROM features f
		JOIN entities e ON e.id = f.id
		JOIN projects p ON p.id = f.project_id
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE e.deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("rebuild: list features: %w", err)
	}
	for fRows.Next() {
		row, err := scanFeatureRow(fRows.Scan)
		if err != nil {
			_ = fRows.Close()
			return 0, fmt.Errorf("rebuild: scan feature: %w", err)
		}
		body := row.Entity.Title + "\n" + row.Entity.Description
		if err := UpsertSearchDocument(ctx, q, "entity", row.ID, SearchDocumentFields{
			EntityID: row.ID, Kind: "feature", ProjectID: row.ProjectEntityID,
			Ref: row.Entity.Ref, Status: string(row.Entity.Status), Title: row.Entity.Title, Body: body,
		}); err != nil {
			_ = fRows.Close()
			return 0, fmt.Errorf("rebuild: index feature %s: %w", row.Entity.Ref, err)
		}
		owners[row.ID] = rebuildOwnerRef{Ref: row.Entity.Ref, ProjectID: row.ProjectEntityID}
		count++
	}
	fErr := fRows.Err()
	_ = fRows.Close()
	if fErr != nil {
		return 0, fmt.Errorf("rebuild: iterate features: %w", fErr)
	}

	dRows, err := q.QueryContext(ctx, `SELECT`+decisionSelectColumns+`
		FROM decisions d
		JOIN entities e ON e.id = d.id
		JOIN projects p ON p.id = d.project_id
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE e.deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("rebuild: list decisions: %w", err)
	}
	for dRows.Next() {
		row, err := scanDecisionRow(dRows.Scan)
		if err != nil {
			_ = dRows.Close()
			return 0, fmt.Errorf("rebuild: scan decision: %w", err)
		}
		body := strings.Join([]string{row.Entity.Context, row.Entity.Decision, row.Entity.Rationale, row.Entity.Consequences}, "\n")
		if err := UpsertSearchDocument(ctx, q, "entity", row.ID, SearchDocumentFields{
			EntityID: row.ID, Kind: "decision", ProjectID: row.ProjectEntityID,
			Ref: row.Entity.Ref, Status: string(row.Entity.Status), Title: row.Entity.Title, Body: body,
		}); err != nil {
			_ = dRows.Close()
			return 0, fmt.Errorf("rebuild: index decision %s: %w", row.Entity.Ref, err)
		}
		owners[row.ID] = rebuildOwnerRef{Ref: row.Entity.Ref, ProjectID: row.ProjectEntityID}
		count++
	}
	dErr := dRows.Err()
	_ = dRows.Close()
	if dErr != nil {
		return 0, fmt.Errorf("rebuild: iterate decisions: %w", dErr)
	}

	ciRows, err := q.QueryContext(ctx, `SELECT`+contentItemSelectColumns+`
		FROM content_items ci
		JOIN entities e ON e.id = ci.id
		JOIN projects p ON p.id = ci.project_id
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE e.deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("rebuild: list content items: %w", err)
	}
	for ciRows.Next() {
		row, err := scanContentItemRow(ciRows.Scan)
		if err != nil {
			_ = ciRows.Close()
			return 0, fmt.Errorf("rebuild: scan content item: %w", err)
		}
		body := contentItemSearchBody(row)
		if err := UpsertSearchDocument(ctx, q, "entity", row.ID, SearchDocumentFields{
			EntityID: row.ID, Kind: string(row.Entity.Kind), ProjectID: row.ProjectEntityID,
			Ref: row.Entity.Ref, Title: row.Entity.Title, Body: body,
		}); err != nil {
			_ = ciRows.Close()
			return 0, fmt.Errorf("rebuild: index content item %s: %w", row.Entity.Ref, err)
		}
		owners[row.ID] = rebuildOwnerRef{Ref: row.Entity.Ref, ProjectID: row.ProjectEntityID}
		count++
	}
	ciErr := ciRows.Err()
	_ = ciRows.Close()
	if ciErr != nil {
		return 0, fmt.Errorf("rebuild: iterate content items: %w", ciErr)
	}

	cRows, err := q.QueryContext(ctx, `
		SELECT c.id, c.entity_id, c.body, p.key, t.seq, t.project_id
		FROM comments c
		JOIN tickets t ON t.id = c.entity_id
		JOIN entities e ON e.id = t.id
		JOIN projects p ON p.id = t.project_id
		WHERE c.deleted_at IS NULL AND e.deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("rebuild: list comments: %w", err)
	}
	for cRows.Next() {
		var (
			commentID, entityID, seq, projectEntityID int64
			body, projectKey                          string
		)
		if err := cRows.Scan(&commentID, &entityID, &body, &projectKey, &seq, &projectEntityID); err != nil {
			_ = cRows.Close()
			return 0, fmt.Errorf("rebuild: scan comment: %w", err)
		}
		ref, err := domain.Format(domain.Reference{ProjectKey: projectKey, Kind: domain.KindTicket, Seq: seq})
		if err != nil {
			_ = cRows.Close()
			return 0, fmt.Errorf("rebuild: format comment's ticket ref: %w", err)
		}
		cid := commentID
		if err := UpsertSearchDocument(ctx, q, "comment", commentID, SearchDocumentFields{
			EntityID: entityID, CommentID: &cid, Kind: "comment", ProjectID: projectEntityID,
			Ref: ref, Title: "", Body: body,
		}); err != nil {
			_ = cRows.Close()
			return 0, fmt.Errorf("rebuild: index comment %d: %w", commentID, err)
		}
		count++
	}
	cErr := cRows.Err()
	_ = cRows.Close()
	if cErr != nil {
		return 0, fmt.Errorf("rebuild: iterate comments: %w", cErr)
	}

	// LEFT JOIN comments c: a comment-attached attachment (a.entity_id
	// NULL) needs its comment's *parent* entity id — c.entity_id — to
	// resolve an owner the same way indexAttachmentSearchDoc's callers
	// do incrementally. Deliberately no c.deleted_at filter here (see
	// this function's own doc): a soft-deleted comment's attachments
	// stay indexed as long as the owning ticket itself is live.
	aRows, err := q.QueryContext(ctx, `
		SELECT a.id, a.entity_id, a.title, a.file_name, a.path_value, c.entity_id
		FROM attachments a
		LEFT JOIN comments c ON c.id = a.comment_id
		WHERE a.deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("rebuild: list attachments: %w", err)
	}
	for aRows.Next() {
		var (
			id                                        int64
			directOwnerEntityID, commentOwnerEntityID sql.NullInt64
			title, fileName, pathValue                sql.NullString
		)
		if err := aRows.Scan(&id, &directOwnerEntityID, &title, &fileName, &pathValue, &commentOwnerEntityID); err != nil {
			_ = aRows.Close()
			return 0, fmt.Errorf("rebuild: scan attachment: %w", err)
		}
		ownerEntityID := directOwnerEntityID.Int64
		if !directOwnerEntityID.Valid {
			ownerEntityID = commentOwnerEntityID.Int64
		}
		owner, ok := owners[ownerEntityID]
		if !ok {
			continue // owner soft-deleted — see this function's doc
		}
		body := fileName.String
		if pathValue.String != "" {
			if body != "" {
				body += "\n"
			}
			body += pathValue.String
		}
		if err := UpsertSearchDocument(ctx, q, "attachment", id, SearchDocumentFields{
			EntityID: ownerEntityID, Kind: "attachment", ProjectID: owner.ProjectID,
			Ref: owner.Ref, Title: title.String, Body: body,
		}); err != nil {
			_ = aRows.Close()
			return 0, fmt.Errorf("rebuild: index attachment %d: %w", id, err)
		}
		count++
	}
	aErr := aRows.Err()
	_ = aRows.Close()
	if aErr != nil {
		return 0, fmt.Errorf("rebuild: iterate attachments: %w", aErr)
	}

	lRows, err := q.QueryContext(ctx, `SELECT id, entity_id, title, url FROM external_links`)
	if err != nil {
		return 0, fmt.Errorf("rebuild: list external links: %w", err)
	}
	for lRows.Next() {
		var (
			id, ownerEntityID int64
			title, url        string
		)
		if err := lRows.Scan(&id, &ownerEntityID, &title, &url); err != nil {
			_ = lRows.Close()
			return 0, fmt.Errorf("rebuild: scan external link: %w", err)
		}
		owner, ok := owners[ownerEntityID]
		if !ok {
			continue // owner soft-deleted
		}
		if err := UpsertSearchDocument(ctx, q, "link", id, SearchDocumentFields{
			EntityID: ownerEntityID, Kind: "link", ProjectID: owner.ProjectID,
			Ref: owner.Ref, Title: title, Body: url,
		}); err != nil {
			_ = lRows.Close()
			return 0, fmt.Errorf("rebuild: index external link %d: %w", id, err)
		}
		count++
	}
	lErr := lRows.Err()
	_ = lRows.Close()
	if lErr != nil {
		return 0, fmt.Errorf("rebuild: iterate external links: %w", lErr)
	}

	return count, nil
}

// contentItemSearchBody composes the searchable text for a plan/
// document — the Markdown body for a markdown representation, or the
// representation's own identifying text (file name, path, URL) for
// the other three, which otherwise have no free text at all to index.
func contentItemSearchBody(row ContentItemRow) string {
	switch row.Entity.Representation {
	case "markdown":
		return row.Entity.Title + "\n" + row.Entity.Body
	case "file":
		return row.Entity.Title + "\n" + row.Entity.FileName
	case "path":
		return row.Entity.Title + "\n" + row.Entity.PathValue
	case "url":
		return row.Entity.Title + "\n" + row.Entity.URLValue
	default:
		return row.Entity.Title
	}
}
