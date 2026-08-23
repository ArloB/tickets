package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/google/uuid"
)

// FeatureRow is the internal (store-only) view of a feature.
// ProjectEntityID/PriorityRank/Position are internal-only, the same
// boundary TicketRow already draws.
type FeatureRow struct {
	Entity          domain.Feature
	ID              int64
	ProjectEntityID int64
	PriorityRank    int64
	Position        int64
}

const featureSelectColumns = `
	e.id, e.uuid, e.version, e.created_at, e.updated_at,
	p.key, f.project_id, f.seq,
	f.title, f.description, f.status, f.priority, f.priority_rank, f.position, e.deleted_at,
	ca.kind, ca.name`

func scanFeatureRow(scan func(dest ...any) error) (FeatureRow, error) {
	var (
		row                      FeatureRow
		u                        []byte
		createdAt, updatedAt     string
		status, priority         string
		seq                      int64
		deletedAt                sql.NullString
		creatorKind, creatorName sql.NullString
	)
	err := scan(&row.ID, &u, &row.Entity.Version, &createdAt, &updatedAt,
		&row.Entity.ProjectKey, &row.ProjectEntityID, &seq,
		&row.Entity.Title, &row.Entity.Description, &status, &priority, &row.PriorityRank, &row.Position, &deletedAt,
		&creatorKind, &creatorName)
	if err != nil {
		return FeatureRow{}, err
	}
	if creatorKind.Valid {
		row.Entity.Creator = &domain.ActorRef{Kind: domain.ActorKind(creatorKind.String), Name: creatorName.String}
	}
	if deletedAt.Valid {
		t, err := parseTime(deletedAt.String)
		if err != nil {
			return FeatureRow{}, fmt.Errorf("parse feature deleted_at: %w", err)
		}
		row.Entity.DeletedAt = &t
	}
	parsed, err := uuid.FromBytes(u)
	if err != nil {
		return FeatureRow{}, fmt.Errorf("parse feature uuid: %w", err)
	}
	row.Entity.UUID = parsed.String()
	if row.Entity.CreatedAt, err = parseTime(createdAt); err != nil {
		return FeatureRow{}, fmt.Errorf("parse feature created_at: %w", err)
	}
	if row.Entity.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return FeatureRow{}, fmt.Errorf("parse feature updated_at: %w", err)
	}
	row.Entity.Status = domain.WorkflowStatus(status)
	row.Entity.Priority = domain.Priority(priority)

	ref, err := domain.Format(domain.Reference{ProjectKey: row.Entity.ProjectKey, Kind: domain.KindFeature, Seq: seq})
	if err != nil {
		return FeatureRow{}, fmt.Errorf("format feature ref: %w", err)
	}
	row.Entity.Ref = ref
	return row, nil
}

// GetFeatureByRef looks up a feature by its parsed reference, or
// returns ErrNotFound. Rejects a reference whose Kind isn't
// KindFeature, the same guard GetTicketByRef applies (queries.go).
func GetFeatureByRef(ctx context.Context, q Querier, ref domain.Reference) (FeatureRow, error) {
	if ref.Kind != domain.KindFeature {
		return FeatureRow{}, ErrNotFound
	}
	query := `SELECT` + featureSelectColumns + `
		FROM features f
		JOIN entities e ON e.id = f.id
		JOIN projects p ON p.key = ?
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE f.project_id = p.id AND f.seq = ? AND e.deleted_at IS NULL`
	row, err := scanFeatureRow(q.QueryRowContext(ctx, query, ref.ProjectKey, ref.Seq).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return FeatureRow{}, ErrNotFound
	}
	if err != nil {
		return FeatureRow{}, fmt.Errorf("get feature %s-F%d: %w", ref.ProjectKey, ref.Seq, err)
	}
	return row, nil
}

// GetFeatureByRefAnyDeletion is GetFeatureByRef without the
// deleted_at IS NULL filter — see GetTicketByRefAnyDeletion's doc;
// Restore needs this to find a soft-deleted feature at all.
func GetFeatureByRefAnyDeletion(ctx context.Context, q Querier, ref domain.Reference) (FeatureRow, error) {
	if ref.Kind != domain.KindFeature {
		return FeatureRow{}, ErrNotFound
	}
	query := `SELECT` + featureSelectColumns + `
		FROM features f
		JOIN entities e ON e.id = f.id
		JOIN projects p ON p.key = ?
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE f.project_id = p.id AND f.seq = ?`
	row, err := scanFeatureRow(q.QueryRowContext(ctx, query, ref.ProjectKey, ref.Seq).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return FeatureRow{}, ErrNotFound
	}
	if err != nil {
		return FeatureRow{}, fmt.Errorf("get feature %s-F%d (any deletion state): %w", ref.ProjectKey, ref.Seq, err)
	}
	return row, nil
}

