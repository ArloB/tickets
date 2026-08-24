package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// attachmentOwner is a resolved attachment target: exactly one of a
// principal entity or a comment. AuditEntityID is always a principal
// entity id — for a comment target it's the comment's *parent* entity,
// since audit_events.entity_id is NOT NULL and comments aren't
// entities themselves (mirroring AddComment's own audit call, which
// records against ticket.ID with the comment id riding along
// separately).
type attachmentOwner struct {
	EntityID      *int64
	CommentID     *int64
	AuditEntityID int64
}

// attachmentAuditEntityID resolves the entity_id an audit_events row
// for this attachment must carry (NOT NULL there): the attachment's
// own owning entity when it's on a principal entity, or that entity's
// comment's parent entity when it's on a comment — the same
// entity-id-plus-riding-along-comment-id shape AddComment's own audit
// call uses.
func (s *Service) attachmentAuditEntityID(ctx context.Context, q store.Querier, row store.AttachmentRow) (int64, error) {
	if row.EntityID != nil {
		return *row.EntityID, nil
	}
	c, err := store.GetComment(ctx, q, *row.CommentID)
	if err != nil {
		return 0, fmt.Errorf("service: look up comment for attachment audit: %w", err)
	}
	return c.EntityID, nil
}

// resolveAttachmentOwner resolves exactly one of ref (a principal
// entity reference) or commentID to the FK pair attachments stores
// against. Callers validate the "exactly one" invariant before this is
// reached; this only resolves whichever one was actually supplied.
func resolveAttachmentOwner(ctx context.Context, q store.Querier, ref domain.Reference, commentID int64) (attachmentOwner, error) {
	if commentID != 0 {
		row, err := store.GetComment(ctx, q, commentID)
		if errors.Is(err, store.ErrNotFound) {
			return attachmentOwner{}, newNotFoundError("comment not found")
		}
		if err != nil {
			return attachmentOwner{}, fmt.Errorf("service: look up comment: %w", err)
		}
		if row.Entity.DeletedAt != nil {
			return attachmentOwner{}, newNotFoundError("comment not found")
		}
		id := row.Entity.ID
		return attachmentOwner{CommentID: &id, AuditEntityID: row.EntityID}, nil
	}
	endpoint, err := resolveAssociationEndpoint(ctx, q, "ref", ref)
	if err != nil {
		return attachmentOwner{}, err
	}
	id := endpoint.EntityID
	return attachmentOwner{EntityID: &id, AuditEntityID: endpoint.EntityID}, nil
}

// CreateAttachmentRequest is CreateAttachment's input. Exactly one of
// Ref (a ticket/feature/decision/plan/document reference) or CommentID
// must be set. Content is required (and streamed straight into the
// blobstore, never buffered whole — §9) when Kind is
// AttachmentKindUpload; PathValue is required when Kind is
// AttachmentKindPath. There is deliberately no URL-kind field — see
// domain.AttachmentKind's doc.
type CreateAttachmentRequest struct {
	Ref       domain.Reference
	CommentID int64
	Title     string
	Kind      domain.AttachmentKind
	Content   io.Reader
	FileName  string
	MediaType string
	PathValue string
}

