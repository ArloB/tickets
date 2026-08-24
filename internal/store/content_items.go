package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/google/uuid"
)

// ContentItemRow is the internal (store-only) view of a plan or
// document — mirrors DecisionRow's shape/doc. ProjectEntityID is
// internal-only, the same boundary DecisionRow draws.
type ContentItemRow struct {
	Entity          domain.ContentItem
	ID              int64
	ProjectEntityID int64
}

const contentItemSelectColumns = `
	e.id, e.uuid, e.version, e.created_at, e.updated_at, e.kind,
	p.key, ci.project_id, ci.seq,
	ci.title, ci.representation, ci.body, e.deleted_at,
	ca.kind, ca.name`

func scanContentItemRow(scan func(dest ...any) error) (ContentItemRow, error) {
	var (
		row                      ContentItemRow
		u                        []byte
		createdAt, updatedAt     string
		kind                     string
		seq                      int64
		deletedAt                sql.NullString
		creatorKind, creatorName sql.NullString
	)
	err := scan(&row.ID, &u, &row.Entity.Version, &createdAt, &updatedAt, &kind,
		&row.Entity.ProjectKey, &row.ProjectEntityID, &seq,
		&row.Entity.Title, &row.Entity.Representation, &row.Entity.Body, &deletedAt,
		&creatorKind, &creatorName)
	if err != nil {
		return ContentItemRow{}, err
	}
	row.Entity.Kind = domain.EntityKind(kind)
	if creatorKind.Valid {
		row.Entity.Creator = &domain.ActorRef{Kind: domain.ActorKind(creatorKind.String), Name: creatorName.String}
	}
	if deletedAt.Valid {
		t, err := parseTime(deletedAt.String)
		if err != nil {
			return ContentItemRow{}, fmt.Errorf("parse content item deleted_at: %w", err)
		}
		row.Entity.DeletedAt = &t
	}
	parsed, err := uuid.FromBytes(u)
	if err != nil {
		return ContentItemRow{}, fmt.Errorf("parse content item uuid: %w", err)
	}
	row.Entity.UUID = parsed.String()
	if row.Entity.CreatedAt, err = parseTime(createdAt); err != nil {
		return ContentItemRow{}, fmt.Errorf("parse content item created_at: %w", err)
	}
	if row.Entity.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ContentItemRow{}, fmt.Errorf("parse content item updated_at: %w", err)
	}

	ref, err := domain.Format(domain.Reference{ProjectKey: row.Entity.ProjectKey, Kind: row.Entity.Kind, Seq: seq})
	if err != nil {
		return ContentItemRow{}, fmt.Errorf("format content item ref: %w", err)
	}
	row.Entity.Ref = ref
	return row, nil
}

