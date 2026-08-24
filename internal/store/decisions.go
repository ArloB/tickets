package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/google/uuid"
)

// DecisionRow is the internal (store-only) view of a decision.
// ProjectEntityID is internal-only, the same boundary FeatureRow/
// TicketRow already draw.
type DecisionRow struct {
	Entity          domain.Decision
	ID              int64
	ProjectEntityID int64
}

const decisionSelectColumns = `
	e.id, e.uuid, e.version, e.created_at, e.updated_at,
	p.key, d.project_id, d.seq,
	d.title, d.context, d.decision, d.rationale, d.status, e.deleted_at,
	ca.kind, ca.name`

func scanDecisionRow(scan func(dest ...any) error) (DecisionRow, error) {
	var (
		row                      DecisionRow
		u                        []byte
		createdAt, updatedAt     string
		status                   string
		seq                      int64
		deletedAt                sql.NullString
		creatorKind, creatorName sql.NullString
	)
	err := scan(&row.ID, &u, &row.Entity.Version, &createdAt, &updatedAt,
		&row.Entity.ProjectKey, &row.ProjectEntityID, &seq,
		&row.Entity.Title, &row.Entity.Context, &row.Entity.Decision, &row.Entity.Rationale, &status, &deletedAt,
		&creatorKind, &creatorName)
	if err != nil {
		return DecisionRow{}, err
	}
	if creatorKind.Valid {
		row.Entity.Creator = &domain.ActorRef{Kind: domain.ActorKind(creatorKind.String), Name: creatorName.String}
	}
	if deletedAt.Valid {
		t, err := parseTime(deletedAt.String)
		if err != nil {
			return DecisionRow{}, fmt.Errorf("parse decision deleted_at: %w", err)
		}
		row.Entity.DeletedAt = &t
	}
	parsed, err := uuid.FromBytes(u)
	if err != nil {
		return DecisionRow{}, fmt.Errorf("parse decision uuid: %w", err)
	}
	row.Entity.UUID = parsed.String()
	if row.Entity.CreatedAt, err = parseTime(createdAt); err != nil {
		return DecisionRow{}, fmt.Errorf("parse decision created_at: %w", err)
	}
	if row.Entity.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return DecisionRow{}, fmt.Errorf("parse decision updated_at: %w", err)
	}
	row.Entity.Status = domain.DecisionStatus(status)

	ref, err := domain.Format(domain.Reference{ProjectKey: row.Entity.ProjectKey, Kind: domain.KindDecision, Seq: seq})
	if err != nil {
		return DecisionRow{}, fmt.Errorf("format decision ref: %w", err)
	}
	row.Entity.Ref = ref
	return row, nil
}

// GetDecisionByRef looks up a decision by its parsed reference, or
// returns ErrNotFound. Rejects a reference whose Kind isn't
// KindDecision, the same guard GetFeatureByRef/GetTicketByRef apply.
func GetDecisionByRef(ctx context.Context, q Querier, ref domain.Reference) (DecisionRow, error) {
	if ref.Kind != domain.KindDecision {
		return DecisionRow{}, ErrNotFound
	}
	query := `SELECT` + decisionSelectColumns + `
		FROM decisions d
		JOIN entities e ON e.id = d.id
		JOIN projects p ON p.key = ?
		LEFT JOIN actors ca ON ca.id = e.created_by
		WHERE d.project_id = p.id AND d.seq = ? AND e.deleted_at IS NULL`
	row, err := scanDecisionRow(q.QueryRowContext(ctx, query, ref.ProjectKey, ref.Seq).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return DecisionRow{}, ErrNotFound
	}
	if err != nil {
		return DecisionRow{}, fmt.Errorf("get decision %s-D%d: %w", ref.ProjectKey, ref.Seq, err)
	}
	return row, nil
}

// DecisionsPage is a cursor-paginated page of decisions, returned by
// ListDecisionsForProjectPage.
type DecisionsPage struct {
	Decisions  []DecisionRow
	NextCursor string
}

