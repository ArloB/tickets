package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/google/uuid"
)

// ErrNotFound is returned by lookup functions when no row matches. It
// carries no store-specific detail — internal/service maps it to
// domain.ErrNotFound at the API boundary.
var ErrNotFound = errors.New("store: not found")

// ErrVersionConflict is returned by conditional updates when the
// expected version doesn't match the row's current version.
var ErrVersionConflict = errors.New("store: version conflict")

// Querier is satisfied by both *sql.DB and *sql.Tx, so every function
// below works whether or not it's part of a larger transaction —
// internal/service decides the transaction boundary; internal/store
// only provides the typed statements (package doc.go).
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// TimeLayout is fixed-width (unlike time.RFC3339Nano, whose "9"
// fractional digits strip trailing zeros). Cursor pagination and every
// ORDER BY on a timestamp column compares these strings
// lexicographically — a variable-width format sorts wrong the moment
// two rows' fractional-second digit counts differ (e.g. ".5967807Z"
// sorts after ".59678071Z", because 'Z' > '1'). Exported so
// internal/service uses the same layout for idempotency_keys.created_at.
const TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Now returns the current instant formatted with TimeLayout. Callers
// that write more than one row inside a single transaction (in
// particular internal/service's tx helper) call this once and pass the
// same string to every write, so rows created together share an
// identical created_at/updated_at rather than drifting by
// microseconds — audit trails and fixture reproducibility both want
// that.
func Now() string { return time.Now().UTC().Format(TimeLayout) }

func parseTime(s string) (time.Time, error) {
	return time.Parse(TimeLayout, s)
}

// InsertEntity inserts a new entities row and returns its internal
// surrogate id (ADR 0002) and freshly generated UUIDv7 (product spec
// §5.2). projectID is nil only when the entity being created is itself
// a project. createdBy is the internal actor id (ADR 0012) — resolve
// it via GetActorIDByRef first; now is the caller's shared transaction
// timestamp (see Now).
//
// entities.created_by is schema-nullable (migration
// 0002_core_domain.sql: SQLite forbids a NOT NULL ALTER TABLE ADD
// COLUMN with a REFERENCES clause) but every InsertEntity call from
// Step 4a onward supplies one — NOT-NULL-ness for new rows is a Go
// invariant, not a schema constraint, the same pattern this codebase
// already uses for enum validation.
func InsertEntity(ctx context.Context, q Querier, projectID *int64, kind domain.EntityKind, createdBy int64, now string) (id int64, id36 string, err error) {
	u, err := uuid.NewV7()
	if err != nil {
		return 0, "", fmt.Errorf("generate uuid: %w", err)
	}
	res, err := q.ExecContext(ctx,
		`INSERT INTO entities(uuid, project_id, kind, version, created_at, updated_at, created_by)
		 VALUES (?, ?, ?, 1, ?, ?, ?)`,
		u[:], projectID, string(kind), now, now, createdBy,
	)
	if err != nil {
		return 0, "", fmt.Errorf("insert entity: %w", err)
	}
	insertedID, err := res.LastInsertId()
	if err != nil {
		return 0, "", fmt.Errorf("last insert id: %w", err)
	}
	return insertedID, u.String(), nil
}

// AllocateReference increments and returns the pre-increment sequence
// for (projectID, kind), seeding the counter at 1 if this is the
// kind's first allocation in the project (ADR 0009). Must be called
// inside the same transaction as the entity it names.
func AllocateReference(ctx context.Context, q Querier, projectID int64, kind domain.EntityKind) (int64, error) {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO reference_counters(project_id, kind, next_seq) VALUES (?, ?, 1)
		 ON CONFLICT(project_id, kind) DO NOTHING`,
		projectID, string(kind),
	); err != nil {
		return 0, fmt.Errorf("seed reference counter: %w", err)
	}
	var seq int64
	err := q.QueryRowContext(ctx,
		`UPDATE reference_counters SET next_seq = next_seq + 1
		 WHERE project_id = ? AND kind = ?
		 RETURNING next_seq - 1`,
		projectID, string(kind),
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("allocate reference: %w", err)
	}
	return seq, nil
}

// InsertProject writes the projects row for an already-inserted
// entities row.
func InsertProject(ctx context.Context, q Querier, entityID int64, key, title, description string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO projects(id, key, title, description, status) VALUES (?, ?, ?, ?, 'active')`,
		entityID, key, title, description,
	)
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

