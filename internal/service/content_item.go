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

// validContentItemKind reports whether kind is one content_items can
// represent — a plan or a document, never a decision/ticket/feature
// (those have their own tables) and never a project (no reference
// token).
func validContentItemKind(kind domain.EntityKind) bool {
	return kind == domain.KindPlan || kind == domain.KindDocument
}

// buildContentItemFields validates and assembles the representation-
// specific column bag for any of the four representations, streaming
// a file upload into the blobstore along the way — mirrors
// Service.buildAttachmentFields, extended with the markdown/url cases
// content items have and attachments don't.
func (s *Service) buildContentItemFields(representation domain.ContentRepresentation, body string, content io.Reader, fileName, mediaType, pathValue, urlValue string) (store.ContentItemFields, error) {
	fields := store.ContentItemFields{Representation: representation}
	switch representation {
	case domain.ContentRepresentationMarkdown:
		fields.Body = body
	case domain.ContentRepresentationFile:
		if content == nil {
			return store.ContentItemFields{}, newValidationError("file", "file content is required for a file representation")
		}
		hash, size, err := s.blobs.Put(content)
		if err != nil {
			return store.ContentItemFields{}, fmt.Errorf("service: store content item blob: %w", err)
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
	case domain.ContentRepresentationPath:
		p := strings.TrimSpace(pathValue)
		if p == "" {
			return store.ContentItemFields{}, newValidationError("path", "path is required for a path representation")
		}
		fields.PathValue = &p
	case domain.ContentRepresentationURL:
		u := strings.TrimSpace(urlValue)
		if !domain.ValidateLinkURL(u) {
			return store.ContentItemFields{}, newValidationError("url", "url must be an http(s) or mailto URL")
		}
		fields.URLValue = &u
	default:
		return store.ContentItemFields{}, newValidationError("representation", "invalid representation %q", representation)
	}
	return fields, nil
}

// contentItemFieldsFromEntity reconstructs a store.ContentItemFields
// from a already-loaded row's current state — used to archive the
// pre-update row into content_versions before UpdateContentItemFields
// overwrites it. Only the fields relevant to the row's own
// representation are ever non-empty (buildContentItemFields only ever
// sets those), so this doesn't need to switch on representation
// itself.
func contentItemFieldsFromEntity(row store.ContentItemRow) store.ContentItemFields {
	f := store.ContentItemFields{
		Representation: domain.ContentRepresentation(row.Entity.Representation),
		Body:           row.Entity.Body,
		FileHash:       row.FileHash,
	}
	if row.Entity.FileName != "" {
		f.FileName = &row.Entity.FileName
	}
	if row.Entity.FileSize != 0 {
		f.FileSize = &row.Entity.FileSize
	}
	if row.Entity.MediaType != "" {
		f.MediaType = &row.Entity.MediaType
	}
	if row.Entity.Checksum != "" {
		f.Checksum = &row.Entity.Checksum
	}
	if row.Entity.PathValue != "" {
		f.PathValue = &row.Entity.PathValue
	}
	if row.Entity.URLValue != "" {
		f.URLValue = &row.Entity.URLValue
	}
	return f
}

// CreateContentItemRequest is CreateContentItem's input. Representation
// defaults to "markdown" when empty, preserving Step 3's original
// behavior for callers that don't care about the other three. Content
// is required (and streamed straight into the blobstore, never
// buffered whole — §9) when Representation is "file"; PathValue/
// URLValue are required for "path"/"url" respectively.
type CreateContentItemRequest struct {
	ProjectKey     string
	Kind           domain.EntityKind
	Title          string
	Representation domain.ContentRepresentation
	Body           string
	Content        io.Reader
	FileName       string
	MediaType      string
	PathValue      string
	URLValue       string
}

// CreateContentItem allocates a reference and creates a plan or
// document in the given project, attributed to actor — mirrors
// CreateDecision's shape (idempotency wired in from day one, since this
// is a real MCP/CLI-reachable write from the start).
func (s *Service) CreateContentItem(ctx context.Context, req CreateContentItemRequest, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (domain.ContentItem, error) {
	if !validContentItemKind(req.Kind) {
		return domain.ContentItem{}, newValidationError("kind", "kind must be \"plan\" or \"document\", got %q", req.Kind)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.ContentItem{}, newValidationError("title", "title is required")
	}
	representation := req.Representation
	if representation == "" {
		representation = domain.ContentRepresentationMarkdown
	}
	fields, err := s.buildContentItemFields(representation, req.Body, req.Content, req.FileName, req.MediaType, req.PathValue, req.URLValue)
	if err != nil {
		return domain.ContentItem{}, err
	}

	var result domain.Reference
	var notifiedIDs []int64
	err = s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if cached, found, err := checkIdempotency(ctx, tx, idemKey, actorID, fingerprint); err != nil {
			return err
		} else if found {
			ref, perr := domain.Parse(cached)
			if perr != nil {
				return fmt.Errorf("service: parse cached content item ref %q: %w", cached, perr)
			}
			result = ref
			return nil
		}

		proj, err := store.GetProjectByKey(ctx, tx, req.ProjectKey)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("project %q not found", req.ProjectKey)
		}
		if err != nil {
			return fmt.Errorf("service: look up project: %w", err)
		}

		entityID, _, err := store.InsertEntity(ctx, tx, &proj.ID, req.Kind, actorID, now)
		if err != nil {
			return fmt.Errorf("service: create content item entity: %w", err)
		}
		seq, err := store.AllocateReference(ctx, tx, proj.ID, req.Kind)
		if err != nil {
			return fmt.Errorf("service: allocate content item reference: %w", err)
		}
		if err := store.InsertContentItem(ctx, tx, entityID, proj.ID, req.Kind, seq, title, fields); err != nil {
			return fmt.Errorf("service: create content item: %w", err)
		}
		ids, err := rescanMentions(ctx, tx, entityID, sourceOwnBody, req.ProjectKey, fields.Body, now, actorID)
		if err != nil {
			return err
		}
		notifiedIDs = ids

		ref := domain.Reference{ProjectKey: req.ProjectKey, Kind: req.Kind, Seq: seq}
		refStr, err := domain.Format(ref)
		if err != nil {
			return fmt.Errorf("service: format created content item ref: %w", err)
		}
		if err := indexContentItemSearchDoc(ctx, tx, entityID, proj.ID, domain.ContentItem{
			Ref: refStr, Kind: req.Kind, Title: title, Representation: string(representation),
			Body: fields.Body, FileName: derefStr(fields.FileName), PathValue: derefStr(fields.PathValue), URLValue: derefStr(fields.URLValue),
		}); err != nil {
			return err
		}
		if err := subscribe(ctx, tx, entityID, actorID, now); err != nil {
			return err
		}

		changes := auditChanges(map[string]any{"ref": refStr, "title": title})
		if err := store.InsertAuditEvent(ctx, tx, entityID, actorID, eventContentItemCreated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		if err := recordIdempotency(ctx, tx, idemKey, actorID, fingerprint, refStr, now); err != nil {
			return err
		}
		result = ref
		return nil
	})
	if err != nil {
		return domain.ContentItem{}, err
	}
	item, err := s.GetContentItem(ctx, result)
	if err != nil {
		return domain.ContentItem{}, err
	}
	s.broadcast(ChangeHint{Kind: HintEntityChanged, Ref: item.Ref, Project: item.ProjectKey})
	s.publishNotified(ctx, notifiedIDs)
	return item, nil
}