// GetContentItemByRef looks up a plan or document by its parsed
// reference, or returns ErrNotFound. Rejects a reference whose Kind
// isn't KindPlan or KindDocument.
func GetContentItemByRef(ctx context.Context, q Querier, ref domain.Reference) (ContentItemRow, error) {
	if ref.Kind != domain.KindPlan && ref.Kind != domain.KindDocument {
		return ContentItemRow{}, ErrNotFound
	}
	query := `SELECT` + contentItemSelectColumns + `
		FROM content_items ci
		JOIN entities e ON e.id = ci.id
		JOIN projects p ON p.key = ?
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE ci.project_id = p.id AND ci.seq = ? AND ci.kind = ? AND e.deleted_at IS NULL`
	row, err := scanContentItemRow(q.QueryRowContext(ctx, query, ref.ProjectKey, ref.Seq, string(ref.Kind)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return ContentItemRow{}, ErrNotFound
	}
	if err != nil {
		refStr, _ := domain.Format(ref)
		return ContentItemRow{}, fmt.Errorf("get content item %s: %w", refStr, err)
	}
	return row, nil
}

// ContentItemsPage is a cursor-paginated page of content items,
// returned by ListContentItemsForProjectPage.
type ContentItemsPage struct {
	Items      []ContentItemRow
	NextCursor string
}

// ListContentItemsForProjectPage returns a project's non-deleted plans
// or documents (kind selects which), cursor-paginated by
// (created_at, id) — content items have no priority/position (§5.9),
// mirroring ListDecisionsForProjectPage.
func ListContentItemsForProjectPage(ctx context.Context, q Querier, projectEntityID int64, kind domain.EntityKind, limit int, afterCreatedAt string, afterID int64) (ContentItemsPage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT`+contentItemSelectColumns+`
		 FROM content_items ci
		 JOIN entities e ON e.id = ci.id
		 JOIN projects p ON p.id = ci.project_id
		 LEFT JOIN actors ca ON ca.id = e.created_by
		 WHERE ci.project_id = ? AND ci.kind = ? AND e.deleted_at IS NULL
		   AND (e.created_at, e.id) > (?, ?)
		 ORDER BY e.created_at ASC, e.id ASC
		 LIMIT ?`,
		projectEntityID, string(kind), afterCreatedAt, afterID, limit+1,
	)
	if err != nil {
		return ContentItemsPage{}, fmt.Errorf("list content items for project %d: %w", projectEntityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ContentItemRow
	for rows.Next() {
		row, err := scanContentItemRow(rows.Scan)
		if err != nil {
			return ContentItemsPage{}, fmt.Errorf("scan content item: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return ContentItemsPage{}, err
	}

	page := ContentItemsPage{Items: out}
	if len(out) > limit {
		page.Items = out[:limit]
		last := page.Items[limit-1]
		page.NextCursor = EncodeCreatedAtIDCursor(last.Entity.CreatedAt.Format(TimeLayout), last.ID)
	}
	return page, nil
}

// GetContentItemRefByEntityID resolves a plan/document's public
// reference from its internal entity id, or ErrNotFound — mirrors
// GetDecisionRefByEntityID. Used when a mention/association edge (bare
// entity ids) needs to render its target on the wire.
func GetContentItemRefByEntityID(ctx context.Context, q Querier, entityID int64) (domain.Reference, error) {
	var (
		projectKey string
		kind       string
		seq        int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.key, e.kind, ci.seq
		 FROM content_items ci
		 JOIN entities e ON e.id = ci.id
		 JOIN projects p ON p.id = ci.project_id
		 WHERE ci.id = ? AND e.deleted_at IS NULL`,
		entityID,
	).Scan(&projectKey, &kind, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Reference{}, ErrNotFound
	}
	if err != nil {
		return domain.Reference{}, fmt.Errorf("get content item ref for entity %d: %w", entityID, err)
	}
	return domain.Reference{ProjectKey: projectKey, Kind: domain.EntityKind(kind), Seq: seq}, nil
}

// GetContentItemRefByEntityIDAnyDeletion is GetContentItemRefByEntityID
// without the deleted_at IS NULL filter — see
// GetTicketRefByEntityIDAnyDeletion's doc for why the activity feed
// needs this. Content items have no soft-delete surface yet, so this is
// currently equivalent to GetContentItemRefByEntityID; it exists for
// symmetry with the ticket/feature/decision variants.
func GetContentItemRefByEntityIDAnyDeletion(ctx context.Context, q Querier, entityID int64) (domain.Reference, error) {
	var (
		projectKey string
		kind       string
		seq        int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.key, e.kind, ci.seq
		 FROM content_items ci
		 JOIN entities e ON e.id = ci.id
		 JOIN projects p ON p.id = ci.project_id
		 WHERE ci.id = ?`,
		entityID,
	).Scan(&projectKey, &kind, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Reference{}, ErrNotFound
	}
	if err != nil {
		return domain.Reference{}, fmt.Errorf("get content item ref (any deletion) for entity %d: %w", entityID, err)
	}
	return domain.Reference{ProjectKey: projectKey, Kind: domain.EntityKind(kind), Seq: seq}, nil
}

// InsertContentItem creates a content_items row. Called inside the
// same transaction as InsertEntity/AllocateReference, mirroring
// InsertDecision's contract. kind must equal the value InsertEntity was
// just called with — see content_items' migration comment on why this
// table denormalizes kind. Step 3 always passes representation
// "markdown"; Steps 4-5 pass the other representation-specific values
// (not yet parameters here — added when those steps extend this
// function).
func InsertContentItem(ctx context.Context, q Querier, entityID, projectEntityID int64, kind domain.EntityKind, seq int64, title, representation, body string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO content_items (id, project_id, kind, seq, title, representation, body) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entityID, projectEntityID, string(kind), seq, title, representation, body,
	); err != nil {
		return fmt.Errorf("insert content item: %w", err)
	}
	return nil
}

// UpdateContentItemFields applies a conditional field update (ADR
// 0008's version-guard pattern via bumpEntityVersion) — mirrors
// UpdateDecisionFields. The archived-version snapshot is a separate
// InsertContentItemVersion call the caller makes first, against the
// pre-update row it already read.
func UpdateContentItemFields(ctx context.Context, q Querier, entityID int64, title, body string, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE content_items SET title = ?, body = ? WHERE id = ?`,
		title, body, entityID,
	); err != nil {
		return 0, fmt.Errorf("update content item fields: %w", err)
	}
	return newVersion, nil
}