// SetProjectGeneralFeature records the project's mandatory General
// feature (ADR 0001).
func SetProjectGeneralFeature(ctx context.Context, q Querier, projectEntityID, generalFeatureEntityID int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE projects SET general_feature_id = ? WHERE id = ?`,
		generalFeatureEntityID, projectEntityID,
	)
	if err != nil {
		return fmt.Errorf("set general feature: %w", err)
	}
	return nil
}

// InsertFeature writes the features row for an already-inserted
// entities row. priority_rank is derived from priority here (rank.go)
// — every write path for it must go through the same derivation.
func InsertFeature(ctx context.Context, q Querier, entityID, projectEntityID, seq int64, title, description, priority string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO features(id, project_id, seq, title, status, description, priority, priority_rank)
		 VALUES (?, ?, ?, ?, 'backlog', ?, ?, ?)`,
		entityID, projectEntityID, seq, title, description, priority, priorityRank(priority),
	)
	if err != nil {
		return fmt.Errorf("insert feature: %w", err)
	}
	return nil
}

// ProjectRow is the internal (store-only) view of a project: the
// domain.Project value plus the internal surrogate id, which service
// needs for further joins but which never leaves internal/service.
type ProjectRow struct {
	Entity           domain.Project
	ID               int64
	GeneralFeatureID int64
}

// GetProjectByKey returns the project and its internal ids, or
// ErrNotFound.
func GetProjectByKey(ctx context.Context, q Querier, key string) (ProjectRow, error) {
	var (
		row                          ProjectRow
		u                            []byte
		status, createdAt, updatedAt string
		generalFeatureID             sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT e.id, e.uuid, p.key, p.title, p.description, p.status, e.version,
		        e.created_at, e.updated_at, p.general_feature_id
		 FROM projects p JOIN entities e ON e.id = p.id
		 WHERE p.key = ? AND e.deleted_at IS NULL`,
		key,
	).Scan(&row.ID, &u, &row.Entity.Key, &row.Entity.Title, &row.Entity.Description,
		&status, &row.Entity.Version, &createdAt, &updatedAt, &generalFeatureID)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectRow{}, ErrNotFound
	}
	if err != nil {
		return ProjectRow{}, fmt.Errorf("get project %q: %w", key, err)
	}
	parsed, err := uuid.FromBytes(u)
	if err != nil {
		return ProjectRow{}, fmt.Errorf("parse project uuid: %w", err)
	}
	row.Entity.UUID = parsed.String()
	row.Entity.Status = domain.ProjectStatus(status)
	if row.Entity.CreatedAt, err = parseTime(createdAt); err != nil {
		return ProjectRow{}, fmt.Errorf("parse project created_at: %w", err)
	}
	if row.Entity.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ProjectRow{}, fmt.Errorf("parse project updated_at: %w", err)
	}
	if generalFeatureID.Valid {
		row.GeneralFeatureID = generalFeatureID.Int64
	}
	return row, nil
}

// ProjectPage is a cursor-paginated page of projects, ordered by
// (created_at, id) — not an offset (docs/contracts/representations.md
// note on avoiding offset pagination wearing a cursor's name).
type ProjectPage struct {
	Projects   []domain.Project
	NextCursor string // empty if this is the last page
}

// ListProjects returns up to limit projects with (created_at, id) after
// the given cursor values (both zero-value for the first page).
func ListProjects(ctx context.Context, q Querier, limit int, afterCreatedAt string, afterID int64) (ProjectPage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT e.id, e.uuid, p.key, p.title, p.description, p.status, e.version,
		        e.created_at, e.updated_at
		 FROM projects p JOIN entities e ON e.id = p.id
		 WHERE e.deleted_at IS NULL
		   AND (e.created_at > ? OR (e.created_at = ? AND e.id > ?))
		 ORDER BY e.created_at ASC, e.id ASC
		 LIMIT ?`,
		afterCreatedAt, afterCreatedAt, afterID, limit+1,
	)
	if err != nil {
		return ProjectPage{}, fmt.Errorf("list projects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// ids/createdAts tracks the raw sort key per fetched row (parallel to
	// page.Projects) so the cursor can be built from the last row that
	// actually ends up IN the page after truncation below — not from
	// whatever the loop happened to scan last, which is the limit+1'th
	// overflow row whenever there is a next page.
	var (
		page       ProjectPage
		ids        []int64
		createdAts []string
	)
	for rows.Next() {
		var (
			id                           int64
			u                            []byte
			status, createdAt, updatedAt string
			p                            domain.Project
		)
		if err := rows.Scan(&id, &u, &p.Key, &p.Title, &p.Description, &status, &p.Version, &createdAt, &updatedAt); err != nil {
			return ProjectPage{}, fmt.Errorf("scan project: %w", err)
		}
		parsed, err := uuid.FromBytes(u)
		if err != nil {
			return ProjectPage{}, fmt.Errorf("parse project uuid: %w", err)
		}
		p.UUID = parsed.String()
		p.Status = domain.ProjectStatus(status)
		if p.CreatedAt, err = parseTime(createdAt); err != nil {
			return ProjectPage{}, fmt.Errorf("parse project created_at: %w", err)
		}
		if p.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return ProjectPage{}, fmt.Errorf("parse project updated_at: %w", err)
		}
		page.Projects = append(page.Projects, p)
		ids = append(ids, id)
		createdAts = append(createdAts, createdAt)
	}
	if err := rows.Err(); err != nil {
		return ProjectPage{}, err
	}

	if len(page.Projects) > limit {
		page.Projects = page.Projects[:limit]
		page.NextCursor = EncodeCreatedAtIDCursor(createdAts[limit-1], ids[limit-1])
	}
	return page, nil
}

// InsertTicket writes the tickets row for an already-inserted entities
// row. priority_rank/severity_rank are derived from priority/severity
// here (see rank.go) — every write path for those two columns must go
// through the same derivation, not just this one.
func InsertTicket(ctx context.Context, q Querier, entityID, projectEntityID, featureEntityID, seq int64,
	ticketType, title, description, status, priority string, severity *string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO tickets(id, project_id, feature_id, seq, type, title, description, status, priority, severity, priority_rank, severity_rank)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entityID, projectEntityID, featureEntityID, seq, ticketType, title, description, status, priority, severity,
		priorityRank(priority), severityRank(severity),
	)
	if err != nil {
		return fmt.Errorf("insert ticket: %w", err)
	}
	return nil
}

