package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// AddCommentRequest is AddComment's input. Ref names any of the six
// commentable kinds §5.10 names: project, feature, ticket, decision,
// plan, or document (Phase 6 Step 2 — previously ticket-only, per this
// field's now-outdated original doc, though comments.entity_id has
// always referenced entities(id) generically; the restriction was a
// service-layer choice, not a schema one).
type AddCommentRequest struct {
	Ref  domain.Reference
	Body string
}

// commentOwner is a resolved comment target — any of the six kinds
// §5.10 names. EntityID/ProjectEntityID feed indexCommentSearchDoc and
// notify's project-scoped resolution; Ref and ProjectKey feed
// rescanMentions' reference scanner and the ChangeHint broadcast. A
// project's Ref is its own Key, never a formatted reference — a
// project has no seq-numbered public reference the way the other five
// kinds do (domain.Format rejects KindProject), matching
// CreateProject's own ChangeHint convention (internal/service/project.go).
type commentOwner struct {
	EntityID        int64
	ProjectEntityID int64
	Ref             string
	ProjectKey      string
}

// resolveCommentOwner resolves any of the six commentable kinds from a
// caller-supplied reference. Delegates to resolveAssociationEndpoint
// for the five kinds it already resolves (ticket/feature/decision/
// plan/document) and adds project support, which
// resolveAssociationEndpoint deliberately excludes: an association is
// project *content*, not a property of the project itself
// (domain.ValidAssociationKind's doc) — a restriction that has no
// bearing on comments, which §5.10 lists projects as receiving
// directly.
func resolveCommentOwner(ctx context.Context, q store.Querier, ref domain.Reference) (commentOwner, error) {
	if ref.Kind == domain.KindProject {
		row, err := store.GetProjectByKey(ctx, q, ref.ProjectKey)
		if errors.Is(err, store.ErrNotFound) {
			return commentOwner{}, newNotFoundError("project not found")
		}
		if err != nil {
			return commentOwner{}, fmt.Errorf("service: look up project: %w", err)
		}
		return commentOwner{EntityID: row.ID, ProjectEntityID: row.ID, Ref: row.Entity.Key, ProjectKey: row.Entity.Key}, nil
	}
	endpoint, err := resolveAssociationEndpoint(ctx, q, "ref", ref)
	if err != nil {
		return commentOwner{}, err
	}
	projectEntityID, err := store.EntityProjectID(ctx, q, endpoint.EntityID)
	if err != nil {
		return commentOwner{}, fmt.Errorf("service: resolve %s's project: %w", ref.Kind, err)
	}
	return commentOwner{EntityID: endpoint.EntityID, ProjectEntityID: projectEntityID, Ref: endpoint.Ref, ProjectKey: ref.ProjectKey}, nil
}

// resolveCommentOwnerByEntityID is resolveCommentOwner's counterpart
// for EditComment/DeleteComment, which only have the comment's
// already-resolved owning entity id (row.EntityID), not the caller-
// supplied reference — mirrors mentionTargetRef's kind-dispatch
// pattern (internal/service/mentions.go) with project support added.
func resolveCommentOwnerByEntityID(ctx context.Context, q store.Querier, entityID int64) (commentOwner, error) {
	kind, err := store.GetEntityKindByID(ctx, q, entityID)
	if errors.Is(err, store.ErrNotFound) {
		return commentOwner{}, newNotFoundError("comment's owning entity not found")
	}
	if err != nil {
		return commentOwner{}, fmt.Errorf("service: resolve comment owner kind: %w", err)
	}
	if kind == domain.KindProject {
		key, err := store.GetProjectKeyByEntityID(ctx, q, entityID)
		if err != nil {
			return commentOwner{}, fmt.Errorf("service: resolve project key: %w", err)
		}
		return commentOwner{EntityID: entityID, ProjectEntityID: entityID, Ref: key, ProjectKey: key}, nil
	}

	var ref domain.Reference
	switch kind {
	case domain.KindTicket:
		ref, err = store.GetTicketRefByEntityID(ctx, q, entityID)
	case domain.KindFeature:
		ref, err = store.GetFeatureRefByEntityID(ctx, q, entityID)
	case domain.KindDecision:
		ref, err = store.GetDecisionRefByEntityID(ctx, q, entityID)
	case domain.KindPlan, domain.KindDocument:
		ref, err = store.GetContentItemRefByEntityID(ctx, q, entityID)
	default:
		return commentOwner{}, fmt.Errorf("service: unexpected comment owner kind %q", kind)
	}
	if err != nil {
		return commentOwner{}, fmt.Errorf("service: resolve comment owner ref: %w", err)
	}
	formatted, err := domain.Format(ref)
	if err != nil {
		return commentOwner{}, fmt.Errorf("service: format comment owner ref: %w", err)
	}
	projectEntityID, err := store.EntityProjectID(ctx, q, entityID)
	if err != nil {
		return commentOwner{}, fmt.Errorf("service: resolve comment owner's project: %w", err)
	}
	return commentOwner{EntityID: entityID, ProjectEntityID: projectEntityID, Ref: formatted, ProjectKey: ref.ProjectKey}, nil
}