// GetContentItem looks up a plan or document by its parsed reference.
func (s *Service) GetContentItem(ctx context.Context, ref domain.Reference) (domain.ContentItem, error) {
	row, err := store.GetContentItemByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return domain.ContentItem{}, newNotFoundError("content item not found")
	}
	if err != nil {
		return domain.ContentItem{}, fmt.Errorf("service: get content item: %w", err)
	}
	return row.Entity, nil
}

// ContentItemsListResult is ListContentItems' output.
type ContentItemsListResult struct {
	Items      []domain.ContentItem
	NextCursor string
}

// ListContentItems returns a cursor-paginated page of a project's
// non-deleted plans or documents (kind selects which) — mirrors
// ListDecisions.
func (s *Service) ListContentItems(ctx context.Context, projectKey string, kind domain.EntityKind, limit int, cursor string) (ContentItemsListResult, error) {
	if !validContentItemKind(kind) {
		return ContentItemsListResult{}, newValidationError("kind", "kind must be \"plan\" or \"document\", got %q", kind)
	}
	proj, err := store.GetProjectByKey(ctx, s.store.DB(), projectKey)
	if errors.Is(err, store.ErrNotFound) {
		return ContentItemsListResult{}, newNotFoundError("project %q not found", projectKey)
	}
	if err != nil {
		return ContentItemsListResult{}, fmt.Errorf("service: look up project: %w", err)
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	afterCreatedAt, afterID, derr := store.DecodeCreatedAtIDCursor(cursor)
	if derr != nil {
		return ContentItemsListResult{}, newValidationError("cursor", "invalid cursor")
	}
	page, err := store.ListContentItemsForProjectPage(ctx, s.store.DB(), proj.ID, kind, limit, afterCreatedAt, afterID)
	if err != nil {
		return ContentItemsListResult{}, fmt.Errorf("service: list content items: %w", err)
	}

	out := make([]domain.ContentItem, len(page.Items))
	for i, row := range page.Items {
		out[i] = row.Entity
	}
	return ContentItemsListResult{Items: out, NextCursor: page.NextCursor}, nil
}

// UpdateContentItemRequest is UpdateContentItem's input — a
// full-representation update, matching UpdateDecisionRequest's
// contract (every field required, no partial merge). There is no
// Representation field here: representation is immutable after
// creation (Phase 5 plan's confirmed decision), so this only ever
// carries the fields relevant to whichever representation the
// existing item already has — Body for markdown, Content/FileName/
// MediaType for file, PathValue for path, URLValue for url.
type UpdateContentItemRequest struct {
	Ref             domain.Reference
	Title           string
	Body            string
	Content         io.Reader
	FileName        string
	MediaType       string
	PathValue       string
	URLValue        string
	ExpectedVersion int64
}

// UpdateContentItem applies a conditional field update (ADR 0008),
// archiving the pre-update row into content_versions first (§5.9:
// "each edit saves a full snapshot") — mirrors UpdateDecision's
// snapshot-then-overwrite order. Reads the item once outside the
// transaction first, purely to learn its (immutable) representation
// before building the new field set — a file upload streams into the
// blobstore ahead of the transaction, the same way CreateContentItem's
// does, so the transaction itself never blocks on I/O.
func (s *Service) UpdateContentItem(ctx context.Context, req UpdateContentItemRequest, actor domain.ActorRef, correlationID string) (domain.ContentItem, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return domain.ContentItem{}, newValidationError("title", "title is required")
	}

	existing, err := store.GetContentItemByRef(ctx, s.store.DB(), req.Ref)
	if errors.Is(err, store.ErrNotFound) {
		return domain.ContentItem{}, newNotFoundError("content item not found")
	}
	if err != nil {
		return domain.ContentItem{}, fmt.Errorf("service: look up content item: %w", err)
	}
	representation := domain.ContentRepresentation(existing.Entity.Representation)
	fields, err := s.buildContentItemFields(representation, req.Body, req.Content, req.FileName, req.MediaType, req.PathValue, req.URLValue)
	if err != nil {
		return domain.ContentItem{}, err
	}

	var result domain.ContentItem
	var notifiedIDs []int64
	err = s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetContentItemByRef(ctx, tx, req.Ref)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("content item not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up content item: %w", err)
		}
		if row.Entity.Version != req.ExpectedVersion {
			return newVersionConflictError(row.Entity.Version)
		}

		oldFields := contentItemFieldsFromEntity(row)
		if err := store.InsertContentItemVersion(ctx, tx, row.ID, row.Entity.Version, row.Entity.Title, oldFields, actorID, now); err != nil {
			return fmt.Errorf("service: archive content item version: %w", err)
		}

		if _, err := store.UpdateContentItemFields(ctx, tx, row.ID, title, fields, req.ExpectedVersion, now); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				current, cerr := store.CurrentEntityVersion(ctx, tx, row.ID)
				if cerr != nil {
					return fmt.Errorf("service: read current version after conflict: %w", cerr)
				}
				return newVersionConflictError(current)
			}
			return fmt.Errorf("service: update content item: %w", err)
		}
		ids, err := rescanMentions(ctx, tx, row.ID, sourceOwnBody, row.Entity.ProjectKey, fields.Body, now, actorID)
		if err != nil {
			return err
		}
		notifiedIDs = ids

		changes := auditChanges(map[string]any{"title": title})
		if err := store.InsertAuditEvent(ctx, tx, row.ID, actorID, eventContentItemUpdated, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}

		updated, err := store.GetContentItemByRef(ctx, tx, req.Ref)
		if err != nil {
			return fmt.Errorf("service: reload updated content item: %w", err)
		}
		if err := indexContentItemSearchDoc(ctx, tx, row.ID, row.ProjectEntityID, updated.Entity); err != nil {
			return err
		}
		result = updated.Entity
		return nil
	})
	if err != nil {
		return domain.ContentItem{}, err
	}
	s.broadcast(ChangeHint{Kind: HintEntityChanged, Ref: result.Ref, Project: result.ProjectKey})
	s.publishNotified(ctx, notifiedIDs)
	return result, nil
}