// TicketRow is the internal (store-only) view of a ticket. PriorityRank/
// SeverityRank/Position are ordering mechanics (rank.go, ADR 0011) —
// deliberately not on domain.Ticket, the same "internal-only, never
// serialized" boundary ADR 0002 already draws for ID/ProjectEntityID/
// FeatureEntityID on this same struct.
type TicketRow struct {
	Entity          domain.Ticket
	ID              int64
	ProjectEntityID int64
	FeatureEntityID int64
	PriorityRank    int64
	SeverityRank    int64
	Position        int64
}

const ticketSelectColumns = `
	e.id, e.uuid, e.version, e.created_at, e.updated_at,
	p.key, t.project_id, t.feature_id, f.seq,
	t.seq, t.type, t.title, t.description, t.status, t.priority, t.severity,
	t.priority_rank, t.severity_rank, t.position,
	a.kind, a.name`

func scanTicketRow(scan func(dest ...any) error) (TicketRow, error) {
	var (
		row                          TicketRow
		u                            []byte
		createdAt, updatedAt         string
		ticketType, status, priority string
		severity                     sql.NullString
		featureSeq, ticketSeq        int64
		assigneeKind, assigneeName   sql.NullString
	)
	err := scan(&row.ID, &u, &row.Entity.Version, &createdAt, &updatedAt,
		&row.Entity.ProjectKey, &row.ProjectEntityID, &row.FeatureEntityID, &featureSeq,
		&ticketSeq, &ticketType, &row.Entity.Title, &row.Entity.Description, &status, &priority, &severity,
		&row.PriorityRank, &row.SeverityRank, &row.Position,
		&assigneeKind, &assigneeName)
	if err != nil {
		return TicketRow{}, err
	}
	parsed, err := uuid.FromBytes(u)
	if err != nil {
		return TicketRow{}, fmt.Errorf("parse ticket uuid: %w", err)
	}
	row.Entity.UUID = parsed.String()
	if row.Entity.CreatedAt, err = parseTime(createdAt); err != nil {
		return TicketRow{}, fmt.Errorf("parse ticket created_at: %w", err)
	}
	if row.Entity.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return TicketRow{}, fmt.Errorf("parse ticket updated_at: %w", err)
	}
	row.Entity.Type = domain.TicketType(ticketType)
	row.Entity.Status = domain.WorkflowStatus(status)
	row.Entity.Priority = domain.Priority(priority)
	if severity.Valid {
		sev := domain.Severity(severity.String)
		row.Entity.Severity = &sev
	}
	if assigneeKind.Valid {
		row.Entity.Assignee = &domain.ActorRef{Kind: domain.ActorKind(assigneeKind.String), Name: assigneeName.String}
	}

	ref, err := domain.Format(domain.Reference{ProjectKey: row.Entity.ProjectKey, Kind: domain.KindTicket, Seq: ticketSeq})
	if err != nil {
		return TicketRow{}, fmt.Errorf("format ticket ref: %w", err)
	}
	row.Entity.Ref = ref

	featureRef, err := domain.Format(domain.Reference{ProjectKey: row.Entity.ProjectKey, Kind: domain.KindFeature, Seq: featureSeq})
	if err != nil {
		return TicketRow{}, fmt.Errorf("format feature ref: %w", err)
	}
	row.Entity.FeatureRef = featureRef

	return row, nil
}