// ListFeaturesForProject returns every non-deleted feature in a
// project, ordered by (priority_rank, position). Unpaginated: unlike
// tickets, a project's feature count is small and bounded (§5.4 calls
// features a short/medium-term grouping, not a bulk record type), so
// there's no caller yet that needs a cursor.
func ListFeaturesForProject(ctx context.Context, q Querier, projectEntityID int64) ([]FeatureRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT`+featureSelectColumns+`
		 FROM features f
		 JOIN entities e ON e.id = f.id
		 JOIN projects p ON p.id = f.project_id
		 LEFT JOIN actors ca ON ca.id = e.created_by
		 WHERE f.project_id = ? AND e.deleted_at IS NULL
		 ORDER BY f.priority_rank ASC, f.position ASC`,
		projectEntityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list features for project %d: %w", projectEntityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []FeatureRow
	for rows.Next() {
		row, err := scanFeatureRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan feature: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FeaturesPage is a cursor-paginated page of features, returned by
// ListFeaturesForProjectPage.
type FeaturesPage struct {
	Features   []FeatureRow
	NextCursor string
}

// scanFeatureRowsPage mirrors tickets_list.go's scanTicketRowsPage:
// fetch limit+1, keep the first limit, build a cursor from the last
// kept row — cursorFor never sees the limit+1'th overflow row, so the
// cursor never names something the client hasn't actually seen.
func scanFeatureRowsPage(rowsIter interface {
	Next() bool
	Err() error
	Scan(dest ...any) error
}, limit int, cursorFor func(FeatureRow) string) (FeaturesPage, error) {
	var rows []FeatureRow
	for rowsIter.Next() {
		row, err := scanFeatureRow(rowsIter.Scan)
		if err != nil {
			return FeaturesPage{}, fmt.Errorf("scan feature: %w", err)
		}
		rows = append(rows, row)
	}
	if err := rowsIter.Err(); err != nil {
		return FeaturesPage{}, err
	}

	page := FeaturesPage{Features: rows}
	if len(rows) > limit {
		page.Features = rows[:limit]
		page.NextCursor = cursorFor(page.Features[limit-1])
	}
	return page, nil
}

// ListFeaturesForProjectPage is ListFeaturesForProject's cursor-
// paginated counterpart (Phase 3 Step 5), ordered by (priority_rank,
// position, id) — id, not created_at, is the tiebreaker: unlike the
// ticket priority queue (tickets_list.go's 4-part (rank, position,
// created_at, id) cursor), a feature has no documented "then creation
// time" tiebreak requirement (product spec §5.6 states that for the
// ticket priority queue specifically), and e.id alone is already a
// strictly unique tiebreaker (SQLite's rowid-backed integer primary
// key) — no need for a second one. This also keeps the cursor's part
// count at 3, distinct from the 2-part simple-list cursor
// (DecodeCreatedAtIDCursor), the 4-part priority-queue cursor, and the
// 5-part issue-register cursor: store.DecodeCursor's only defense
// against a cursor from the wrong view is its part count, so reusing
// an already-taken length here would let a ticket priority-queue
// cursor be replayed against this endpoint without ever failing
// validation — wrong data, not a crash, and much harder to notice.
func ListFeaturesForProjectPage(ctx context.Context, q Querier, projectEntityID int64, limit int, afterRank, afterPosition, afterID int64) (FeaturesPage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT`+featureSelectColumns+`
		 FROM features f
		 JOIN entities e ON e.id = f.id
		 JOIN projects p ON p.id = f.project_id
		 LEFT JOIN actors ca ON ca.id = e.created_by
		 WHERE f.project_id = ? AND e.deleted_at IS NULL
		   AND (f.priority_rank, f.position, e.id) > (?, ?, ?)
		 ORDER BY f.priority_rank ASC, f.position ASC, e.id ASC
		 LIMIT ?`,
		projectEntityID, afterRank, afterPosition, afterID, limit+1,
	)
	if err != nil {
		return FeaturesPage{}, fmt.Errorf("list features for project %d (paginated): %w", projectEntityID, err)
	}
	defer func() { _ = rows.Close() }()

	return scanFeatureRowsPage(rows, limit, func(row FeatureRow) string {
		return EncodeCursor(
			strconv.FormatInt(row.PriorityRank, 10),
			strconv.FormatInt(row.Position, 10),
			strconv.FormatInt(row.ID, 10),
		)
	})
}

// GetFeatureRefByEntityID resolves a feature's public reference from
// its internal entity id, or ErrNotFound if missing/deleted/not a
// feature — GetFeatureByRef's reverse, mirroring
// GetTicketRefByEntityID (relationships.go). Used when a mention edge
// (which stores bare entity ids) needs to render its target on the
// wire.
func GetFeatureRefByEntityID(ctx context.Context, q Querier, entityID int64) (domain.Reference, error) {
	var (
		projectKey string
		seq        int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.key, f.seq
		 FROM features f
		 JOIN entities e ON e.id = f.id
		 JOIN projects p ON p.id = f.project_id
		 WHERE f.id = ? AND e.deleted_at IS NULL`,
		entityID,
	).Scan(&projectKey, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Reference{}, ErrNotFound
	}
	if err != nil {
		return domain.Reference{}, fmt.Errorf("get feature ref for entity %d: %w", entityID, err)
	}
	return domain.Reference{ProjectKey: projectKey, Kind: domain.KindFeature, Seq: seq}, nil
}

// UpdateFeatureFields applies a conditional update to a feature's
// title/description/priority/position (ADR 0008's version-guard
// pattern via bumpEntityVersion). position mirrors
// UpdateTicketFields' contract: the caller always supplies it
// explicitly, either unchanged or (on a priority change) the new
// group's tail position. now is the caller's shared transaction
// timestamp (see Now).
func UpdateFeatureFields(ctx context.Context, q Querier, entityID int64, title, description, priority string, position, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE features SET title = ?, description = ?, priority = ?, priority_rank = ?, position = ? WHERE id = ?`,
		title, description, priority, priorityRank(priority), position, entityID,
	); err != nil {
		return 0, fmt.Errorf("update feature fields: %w", err)
	}
	return newVersion, nil
}