// ListContentItemVersions returns a content item's archived prior
// states, oldest first (§5.9). Does not include the live state.
func (s *Service) ListContentItemVersions(ctx context.Context, ref domain.Reference) ([]domain.ContentItemVersion, error) {
	row, err := store.GetContentItemByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("content item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: look up content item: %w", err)
	}
	versions, err := store.ListContentItemVersions(ctx, s.store.DB(), row.ID)
	if err != nil {
		return nil, fmt.Errorf("service: list content item versions: %w", err)
	}
	return versions, nil
}

// ContentItemDiff is GetContentItemDiff's output: a line-level diff of
// the title and Markdown body between two named versions — mirrors
// DecisionDiff, narrowed to the two fields a content item actually has.
type ContentItemDiff struct {
	FromVersion int64
	ToVersion   int64
	Title       []domain.DiffLine
	Body        []domain.DiffLine
}

type contentItemVersionState struct {
	Title, Body string
}

// GetContentItemDiff computes a line-level diff (§5.9: "the UI computes
// a line-level diff between versions") between content item version
// numbers from and to — mirrors GetDecisionDiff. Only meaningful for
// representation="markdown" (Step 3's only representation); Steps 4-5
// don't attempt a diff for file/path/url versions (§5.9: "binary line
// diffs are not attempted").
func (s *Service) GetContentItemDiff(ctx context.Context, ref domain.Reference, from, to int64) (ContentItemDiff, error) {
	row, err := store.GetContentItemByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return ContentItemDiff{}, newNotFoundError("content item not found")
	}
	if err != nil {
		return ContentItemDiff{}, fmt.Errorf("service: look up content item: %w", err)
	}

	fromState, err := s.contentItemStateAtVersion(ctx, row, from)
	if err != nil {
		return ContentItemDiff{}, err
	}
	toState, err := s.contentItemStateAtVersion(ctx, row, to)
	if err != nil {
		return ContentItemDiff{}, err
	}

	return ContentItemDiff{
		FromVersion: from,
		ToVersion:   to,
		Title:       domain.LineDiff(fromState.Title, toState.Title),
		Body:        domain.LineDiff(fromState.Body, toState.Body),
	}, nil
}

