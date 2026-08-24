package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
)

// AttachmentRow is the internal (store-only) view of an attachment.
// EntityID/CommentID are the raw FK values — exactly one is non-nil,
// enforced by internal/service, not this layer (see the migration's
// comment on why SQLite can't portably CHECK that). Resolving EntityID
// to a public reference is a service-layer concern (mentionTargetRef
// already does this generically across every principal kind), the
// same boundary DecisionRow draws for SupersededByID.
type AttachmentRow struct {
	Entity    domain.Attachment
	ID        int64
	EntityID  *int64
	CommentID *int64
	FileHash  *string
}

const attachmentSelectColumns = `
	a.id, a.entity_id, a.comment_id, a.kind, a.title, a.current_version,
	a.file_hash, a.file_name, a.file_size, a.media_type, a.checksum, a.path_value,
	a.created_at, a.deleted_at, ca.kind, ca.name`

func scanAttachmentRow(scan func(dest ...any) error) (AttachmentRow, error) {
	var (
		row                      AttachmentRow
		entityID, commentID      sql.NullInt64
		kind                     string
		fileHash, fileName       sql.NullString
		fileSize                 sql.NullInt64
		mediaType, checksum      sql.NullString
		pathValue                sql.NullString
		createdAt                string
		deletedAt                sql.NullString
		creatorKind, creatorName string
	)
	err := scan(&row.ID, &entityID, &commentID, &kind, &row.Entity.Title, &row.Entity.CurrentVersion,
		&fileHash, &fileName, &fileSize, &mediaType, &checksum, &pathValue,
		&createdAt, &deletedAt, &creatorKind, &creatorName)
	if err != nil {
		return AttachmentRow{}, err
	}
	if entityID.Valid {
		id := entityID.Int64
		row.EntityID = &id
	}
	if commentID.Valid {
		id := commentID.Int64
		row.CommentID = &id
		row.Entity.CommentID = id
	}
	row.Entity.ID = row.ID
	row.Entity.Kind = domain.AttachmentKind(kind)
	if fileHash.Valid {
		row.FileHash = &fileHash.String
	}
	if fileName.Valid {
		row.Entity.FileName = fileName.String
	}
	if fileSize.Valid {
		row.Entity.FileSize = fileSize.Int64
	}
	if mediaType.Valid {
		row.Entity.MediaType = mediaType.String
	}
	if checksum.Valid {
		row.Entity.Checksum = checksum.String
	}
	if pathValue.Valid {
		row.Entity.PathValue = pathValue.String
	}
	row.Entity.Creator = domain.ActorRef{Kind: domain.ActorKind(creatorKind), Name: creatorName}
	if row.Entity.CreatedAt, err = parseTime(createdAt); err != nil {
		return AttachmentRow{}, fmt.Errorf("parse attachment created_at: %w", err)
	}
	if deletedAt.Valid {
		t, err := parseTime(deletedAt.String)
		if err != nil {
			return AttachmentRow{}, fmt.Errorf("parse attachment deleted_at: %w", err)
		}
		row.Entity.DeletedAt = &t
	}
	return row, nil
}

// AttachmentFields groups the representation-specific columns shared
// by attachments/attachment_versions inserts and updates — the same
// nullable-column-bag shape content_items uses for its own
// representation-specific fields.
type AttachmentFields struct {
	Kind      domain.AttachmentKind
	FileHash  *string
	FileName  *string
	FileSize  *int64
	MediaType *string
	Checksum  *string
	PathValue *string
}

// InsertAttachment creates an attachment's current-state row. Exactly
// one of entityID/commentID must be non-nil — validated by
// internal/service before this is called.
func InsertAttachment(ctx context.Context, q Querier, entityID, commentID *int64, title string, f AttachmentFields, createdBy int64, now string) (int64, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO attachments (entity_id, comment_id, kind, title, current_version, file_hash, file_name, file_size, media_type, checksum, path_value, created_at, created_by)
		 VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entityID, commentID, string(f.Kind), title, f.FileHash, f.FileName, f.FileSize, f.MediaType, f.Checksum, f.PathValue, now, createdBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert attachment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("insert attachment: last insert id: %w", err)
	}
	if err := InsertAttachmentVersion(ctx, q, id, 1, f, createdBy, now); err != nil {
		return 0, err
	}
	return id, nil
}