// CreateAttachment stores a new attachment (version 1) on a principal
// entity or a comment, within ADR 0007's boundary: an upload's bytes
// are hashed and content-addressed into the blobstore as they're
// streamed in; a path attachment stores only the path string, and
// nothing in this codebase ever reads it back off disk.
func (s *Service) CreateAttachment(ctx context.Context, req CreateAttachmentRequest, actor domain.ActorRef, correlationID string) (domain.Attachment, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.Attachment{}, newValidationError("title", "title is required")
	}
	if !req.Kind.Valid() {
		return domain.Attachment{}, newValidationError("kind", "invalid attachment kind %q", req.Kind)
	}
	hasRef := req.Ref.ProjectKey != ""
	hasComment := req.CommentID != 0
	if hasRef == hasComment {
		return domain.Attachment{}, newValidationError("owner", "attachment must target exactly one of an entity reference or a comment")
	}

	fields, err := s.buildAttachmentFields(req.Kind, req.Content, req.FileName, req.MediaType, req.PathValue)
	if err != nil {
		return domain.Attachment{}, err
	}

	var attachmentID int64
	err = s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		owner, err := resolveAttachmentOwner(ctx, tx, req.Ref, req.CommentID)
		if err != nil {
			return err
		}

		id, err := store.InsertAttachment(ctx, tx, owner.EntityID, owner.CommentID, title, fields, actorID, now)
		if err != nil {
			return fmt.Errorf("service: insert attachment: %w", err)
		}
		attachmentID = id

		changes := auditChanges(map[string]any{"attachment_id": id, "title": title, "kind": string(req.Kind)})
		if err := store.InsertAuditEvent(ctx, tx, owner.AuditEntityID, actorID, eventAttachmentAdded, corrID, owner.CommentID, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Attachment{}, err
	}
	return s.GetAttachment(ctx, attachmentID)
}

// buildAttachmentFields validates and assembles the representation-
// specific column bag for either kind, streaming an upload into the
// blobstore along the way.
func (s *Service) buildAttachmentFields(kind domain.AttachmentKind, content io.Reader, fileName, mediaType, pathValue string) (store.AttachmentFields, error) {
	fields := store.AttachmentFields{Kind: kind}
	switch kind {
	case domain.AttachmentKindUpload:
		if content == nil {
			return store.AttachmentFields{}, newValidationError("file", "file content is required for an upload attachment")
		}
		hash, size, err := s.blobs.Put(content)
		if err != nil {
			return store.AttachmentFields{}, fmt.Errorf("service: store attachment blob: %w", err)
		}
		fields.FileHash = &hash
		fields.Checksum = &hash
		fields.FileSize = &size
		if fileName != "" {
			fields.FileName = &fileName
		}
		if mediaType != "" {
			fields.MediaType = &mediaType
		}
	case domain.AttachmentKindPath:
		p := strings.TrimSpace(pathValue)
		if p == "" {
			return store.AttachmentFields{}, newValidationError("path", "path is required for a path attachment")
		}
		fields.PathValue = &p
		if fileName != "" {
			fields.FileName = &fileName
		}
		if mediaType != "" {
			fields.MediaType = &mediaType
		}
	}
	return fields, nil
}

// GetAttachment looks up an attachment by id.
func (s *Service) GetAttachment(ctx context.Context, id int64) (domain.Attachment, error) {
	row, err := store.GetAttachment(ctx, s.store.DB(), id)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Attachment{}, newNotFoundError("attachment not found")
	}
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("service: get attachment: %w", err)
	}
	return s.resolveAttachmentEntity(ctx, s.store.DB(), row)
}

// resolveAttachmentEntity fills in a store.AttachmentRow's public
// OwnerRef from its raw EntityID, mirroring resolveDecisionEntity's
// boundary: the store layer never joins across to format a ref.
func (s *Service) resolveAttachmentEntity(ctx context.Context, q store.Querier, row store.AttachmentRow) (domain.Attachment, error) {
	a := row.Entity
	if row.EntityID != nil {
		ref, err := mentionTargetRef(ctx, q, *row.EntityID)
		if err != nil {
			return domain.Attachment{}, fmt.Errorf("service: resolve attachment owner ref: %w", err)
		}
		formatted, err := domain.Format(ref)
		if err != nil {
			return domain.Attachment{}, fmt.Errorf("service: format attachment owner ref: %w", err)
		}
		a.OwnerRef = formatted
	}
	return a, nil
}

// ListAttachmentsForRef returns a principal entity's non-deleted
// attachments, oldest first.
func (s *Service) ListAttachmentsForRef(ctx context.Context, ref domain.Reference) ([]domain.Attachment, error) {
	endpoint, err := resolveAssociationEndpoint(ctx, s.store.DB(), "ref", ref)
	if err != nil {
		return nil, err
	}
	return s.listAttachments(ctx, store.ListAttachmentsForEntity, endpoint.EntityID)
}