// AddComment adds a comment to a ticket and scans its body for
// references in the same transaction (ADR 0015). idemKey/fingerprint
// are Phase 3's addition: Phase 1's doc originally skipped this,
// reasoning that nothing yet called AddComment over the network and
// needed retry-safety — Phase 3's CLI/MCP callers are exactly that
// caller, so the cached ref_key here is the comment's own id
// (formatted as a decimal string, since comments have no public
// reference the way tickets/projects do) rather than a domain
// reference.
func (s *Service) AddComment(ctx context.Context, req AddCommentRequest, actor domain.ActorRef, correlationID, idemKey, fingerprint string) (domain.Comment, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return domain.Comment{}, newValidationError("body", "body is required")
	}

	var commentID int64
	var hint ChangeHint
	var notifiedIDs []int64
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if cached, found, err := checkIdempotency(ctx, tx, idemKey, actorID, fingerprint); err != nil {
			return err
		} else if found {
			id, perr := strconv.ParseInt(cached, 10, 64)
			if perr != nil {
				return fmt.Errorf("service: parse cached comment id %q: %w", cached, perr)
			}
			commentID = id
			return nil // no writes happened on this path; committing is a no-op
		}

		owner, err := resolveCommentOwner(ctx, tx, req.Ref)
		if err != nil {
			return err
		}

		// Loaded before this comment's own auto-subscribe below so the
		// commenter can never appear in their own "commented" fan-out —
		// though notify() also self-excludes by actor id regardless, so
		// this ordering documents the intent rather than being load-
		// bearing on its own.
		subscriberIDs, err := store.ListSubscriberActorIDs(ctx, tx, owner.EntityID)
		if err != nil {
			return fmt.Errorf("service: list subscribers: %w", err)
		}

		id, err := store.InsertComment(ctx, tx, owner.EntityID, actorID, body, now)
		if err != nil {
			return fmt.Errorf("service: insert comment: %w", err)
		}
		commentID = id

		mentioned, err := rescanMentions(ctx, tx, owner.EntityID, commentID, owner.ProjectKey, body, now, actorID)
		if err != nil {
			return err
		}
		if err := indexCommentSearchDoc(ctx, tx, commentID, owner.EntityID, owner.ProjectEntityID, owner.Ref, body); err != nil {
			return err
		}
		if err := subscribe(ctx, tx, owner.EntityID, actorID, now); err != nil {
			return err
		}
		notifiedIDs = mentioned
		for _, recipientID := range subscriberIDs {
			ok, err := notify(ctx, tx, recipientID, notificationKindCommented, owner.EntityID, commentID, actorID, now)
			if err != nil {
				return err
			}
			if ok {
				notifiedIDs = append(notifiedIDs, recipientID)
			}
		}
		hint = ChangeHint{Kind: HintEntityChanged, Ref: owner.Ref, Project: owner.ProjectKey}

		cid := commentID
		changes := auditChanges(map[string]any{"comment_id": commentID})
		if err := store.InsertAuditEvent(ctx, tx, owner.EntityID, actorID, eventCommentAdded, corrID, &cid, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		if err := recordIdempotency(ctx, tx, idemKey, actorID, fingerprint, strconv.FormatInt(commentID, 10), now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domain.Comment{}, err
	}
	s.broadcast(hint)
	s.publishNotified(ctx, notifiedIDs)
	return s.GetComment(ctx, commentID)
}

// GetComment looks up a comment by its own id — tombstones (soft-
// deleted comments) are returned, not hidden, so a caller can render
// the visible-tombstone UI §5.10 calls for.
func (s *Service) GetComment(ctx context.Context, commentID int64) (domain.Comment, error) {
	row, err := store.GetComment(ctx, s.store.DB(), commentID)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Comment{}, newNotFoundError("comment not found")
	}
	if err != nil {
		return domain.Comment{}, fmt.Errorf("service: get comment: %w", err)
	}
	return row.Entity, nil
}

// ListComments returns every comment on any of the six commentable
// kinds (§5.10), oldest first, tombstones included.
func (s *Service) ListComments(ctx context.Context, ref domain.Reference) ([]domain.Comment, error) {
	owner, err := resolveCommentOwner(ctx, s.store.DB(), ref)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListCommentsForEntity(ctx, s.store.DB(), owner.EntityID)
	if err != nil {
		return nil, fmt.Errorf("service: list comments: %w", err)
	}
	out := make([]domain.Comment, len(rows))
	for i, row := range rows {
		out[i] = row.Entity
	}
	return out, nil
}

// GetCommentHistory returns a comment's archived prior bodies, oldest
// first (§5.10: "comment edits create versions and remain visible in
// the audit trail").
func (s *Service) GetCommentHistory(ctx context.Context, commentID int64) ([]domain.CommentVersion, error) {
	if _, err := s.GetComment(ctx, commentID); err != nil {
		return nil, err
	}
	versions, err := store.ListCommentVersions(ctx, s.store.DB(), commentID)
	if err != nil {
		return nil, fmt.Errorf("service: list comment history: %w", err)
	}
	return versions, nil
}