func (s *Service) contentItemStateAtVersion(ctx context.Context, row store.ContentItemRow, version int64) (contentItemVersionState, error) {
	if version == row.Entity.Version {
		return contentItemVersionState{Title: row.Entity.Title, Body: row.Entity.Body}, nil
	}
	if version < 1 || version > row.Entity.Version {
		return contentItemVersionState{}, newValidationError("version", "content item %s has no version %d", row.Entity.Ref, version)
	}
	v, err := store.GetContentItemVersion(ctx, s.store.DB(), row.ID, version)
	if errors.Is(err, store.ErrNotFound) {
		return contentItemVersionState{}, newValidationError("version", "content item %s has no version %d", row.Entity.Ref, version)
	}
	if err != nil {
		return contentItemVersionState{}, fmt.Errorf("service: get content item version: %w", err)
	}
	return contentItemVersionState{Title: v.Title, Body: v.Body}, nil
}

// ContentItemDownload is DownloadContentItem/DownloadContentItemVersion's
// result — mirrors AttachmentDownload.
type ContentItemDownload struct {
	Content   io.ReadCloser
	FileName  string
	MediaType string
	FileSize  int64
}

// DownloadContentItem opens a streamed reader for a file-representation
// content item's current bytes — mirrors DownloadAttachment. Returns a
// validation error for any other representation, including "path"
// (ADR 0007's boundary: nothing in this codebase ever opens a path
// representation's target) and "url" (a url representation is a link,
// not downloadable content).
func (s *Service) DownloadContentItem(ctx context.Context, ref domain.Reference) (ContentItemDownload, error) {
	row, err := store.GetContentItemByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return ContentItemDownload{}, newNotFoundError("content item not found")
	}
	if err != nil {
		return ContentItemDownload{}, fmt.Errorf("service: look up content item: %w", err)
	}
	if domain.ContentRepresentation(row.Entity.Representation) != domain.ContentRepresentationFile || row.FileHash == nil {
		return ContentItemDownload{}, newValidationError("representation", "%s has no downloadable content", row.Entity.Ref)
	}
	rc, err := s.blobs.Open(*row.FileHash)
	if err != nil {
		return ContentItemDownload{}, fmt.Errorf("service: open content item blob: %w", err)
	}
	return ContentItemDownload{Content: rc, FileName: row.Entity.FileName, MediaType: row.Entity.MediaType, FileSize: row.Entity.FileSize}, nil
}