// GetAttachment looks up an attachment by id, or ErrNotFound if
// missing or soft-deleted.
func GetAttachment(ctx context.Context, q Querier, id int64) (AttachmentRow, error) {
	query := `SELECT` + attachmentSelectColumns + `
		FROM attachments a
		JOIN actors ca ON ca.id = a.created_by
		WHERE a.id = ? AND a.deleted_at IS NULL`
	row, err := scanAttachmentRow(q.QueryRowContext(ctx, query, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return AttachmentRow{}, ErrNotFound
	}
	if err != nil {
		return AttachmentRow{}, fmt.Errorf("get attachment %d: %w", id, err)
	}
	return row, nil
}

// ListAttachmentsForEntity returns entityID's non-deleted attachments,
// oldest first.
func ListAttachmentsForEntity(ctx context.Context, q Querier, entityID int64) ([]AttachmentRow, error) {
	return queryAttachments(ctx, q, "a.entity_id = ?", entityID)
}

// ListAttachmentsForComment returns commentID's non-deleted
// attachments, oldest first.
func ListAttachmentsForComment(ctx context.Context, q Querier, commentID int64) ([]AttachmentRow, error) {
	return queryAttachments(ctx, q, "a.comment_id = ?", commentID)
}

func queryAttachments(ctx context.Context, q Querier, where string, arg int64) ([]AttachmentRow, error) {
	query := `SELECT` + attachmentSelectColumns + `
		FROM attachments a
		JOIN actors ca ON ca.id = a.created_by
		WHERE ` + where + ` AND a.deleted_at IS NULL
		ORDER BY a.created_at ASC, a.id ASC`
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AttachmentRow
	for rows.Next() {
		row, err := scanAttachmentRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// InsertAttachmentVersion archives one attachment state — called both
// for the initial version (from InsertAttachment) and for every
// subsequent replace (from UpdateAttachmentCurrent's caller, which
// archives the *new* state going forward, matching "each edit saves a
// full snapshot," §5.11 — unlike decisions/content_items, there is no
// pre-update snapshot to archive first, since a replaced binary/path
// version has nothing worth diffing against; the version list itself
// is the history).
func InsertAttachmentVersion(ctx context.Context, q Querier, attachmentID, version int64, f AttachmentFields, uploadedBy int64, now string) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO attachment_versions(attachment_id, version, kind, file_hash, file_name, file_size, media_type, checksum, path_value, uploaded_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attachmentID, version, string(f.Kind), f.FileHash, f.FileName, f.FileSize, f.MediaType, f.Checksum, f.PathValue, uploadedBy, now,
	); err != nil {
		return fmt.Errorf("insert attachment version: %w", err)
	}
	return nil
}

// ListAttachmentVersions returns an attachment's archived states,
// oldest first — including the current one (attachment_versions holds
// every version here, unlike decisions/content_items, since
// InsertAttachment archives version 1 immediately rather than waiting
// for a first edit).
func ListAttachmentVersions(ctx context.Context, q Querier, attachmentID int64) ([]domain.AttachmentVersion, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT av.version, av.kind, av.file_name, av.file_size, av.media_type, av.checksum, av.path_value, a.kind, a.name, av.created_at
		 FROM attachment_versions av JOIN actors a ON a.id = av.uploaded_by
		 WHERE av.attachment_id = ?
		 ORDER BY av.version ASC`,
		attachmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("list attachment versions for %d: %w", attachmentID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.AttachmentVersion
	for rows.Next() {
		v, err := scanAttachmentVersion(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAttachmentVersion returns one archived version by number, or
// ErrNotFound.
func GetAttachmentVersion(ctx context.Context, q Querier, attachmentID, version int64) (domain.AttachmentVersion, error) {
	row := q.QueryRowContext(ctx,
		`SELECT av.version, av.kind, av.file_name, av.file_size, av.media_type, av.checksum, av.path_value, a.kind, a.name, av.created_at
		 FROM attachment_versions av JOIN actors a ON a.id = av.uploaded_by
		 WHERE av.attachment_id = ? AND av.version = ?`,
		attachmentID, version,
	)
	v, err := scanAttachmentVersion(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AttachmentVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.AttachmentVersion{}, fmt.Errorf("get attachment version %d/%d: %w", attachmentID, version, err)
	}
	return v, nil
}

// GetAttachmentVersionBlobHash returns the file_hash a specific
// archived version points at — used by the versioned-download route,
// which needs the hash without pulling every other column.
func GetAttachmentVersionBlobHash(ctx context.Context, q Querier, attachmentID, version int64) (fileHash *string, fileName string, mediaType string, err error) {
	var hash, name, media sql.NullString
	row := q.QueryRowContext(ctx,
		`SELECT file_hash, file_name, media_type FROM attachment_versions WHERE attachment_id = ? AND version = ?`,
		attachmentID, version,
	)
	if err := row.Scan(&hash, &name, &media); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", "", ErrNotFound
		}
		return nil, "", "", fmt.Errorf("get attachment version blob hash %d/%d: %w", attachmentID, version, err)
	}
	if hash.Valid {
		fileHash = &hash.String
	}
	return fileHash, name.String, media.String, nil
}

func scanAttachmentVersion(scan func(dest ...any) error) (domain.AttachmentVersion, error) {
	var (
		v                              domain.AttachmentVersion
		kind                           string
		fileName, mediaType            sql.NullString
		fileSize                       sql.NullInt64
		checksum, pathValue            sql.NullString
		uploadedByKind, uploadedByName string
		createdAt                      string
	)
	if err := scan(&v.Version, &kind, &fileName, &fileSize, &mediaType, &checksum, &pathValue, &uploadedByKind, &uploadedByName, &createdAt); err != nil {
		return domain.AttachmentVersion{}, fmt.Errorf("scan attachment version: %w", err)
	}
	v.Kind = domain.AttachmentKind(kind)
	if fileName.Valid {
		v.FileName = fileName.String
	}
	if fileSize.Valid {
		v.FileSize = fileSize.Int64
	}
	if mediaType.Valid {
		v.MediaType = mediaType.String
	}
	if checksum.Valid {
		v.Checksum = checksum.String
	}
	if pathValue.Valid {
		v.PathValue = pathValue.String
	}
	v.UploadedBy = domain.ActorRef{Kind: domain.ActorKind(uploadedByKind), Name: uploadedByName}
	var err error
	if v.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.AttachmentVersion{}, fmt.Errorf("parse attachment version created_at: %w", err)
	}
	return v, nil
}

// UpdateAttachmentCurrent applies a conditional replace: it only takes
// effect if the attachment's current_version matches expectedVersion
// and it is not already soft-deleted, returning ErrVersionConflict
// otherwise — the same conditional-update pattern
// UpdateCommentBody/SoftDeleteComment use, since attachments (like
// comments) version themselves independently of entities.version.
func UpdateAttachmentCurrent(ctx context.Context, q Querier, attachmentID int64, f AttachmentFields, expectedVersion int64) (newVersion int64, err error) {
	res, err := q.ExecContext(ctx,
		`UPDATE attachments SET current_version = current_version + 1, kind = ?, file_hash = ?, file_name = ?, file_size = ?, media_type = ?, checksum = ?, path_value = ?
		 WHERE id = ? AND current_version = ? AND deleted_at IS NULL`,
		string(f.Kind), f.FileHash, f.FileName, f.FileSize, f.MediaType, f.Checksum, f.PathValue, attachmentID, expectedVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("update attachment: %w", err)
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

// SoftDeleteAttachment applies a conditional soft-delete — the row and
// every attachment_versions entry survive as a tombstone (§5.11's
// history requirement, mirroring SoftDeleteComment).
func SoftDeleteAttachment(ctx context.Context, q Querier, attachmentID int64, expectedVersion int64, now string) error {
	res, err := q.ExecContext(ctx,
		`UPDATE attachments SET deleted_at = ? WHERE id = ? AND current_version = ? AND deleted_at IS NULL`,
		now, attachmentID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("soft-delete attachment: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return ErrVersionConflict
	}
	return nil
}