// ListDecisionsForProjectPage returns a project's non-deleted
// decisions, cursor-paginated by (created_at, id) — decisions have no
// priority/position (§5.8), so this is the same simple ordering
// store.ListProjects uses, not a priority-queue-style one.
func ListDecisionsForProjectPage(ctx context.Context, q Querier, projectEntityID int64, limit int, afterCreatedAt string, afterID int64) (DecisionsPage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT`+decisionSelectColumns+`
		 FROM decisions d
		 JOIN entities e ON e.id = d.id
		 JOIN projects p ON p.id = d.project_id
		 LEFT JOIN actors ca ON ca.id = e.created_by
		 WHERE d.project_id = ? AND e.deleted_at IS NULL
		   AND (e.created_at, e.id) > (?, ?)
		 ORDER BY e.created_at ASC, e.id ASC
		 LIMIT ?`,
		projectEntityID, afterCreatedAt, afterID, limit+1,
	)
	if err != nil {
		return DecisionsPage{}, fmt.Errorf("list decisions for project %d: %w", projectEntityID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []DecisionRow
	for rows.Next() {
		row, err := scanDecisionRow(rows.Scan)
		if err != nil {
			return DecisionsPage{}, fmt.Errorf("scan decision: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return DecisionsPage{}, err
	}

	page := DecisionsPage{Decisions: out}
	if len(out) > limit {
		page.Decisions = out[:limit]
		last := page.Decisions[limit-1]
		page.NextCursor = EncodeCreatedAtIDCursor(last.Entity.CreatedAt.Format(TimeLayout), last.ID)
	}
	return page, nil
}

// GetDecisionRefByEntityID resolves a decision's public reference from
// its internal entity id, or ErrNotFound if missing/deleted/not a
// decision — GetDecisionByRef's reverse, mirroring
// GetFeatureRefByEntityID. Used when a mention edge (which stores bare
// entity ids) needs to render its target on the wire.
func GetDecisionRefByEntityID(ctx context.Context, q Querier, entityID int64) (domain.Reference, error) {
	var (
		projectKey string
		seq        int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.key, d.seq
		 FROM decisions d
		 JOIN entities e ON e.id = d.id
		 JOIN projects p ON p.id = d.project_id
		 WHERE d.id = ? AND e.deleted_at IS NULL`,
		entityID,
	).Scan(&projectKey, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Reference{}, ErrNotFound
	}
	if err != nil {
		return domain.Reference{}, fmt.Errorf("get decision ref for entity %d: %w", entityID, err)
	}
	return domain.Reference{ProjectKey: projectKey, Kind: domain.KindDecision, Seq: seq}, nil
}

// GetDecisionRefByEntityIDAnyDeletion is GetDecisionRefByEntityID
// without the deleted_at IS NULL filter — see
// GetTicketRefByEntityIDAnyDeletion's doc (relationships.go) for why
// the activity feed needs this. Decisions have no soft-delete surface
// yet (no DELETE route), so this is currently equivalent to
// GetDecisionRefByEntityID; it exists for symmetry with the
// ticket/feature variants so the activity feed's resolution path
// doesn't need a kind-specific special case, and so it's already
// correct whenever decision soft-delete is added.
func GetDecisionRefByEntityIDAnyDeletion(ctx context.Context, q Querier, entityID int64) (domain.Reference, error) {
	var (
		projectKey string
		seq        int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT p.key, d.seq
		 FROM decisions d
		 JOIN projects p ON p.id = d.project_id
		 WHERE d.id = ?`,
		entityID,
	).Scan(&projectKey, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Reference{}, ErrNotFound
	}
	if err != nil {
		return domain.Reference{}, fmt.Errorf("get decision ref (any deletion) for entity %d: %w", entityID, err)
	}
	return domain.Reference{ProjectKey: projectKey, Kind: domain.KindDecision, Seq: seq}, nil
}

// InsertDecision creates a decision row. Called inside the same
// transaction as InsertEntity/AllocateReference, mirroring
// InsertFeature's contract.
func InsertDecision(ctx context.Context, q Querier, entityID, projectEntityID, seq int64, title, context_, decisionText, rationale, status string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO decisions (id, project_id, seq, title, context, decision, rationale, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entityID, projectEntityID, seq, title, context_, decisionText, rationale, status,
	); err != nil {
		return fmt.Errorf("insert decision: %w", err)
	}
	return nil
}

// UpdateDecisionFields applies a conditional field update (ADR 0008's
// version-guard pattern via bumpEntityVersion) — a plain field
// overwrite, not a versioned/archived edit the way EditComment is:
// Phase 3's decisions slice has no version-history requirement of its
// own (that's Phase 5's extension point, alongside supersession
// linking).
func UpdateDecisionFields(ctx context.Context, q Querier, entityID int64, title, context_, decisionText, rationale, status string, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE decisions SET title = ?, context = ?, decision = ?, rationale = ?, status = ? WHERE id = ?`,
		title, context_, decisionText, rationale, status, entityID,
	); err != nil {
		return 0, fmt.Errorf("update decision fields: %w", err)
	}
	return newVersion, nil
}