// ListAttachmentsForComment returns a comment's non-deleted
// attachments, oldest first.
func (s *Service) ListAttachmentsForComment(ctx context.Context, commentID int64) ([]domain.Attachment, error) {
	row, err := store.GetComment(ctx, s.store.DB(), commentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("comment not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: look up comment: %w", err)
	}
	return s.listAttachments(ctx, store.ListAttachmentsForComment, row.Entity.ID)
}

func (s *Service) listAttachments(ctx context.Context, list func(context.Context, store.Querier, int64) ([]store.AttachmentRow, error), id int64) ([]domain.Attachment, error) {
	rows, err := list(ctx, s.store.DB(), id)
	if err != nil {
		return nil, fmt.Errorf("service: list attachments: %w", err)
	}
	out := make([]domain.Attachment, len(rows))
	for i, row := range rows {
		a, err := s.resolveAttachmentEntity(ctx, s.store.DB(), row)
		if err != nil {
			return nil, err
		}
		out[i] = a
	}
	return out, nil
}

// ListAttachmentVersions returns an attachment's archived states,
// oldest first (including the current one — see the store layer's
// doc on why attachment_versions holds every version, unlike
// decisions/content_items).
func (s *Service) ListAttachmentVersions(ctx context.Context, id int64) ([]domain.AttachmentVersion, error) {
	if _, err := store.GetAttachment(ctx, s.store.DB(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, newNotFoundError("attachment not found")
		}
		return nil, fmt.Errorf("service: look up attachment: %w", err)
	}
	versions, err := store.ListAttachmentVersions(ctx, s.store.DB(), id)
	if err != nil {
		return nil, fmt.Errorf("service: list attachment versions: %w", err)
	}
	return versions, nil
}

// AttachmentDownload is DownloadAttachment's result: a streamed
// reader (the caller must Close it) plus display metadata.
type AttachmentDownload struct {
	Content   io.ReadCloser
	FileName  string
	MediaType string
	FileSize  int64
}

// DownloadAttachment opens a streamed reader for an upload
// attachment's current bytes. A path attachment has no bytes to
// stream — ADR 0007's boundary means there is no code path here that
// ever opens its target — so this returns a validation error for one,
// which the caller renders as "this attachment has no downloadable
// content."
func (s *Service) DownloadAttachment(ctx context.Context, id int64) (AttachmentDownload, error) {
	row, err := store.GetAttachment(ctx, s.store.DB(), id)
	if errors.Is(err, store.ErrNotFound) {
		return AttachmentDownload{}, newNotFoundError("attachment not found")
	}
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf("service: look up attachment: %w", err)
	}
	if row.Entity.Kind != domain.AttachmentKindUpload || row.FileHash == nil {
		return AttachmentDownload{}, newValidationError("kind", "attachment %d has no downloadable content", id)
	}
	rc, err := s.blobs.Open(*row.FileHash)
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf("service: open attachment blob: %w", err)
	}
	return AttachmentDownload{Content: rc, FileName: row.Entity.FileName, MediaType: row.Entity.MediaType, FileSize: row.Entity.FileSize}, nil
}

// DownloadAttachmentVersion is DownloadAttachment for one archived
// version rather than the current state.
func (s *Service) DownloadAttachmentVersion(ctx context.Context, id, version int64) (AttachmentDownload, error) {
	if _, err := store.GetAttachment(ctx, s.store.DB(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return AttachmentDownload{}, newNotFoundError("attachment not found")
		}
		return AttachmentDownload{}, fmt.Errorf("service: look up attachment: %w", err)
	}
	hash, fileName, mediaType, err := store.GetAttachmentVersionBlobHash(ctx, s.store.DB(), id, version)
	if errors.Is(err, store.ErrNotFound) {
		return AttachmentDownload{}, newValidationError("version", "attachment %d has no version %d", id, version)
	}
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf("service: look up attachment version: %w", err)
	}
	if hash == nil {
		return AttachmentDownload{}, newValidationError("kind", "attachment %d version %d has no downloadable content", id, version)
	}
	rc, err := s.blobs.Open(*hash)
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf("service: open attachment blob: %w", err)
	}
	return AttachmentDownload{Content: rc, FileName: fileName, MediaType: mediaType}, nil
}