// InsertContentItemVersion archives one content item's pre-update state
// — called with the row's fields and version *before*
// UpdateContentItemFields overwrites them, mirroring
// InsertDecisionVersion's ordering.
func InsertContentItemVersion(ctx context.Context, q Querier, contentItemID, version int64, representation, title, body string, editedBy int64, now string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO content_versions(content_item_id, version, representation, title, body, edited_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		contentItemID, version, representation, title, body, editedBy, now,
	); err != nil {
		return fmt.Errorf("insert content item version: %w", err)
	}
	return nil
}

// ListContentItemVersions returns a content item's archived prior
// states, oldest first — mirrors ListDecisionVersions.
func ListContentItemVersions(ctx context.Context, q Querier, contentItemID int64) ([]domain.ContentItemVersion, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT cv.version, cv.representation, cv.title, cv.body, a.kind, a.name, cv.created_at
		 FROM content_versions cv JOIN actors a ON a.id = cv.edited_by
		 WHERE cv.content_item_id = ?
		 ORDER BY cv.version ASC`,
		contentItemID,
	)
	if err != nil {
		return nil, fmt.Errorf("list content item versions for %d: %w", contentItemID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.ContentItemVersion
	for rows.Next() {
		var (
			v                          domain.ContentItemVersion
			body                       sql.NullString
			editedByKind, editedByName string
			createdAt                  string
		)
		if err := rows.Scan(&v.Version, &v.Representation, &v.Title, &body, &editedByKind, &editedByName, &createdAt); err != nil {
			return nil, fmt.Errorf("scan content item version: %w", err)
		}
		v.Body = body.String
		v.EditedBy = domain.ActorRef{Kind: domain.ActorKind(editedByKind), Name: editedByName}
		if v.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("parse content item version created_at: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetContentItemVersion returns one archived version by number, or
// ErrNotFound — mirrors GetDecisionVersion.
func GetContentItemVersion(ctx context.Context, q Querier, contentItemID, version int64) (domain.ContentItemVersion, error) {
	var (
		v                          domain.ContentItemVersion
		body                       sql.NullString
		editedByKind, editedByName string
		createdAt                  string
	)
	err := q.QueryRowContext(ctx,
		`SELECT cv.version, cv.representation, cv.title, cv.body, a.kind, a.name, cv.created_at
		 FROM content_versions cv JOIN actors a ON a.id = cv.edited_by
		 WHERE cv.content_item_id = ? AND cv.version = ?`,
		contentItemID, version,
	).Scan(&v.Version, &v.Representation, &v.Title, &body, &editedByKind, &editedByName, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ContentItemVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ContentItemVersion{}, fmt.Errorf("get content item version %d/%d: %w", contentItemID, version, err)
	}
	v.Body = body.String
	v.EditedBy = domain.ActorRef{Kind: domain.ActorKind(editedByKind), Name: editedByName}
	if v.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.ContentItemVersion{}, fmt.Errorf("parse content item version created_at: %w", err)
	}
	return v, nil
}
