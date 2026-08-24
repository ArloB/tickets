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
// TicketRow already draw. SupersededByID is the raw entities.id
// superseded_by stores (nil until set) — internal/service resolves it
// to a public reference via GetDecisionRefByEntityIDAnyDeletion,
// mirroring how derived_mentions' bare target ids are resolved; this
// layer never joins across to format a ref itself.
type DecisionRow struct {
	Entity          domain.Decision
	ID              int64
	ProjectEntityID int64
	SupersededByID  *int64
}

const decisionSelectColumns = `
	e.id, e.uuid, e.version, e.created_at, e.updated_at,
	p.key, d.project_id, d.seq,
	d.title, d.context, d.decision, d.rationale, d.consequences, d.status, d.superseded_by, e.deleted_at,
	ca.kind, ca.name`

func scanDecisionRow(scan func(dest ...any) error) (DecisionRow, error) {
	var (
		row                      DecisionRow
		u                        []byte
		createdAt, updatedAt     string
		status                   string
		seq                      int64
		supersededBy             sql.NullInt64
		deletedAt                sql.NullString
		creatorKind, creatorName sql.NullString
	)
	err := scan(&row.ID, &u, &row.Entity.Version, &createdAt, &updatedAt,
		&row.Entity.ProjectKey, &row.ProjectEntityID, &seq,
		&row.Entity.Title, &row.Entity.Context, &row.Entity.Decision, &row.Entity.Rationale, &row.Entity.Consequences, &status, &supersededBy, &deletedAt,
		&creatorKind, &creatorName)
	if err != nil {
		return DecisionRow{}, err
	}
	if supersededBy.Valid {
		id := supersededBy.Int64
		row.SupersededByID = &id
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
func InsertDecision(ctx context.Context, q Querier, entityID, projectEntityID, seq int64, title, context_, decisionText, rationale, consequences, status string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO decisions (id, project_id, seq, title, context, decision, rationale, consequences, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entityID, projectEntityID, seq, title, context_, decisionText, rationale, consequences, status,
	); err != nil {
		return fmt.Errorf("insert decision: %w", err)
	}
	return nil
}

// UpdateDecisionFields applies a conditional field update (ADR 0008's
// version-guard pattern via bumpEntityVersion) — a plain field
// overwrite; the archived-version snapshot (§5.8: "every version
// remains visible") is a separate InsertDecisionVersion call the
// caller makes first, against the pre-update row it already read,
// mirroring EditComment's InsertCommentVersion-then-UpdateCommentBody
// order. supersededByID is nil to clear an existing supersession link
// (this is a full-representation update, like every other field here —
// whatever the caller sends is exactly what gets stored).
func UpdateDecisionFields(ctx context.Context, q Querier, entityID int64, title, context_, decisionText, rationale, consequences, status string, supersededByID *int64, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE decisions SET title = ?, context = ?, decision = ?, rationale = ?, consequences = ?, status = ?, superseded_by = ? WHERE id = ?`,
		title, context_, decisionText, rationale, consequences, status, supersededByID, entityID,
	); err != nil {
		return 0, fmt.Errorf("update decision fields: %w", err)
	}
	return newVersion, nil
}

// InsertDecisionVersion archives one decision's pre-update state —
// called with the row's fields and version *before* UpdateDecisionFields
// overwrites them, the same order EditComment uses for comment_versions.
func InsertDecisionVersion(ctx context.Context, q Querier, decisionID, version int64, title, context_, decisionText, rationale, consequences, status string, editedBy int64, now string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO decision_versions(decision_id, version, title, context, decision, rationale, consequences, status, edited_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decisionID, version, title, context_, decisionText, rationale, consequences, status, editedBy, now,
	); err != nil {
		return fmt.Errorf("insert decision version: %w", err)
	}
	return nil
}

// ListDecisionVersions returns a decision's archived prior states,
// oldest first — the live row (decisions table) is not included, since
// it's not archived until the next edit, mirroring ListCommentVersions.
func ListDecisionVersions(ctx context.Context, q Querier, decisionID int64) ([]domain.DecisionVersion, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT dv.version, dv.title, dv.context, dv.decision, dv.rationale, dv.consequences, dv.status, a.kind, a.name, dv.created_at
		 FROM decision_versions dv JOIN actors a ON a.id = dv.edited_by
		 WHERE dv.decision_id = ?
		 ORDER BY dv.version ASC`,
		decisionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list decision versions for %d: %w", decisionID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.DecisionVersion
	for rows.Next() {
		var (
			v                          domain.DecisionVersion
			status                     string
			editedByKind, editedByName string
			createdAt                  string
		)
		if err := rows.Scan(&v.Version, &v.Title, &v.Context, &v.Decision, &v.Rationale, &v.Consequences, &status, &editedByKind, &editedByName, &createdAt); err != nil {
			return nil, fmt.Errorf("scan decision version: %w", err)
		}
		v.Status = domain.DecisionStatus(status)
		v.EditedBy = domain.ActorRef{Kind: domain.ActorKind(editedByKind), Name: editedByName}
		if v.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("parse decision version created_at: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDecisionVersion returns one archived version by number, or
// ErrNotFound. Used by the diff endpoint, which names two version
// numbers rather than always comparing against the live row.
func GetDecisionVersion(ctx context.Context, q Querier, decisionID, version int64) (domain.DecisionVersion, error) {
	var (
		v                          domain.DecisionVersion
		status                     string
		editedByKind, editedByName string
		createdAt                  string
	)
	err := q.QueryRowContext(ctx,
		`SELECT dv.version, dv.title, dv.context, dv.decision, dv.rationale, dv.consequences, dv.status, a.kind, a.name, dv.created_at
		 FROM decision_versions dv JOIN actors a ON a.id = dv.edited_by
		 WHERE dv.decision_id = ? AND dv.version = ?`,
		decisionID, version,
	).Scan(&v.Version, &v.Title, &v.Context, &v.Decision, &v.Rationale, &v.Consequences, &status, &editedByKind, &editedByName, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DecisionVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.DecisionVersion{}, fmt.Errorf("get decision version %d/%d: %w", decisionID, version, err)
	}
	v.Status = domain.DecisionStatus(status)
	v.EditedBy = domain.ActorRef{Kind: domain.ActorKind(editedByKind), Name: editedByName}
	if v.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.DecisionVersion{}, fmt.Errorf("parse decision version created_at: %w", err)
	}
	return v, nil
}