// ReplaceAttachmentRequest is ReplaceAttachment's input — a new
// version, not an in-place edit of the current one (§5.11: "each edit
// saves a full snapshot").
type ReplaceAttachmentRequest struct {
	ID              int64
	Kind            domain.AttachmentKind
	Content         io.Reader
	FileName        string
	MediaType       string
	PathValue       string
	ExpectedVersion int64
}

// ReplaceAttachment stores a new version as the attachment's current
// state, archiving it into attachment_versions as it's written —
// conditional on ExpectedVersion matching current_version (ADR 0008's
// pattern, applied to attachments' own version column the same way
// comments apply it to comments.version).
func (s *Service) ReplaceAttachment(ctx context.Context, req ReplaceAttachmentRequest, actor domain.ActorRef, correlationID string) (domain.Attachment, error) {
	if !req.Kind.Valid() {
		return domain.Attachment{}, newValidationError("kind", "invalid attachment kind %q", req.Kind)
	}
	fields, err := s.buildAttachmentFields(req.Kind, req.Content, req.FileName, req.MediaType, req.PathValue)
	if err != nil {
		return domain.Attachment{}, err
	}

	var result domain.Attachment
	err = s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		existing, err := store.GetAttachment(ctx, tx, req.ID)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("attachment not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up attachment: %w", err)
		}

		newVersion, err := store.UpdateAttachmentCurrent(ctx, tx, req.ID, fields, req.ExpectedVersion)
		if err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				return newVersionConflictError(existing.Entity.CurrentVersion)
			}
			return fmt.Errorf("service: replace attachment: %w", err)
		}
		if err := store.InsertAttachmentVersion(ctx, tx, req.ID, newVersion, fields, actorID, now); err != nil {
			return fmt.Errorf("service: archive attachment version: %w", err)
		}

		auditEntityID, err := s.attachmentAuditEntityID(ctx, tx, existing)
		if err != nil {
			return err
		}
		changes := auditChanges(map[string]any{"attachment_id": req.ID, "version": newVersion})
		if err := store.InsertAuditEvent(ctx, tx, auditEntityID, actorID, eventAttachmentReplaced, corrID, existing.CommentID, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetAttachment(ctx, tx, req.ID)
		if err != nil {
			return fmt.Errorf("service: reload replaced attachment: %w", err)
		}
		result, err = s.resolveAttachmentEntity(ctx, tx, updated)
		return err
	})
	if err != nil {
		return domain.Attachment{}, err
	}
	return result, nil
}

// DeleteAttachmentRequest is DeleteAttachment's input.
type DeleteAttachmentRequest struct {
	ID              int64
	ExpectedVersion int64
}

// DeleteAttachment soft-deletes an attachment: the row and every
// archived version survive as a tombstone (§5.11).
func (s *Service) DeleteAttachment(ctx context.Context, req DeleteAttachmentRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		existing, err := store.GetAttachment(ctx, tx, req.ID)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("attachment not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up attachment: %w", err)
		}

		if err := store.SoftDeleteAttachment(ctx, tx, req.ID, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				return newVersionConflictError(existing.Entity.CurrentVersion)
			}
			return fmt.Errorf("service: delete attachment: %w", err)
		}

		auditEntityID, err := s.attachmentAuditEntityID(ctx, tx, existing)
		if err != nil {
			return err
		}
		changes := auditChanges(map[string]any{"attachment_id": req.ID})
		if err := store.InsertAuditEvent(ctx, tx, auditEntityID, actorID, eventAttachmentRemoved, corrID, existing.CommentID, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
}