// DownloadContentItemVersion is DownloadContentItem for one archived
// version rather than the current state.
func (s *Service) DownloadContentItemVersion(ctx context.Context, ref domain.Reference, version int64) (ContentItemDownload, error) {
	row, err := store.GetContentItemByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return ContentItemDownload{}, newNotFoundError("content item not found")
	}
	if err != nil {
		return ContentItemDownload{}, fmt.Errorf("service: look up content item: %w", err)
	}
	hash, fileName, mediaType, err := store.GetContentItemVersionBlobHash(ctx, s.store.DB(), row.ID, version)
	if errors.Is(err, store.ErrNotFound) {
		return ContentItemDownload{}, newValidationError("version", "%s has no version %d", row.Entity.Ref, version)
	}
	if err != nil {
		return ContentItemDownload{}, fmt.Errorf("service: look up content item version: %w", err)
	}
	if hash == nil {
		return ContentItemDownload{}, newValidationError("representation", "%s version %d has no downloadable content", row.Entity.Ref, version)
	}
	rc, err := s.blobs.Open(*hash)
	if err != nil {
		return ContentItemDownload{}, fmt.Errorf("service: open content item blob: %w", err)
	}
	return ContentItemDownload{Content: rc, FileName: fileName, MediaType: mediaType}, nil
}
