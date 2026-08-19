package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
	f.title, f.description, f.status, f.priority, f.priority_rank, f.position`

func scanFeatureRow(scan func(dest ...any) error) (FeatureRow, error) {
	var (
		row                  FeatureRow
		u                    []byte
		createdAt, updatedAt string
		status, priority     string
		seq                  int64
	)
	err := scan(&row.ID, &u, &row.Entity.Version, &createdAt, &updatedAt,
		&row.Entity.ProjectKey, &row.ProjectEntityID, &seq,
		&row.Entity.Title, &row.Entity.Description, &status, &priority, &row.PriorityRank, &row.Position)
	if err != nil {
		return FeatureRow{}, err
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
// title/description/priority (ADR 0008's version-guard pattern via
// bumpEntityVersion). now is the caller's shared transaction
// timestamp (see Now).
func UpdateFeatureFields(ctx context.Context, q Querier, entityID int64, title, description, priority string, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE features SET title = ?, description = ?, priority = ?, priority_rank = ? WHERE id = ?`,
		title, description, priority, priorityRank(priority), entityID,
	); err != nil {
		return 0, fmt.Errorf("update feature fields: %w", err)
	}
	return newVersion, nil
}