// EditCommentRequest is EditComment's input.
type EditCommentRequest struct {
	CommentID       int64
	Body            string
	ExpectedVersion int64
}

// EditComment archives the comment's current body into
// comment_versions, writes the new body, and rescans mentions — all
// in one transaction. A soft-deleted comment cannot be edited (there
// is no live content to change): not_found, the same as if it never
// existed, since a caller has no way to distinguish "gone" from
// "tombstoned" without first calling GetComment.
func (s *Service) EditComment(ctx context.Context, req EditCommentRequest, actor domain.ActorRef, correlationID string) (domain.Comment, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return domain.Comment{}, newValidationError("body", "body is required")
	}

	var hint ChangeHint
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetComment(ctx, tx, req.CommentID)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("comment not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up comment: %w", err)
		}
		if row.Entity.DeletedAt != nil {
			return newNotFoundError("comment not found")
		}
		if row.Entity.Version != req.ExpectedVersion {
			return newVersionConflictError(row.Entity.Version)
		}

		if err := store.InsertCommentVersion(ctx, tx, req.CommentID, row.Entity.Version, row.Entity.Body, actorID, now); err != nil {
			return fmt.Errorf("service: archive comment version: %w", err)
		}
		if _, err := store.UpdateCommentBody(ctx, tx, req.CommentID, body, req.ExpectedVersion, now); err != nil {
			// The version equality check above already guarantees this
			// succeeds inside the same BEGIN IMMEDIATE transaction (ADR
			// 0003: nothing else can have written this row concurrently) —
			// reaching this branch means something unexpected happened.
			return fmt.Errorf("service: update comment body: %w", err)
		}

		owner, err := resolveCommentOwnerByEntityID(ctx, tx, row.EntityID)
		if err != nil {
			return fmt.Errorf("service: resolve comment's owner: %w", err)
		}
		if _, err := rescanMentions(ctx, tx, row.EntityID, req.CommentID, owner.ProjectKey, body, now, actorID); err != nil {
			return err
		}
		if err := indexCommentSearchDoc(ctx, tx, req.CommentID, row.EntityID, owner.ProjectEntityID, owner.Ref, body); err != nil {
			return err
		}
		hint = ChangeHint{Kind: HintEntityChanged, Ref: owner.Ref, Project: owner.ProjectKey}

		cid := req.CommentID
		changes := auditChanges(map[string]any{"comment_id": req.CommentID})
		if err := store.InsertAuditEvent(ctx, tx, row.EntityID, actorID, eventCommentEdited, corrID, &cid, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Comment{}, err
	}
	s.broadcast(hint)
	return s.GetComment(ctx, req.CommentID)
}

// DeleteCommentRequest is DeleteComment's input.
type DeleteCommentRequest struct {
	CommentID       int64
	ExpectedVersion int64
}

// DeleteComment soft-deletes a comment (the tombstone stays visible,
// §5.10) and clears the mention edges its body created — a
// tombstoned comment's backlinks shouldn't stay live, and there is no
// comment restore in the plan, so deleting beats filtering here
// (contrast with a soft-deleted ticket/feature, where mentions are
// filtered at read time instead, since those can be restored).
func (s *Service) DeleteComment(ctx context.Context, req DeleteCommentRequest, actor domain.ActorRef, correlationID string) error {
	var hint ChangeHint
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		row, err := store.GetComment(ctx, tx, req.CommentID)
		if errors.Is(err, store.ErrNotFound) {
			return newNotFoundError("comment not found")
		}
		if err != nil {
			return fmt.Errorf("service: look up comment: %w", err)
		}
		if row.Entity.DeletedAt != nil {
			return newNotFoundError("comment not found")
		}
		if row.Entity.Version != req.ExpectedVersion {
			return newVersionConflictError(row.Entity.Version)
		}

		if err := store.SoftDeleteComment(ctx, tx, req.CommentID, req.ExpectedVersion, now); err != nil {
			return fmt.Errorf("service: soft-delete comment: %w", err)
		}
		if err := store.DeleteMentionsFromSource(ctx, tx, row.EntityID, req.CommentID); err != nil {
			return fmt.Errorf("service: clear comment's mentions: %w", err)
		}
		if err := store.DeleteSearchDocumentForComment(ctx, tx, req.CommentID); err != nil {
			return fmt.Errorf("service: remove comment search document: %w", err)
		}

		owner, err := resolveCommentOwnerByEntityID(ctx, tx, row.EntityID)
		if err != nil {
			return fmt.Errorf("service: resolve comment's owner: %w", err)
		}
		hint = ChangeHint{Kind: HintEntityChanged, Ref: owner.Ref, Project: owner.ProjectKey}

		cid := req.CommentID
		changes := auditChanges(map[string]any{"comment_id": req.CommentID})
		if err := store.InsertAuditEvent(ctx, tx, row.EntityID, actorID, eventCommentDeleted, corrID, &cid, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.broadcast(hint)
	return nil
}
