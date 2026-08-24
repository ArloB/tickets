package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// ExternalLink is one named external link as returned to callers
// (product spec §5.11's "named external links" half — uploaded/path
// attachments are Phase 5). ID is opaque beyond being what
// RemoveExternalLink's caller supplies back.
type ExternalLink struct {
	ID    int64
	Title string
	URL   string
}

// AddExternalLinkRequest is AddExternalLink's input. Ref may be a
// ticket, feature, or decision reference — the same three kinds
// resolveAssociationEndpoint (association.go) already resolves, reused
// here rather than duplicated.
type AddExternalLinkRequest struct {
	Ref   domain.Reference
	Title string
	URL   string
}

// AddExternalLink attaches a named external link to ref. No version
// column and no in-place edit (docs/contracts/concurrency.md) — change
// a link by removing and re-adding it.
func (s *Service) AddExternalLink(ctx context.Context, req AddExternalLinkRequest, actor domain.ActorRef, correlationID string) (ExternalLink, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return ExternalLink{}, newValidationError("title", "title is required")
	}
	if !domain.ValidateLinkURL(req.URL) {
		return ExternalLink{}, newValidationError("url", "url must be an http(s) or mailto URL")
	}

	var out ExternalLink
	err := s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		endpoint, err := resolveAssociationEndpoint(ctx, tx, "ref", req.Ref)
		if err != nil {
			return err
		}
		id, err := store.InsertExternalLink(ctx, tx, endpoint.EntityID, title, req.URL, actorID, now)
		if err != nil {
			return fmt.Errorf("service: insert external link: %w", err)
		}
		projectID, err := store.EntityProjectID(ctx, tx, endpoint.EntityID)
		if err != nil {
			return fmt.Errorf("service: resolve link owner project for indexing: %w", err)
		}
		if err := indexLinkSearchDoc(ctx, tx, id, endpoint.EntityID, projectID, endpoint.Ref, title, req.URL); err != nil {
			return err
		}
		changes := auditChanges(map[string]any{"title": title, "url": req.URL})
		if err := store.InsertAuditEvent(ctx, tx, endpoint.EntityID, actorID, eventExternalLinkAdded, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		out = ExternalLink{ID: id, Title: title, URL: req.URL}
		return nil
	})
	if err != nil {
		return ExternalLink{}, err
	}
	return out, nil
}

// RemoveExternalLinkRequest is RemoveExternalLink's input.
type RemoveExternalLinkRequest struct {
	Ref    domain.Reference
	LinkID int64
}

// RemoveExternalLink deletes one link, scoped to ref's entity so a
// caller cannot delete a link belonging to a different entity by
// guessing/reusing another entity's link id.
func (s *Service) RemoveExternalLink(ctx context.Context, req RemoveExternalLinkRequest, actor domain.ActorRef, correlationID string) error {
	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		endpoint, err := resolveAssociationEndpoint(ctx, tx, "ref", req.Ref)
		if err != nil {
			return err
		}
		deleted, err := store.DeleteExternalLink(ctx, tx, endpoint.EntityID, req.LinkID)
		if err != nil {
			return fmt.Errorf("service: delete external link: %w", err)
		}
		if !deleted {
			return newNotFoundError("link not found")
		}
		if err := store.DeleteSearchDocumentForLink(ctx, tx, req.LinkID); err != nil {
			return fmt.Errorf("service: remove link search document: %w", err)
		}
		changes := auditChanges(map[string]any{"link_id": req.LinkID})
		if err := store.InsertAuditEvent(ctx, tx, endpoint.EntityID, actorID, eventExternalLinkRemoved, corrID, nil, changes, now); err != nil {
			return fmt.Errorf("service: record audit event: %w", err)
		}
		return nil
	})
}

// GetExternalLinks returns ref's links, oldest first.
func (s *Service) GetExternalLinks(ctx context.Context, ref domain.Reference) ([]ExternalLink, error) {
	endpoint, err := resolveAssociationEndpoint(ctx, s.store.DB(), "ref", ref)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListExternalLinks(ctx, s.store.DB(), endpoint.EntityID)
	if err != nil {
		return nil, fmt.Errorf("service: list external links: %w", err)
	}
	out := make([]ExternalLink, len(rows))
	for i, row := range rows {
		out[i] = ExternalLink{ID: row.ID, Title: row.Title, URL: row.URL}
	}
	return out, nil
}