// GetTicketByRef looks up a ticket by its parsed reference, or returns
// ErrNotFound. It rejects a reference whose Kind isn't KindTicket
// rather than matching on (ProjectKey, Seq) alone — the query below
// joins through the tickets table regardless of Kind, so
// GetTicketByRef(Reference{ProjectKey: "ABC", Kind: KindFeature, Seq: 1})
// would otherwise silently return ticket ABC-1 for a request that
// asked for feature ABC-F1. Harmless in Phase 0 (only tickets had a
// resolvable reference), but a real bug once features get one too.
func GetTicketByRef(ctx context.Context, q Querier, ref domain.Reference) (TicketRow, error) {
	if ref.Kind != domain.KindTicket {
		return TicketRow{}, ErrNotFound
	}
	query := `SELECT` + ticketSelectColumns + `
		FROM tickets t
		JOIN entities e ON e.id = t.id
		JOIN projects p ON p.key = ?
		JOIN features f ON f.id = t.feature_id
		LEFT JOIN actors a ON a.id = t.assignee_id
		WHERE t.project_id = p.id AND t.seq = ? AND e.deleted_at IS NULL`
	row, err := scanTicketRow(q.QueryRowContext(ctx, query, ref.ProjectKey, ref.Seq).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return TicketRow{}, ErrNotFound
	}
	if err != nil {
		return TicketRow{}, fmt.Errorf("get ticket %s-%d: %w", ref.ProjectKey, ref.Seq, err)
	}
	return row, nil
}

// bumpEntityVersion is the conditional-update pattern every mutating
// store function on an entities-backed row uses: it only takes effect
// if the row's current version matches expectedVersion and the row is
// not soft-deleted, returning ErrVersionConflict otherwise (ADR 0008).
// Centralized so every new Phase 1 field-update function gets the
// same soft-delete-aware behavior for free instead of reimplementing
// it per table — callers still look up the row first via a
// deleted_at-filtering Get*ByRef (so a delete cannot race this call
// inside the same BEGIN IMMEDIATE transaction, ADR 0003), but the
// WHERE clause here is the same defense-in-depth Phase 0 already had.
// now is the caller's shared transaction timestamp (see Now).
func bumpEntityVersion(ctx context.Context, q Querier, entityID int64, expectedVersion int64, now string) (newVersion int64, err error) {
	res, err := q.ExecContext(ctx,
		`UPDATE entities SET version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ? AND deleted_at IS NULL`,
		now, entityID, expectedVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("bump entity version: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return 0, ErrVersionConflict
	}
	return expectedVersion + 1, nil
}

// UpdateTicketStatus applies a conditional status update: it only
// takes effect if the row's current version matches expectedVersion
// (ADR 0008 / docs/contracts/concurrency.md). Returns ErrVersionConflict
// (with the row's actual current version) if it does not. now is the
// caller's shared transaction timestamp (see Now).
func UpdateTicketStatus(ctx context.Context, q Querier, entityID int64, newStatus string, expectedVersion int64, now string) (newVersion int64, err error) {
	newVersion, err = bumpEntityVersion(ctx, q, entityID, expectedVersion, now)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE tickets SET status = ? WHERE id = ?`, newStatus, entityID); err != nil {
		return 0, fmt.Errorf("update ticket status: %w", err)
	}
	return newVersion, nil
}

// CurrentEntityVersion is used to populate current_version on a 409
// version_conflict response (docs/contracts/errors.md).
func CurrentEntityVersion(ctx context.Context, q Querier, entityID int64) (int64, error) {
	var v int64
	err := q.QueryRowContext(ctx, `SELECT version FROM entities WHERE id = ?`, entityID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("current version: %w", err)
	}
	return v, nil
}

// PurgeIdempotencyKeysOlderThan deletes idempotency_keys rows whose
// created_at is strictly before cutoff (TimeLayout-formatted, see
// Now), returning the number deleted. 0001_initial.sql's comment on
// idempotency_keys promises "bounded retention... enforced by
// application-level cleanup" — this is that cleanup function.
// docs/contracts/concurrency.md parks *scheduling* it (deciding a
// retention window and calling this on a timer) with Phase 2's
// admin/maintenance operations; this function exists now so Phase 2
// has something to call rather than needing to write and test it then.
func PurgeIdempotencyKeysOlderThan(ctx context.Context, q Querier, cutoff string) (int64, error) {
	res, err := q.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge idempotency keys: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return affected, nil
}
