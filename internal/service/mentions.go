package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// sourceOwnBody is derived_mentions.source_comment_id's sentinel for
// "the source entity's own Markdown body" (migration
// 0002_core_domain.sql: NOT NULL DEFAULT 0, never a real comments.id
// since that column AUTOINCREMENTs from 1).
const sourceOwnBody = 0

// rescanMentions deletes and reinserts derived_mentions rows for one
// (sourceEntityID, sourceCommentID) body from the Markdown references
// domain.ScanReferences finds in body (ADR 0015, product spec §5.2),
// and — since ADR 0019 — does the same delete-and-reinsert pass for
// actor_mentions from domain.ScanActorMentions, emitting a "mentioned"
// notification for each newly-mentioned actor. One function, two
// scans, in the same transaction as the body write both describe,
// rather than a parallel rescanActorMentions called from every one of
// this function's own call sites a second time. scopeProjectKey
// resolves the project-scoped #123 short form — pass the project the
// body's owning entity belongs to. actorID is who wrote this body
// (the withTx closure's own resolved actor), needed to suppress a
// self-mention notification and to attribute the ones that fire.
//
// A well-formed reference to a specific record (or actor) that
// doesn't exist is silently skipped, not an error — "well-formed but
// unresolvable references are simply not stored as edges" (the Phase
// 1 plan's Step 4). A self-mention is also skipped for both scans;
// neither table's primary key rejects a self-loop on its own, so this
// is a real guard, not defensive insurance.
//
// Returns the actor ids notified (the "mentioned" recipients) so the
// caller's outer method can fold them into its own post-commit
// publishNotified call — see notify's doc for why.
func rescanMentions(ctx context.Context, tx *sql.Tx, sourceEntityID, sourceCommentID int64, scopeProjectKey, body, now string, actorID int64) ([]int64, error) {
	if err := store.DeleteMentionsFromSource(ctx, tx, sourceEntityID, sourceCommentID); err != nil {
		return nil, fmt.Errorf("service: clear mentions: %w", err)
	}
	for _, ref := range domain.ScanReferences(body, scopeProjectKey) {
		targetID, err := resolveMentionTarget(ctx, tx, ref)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("service: resolve mention: %w", err)
		}
		if targetID == sourceEntityID {
			continue
		}
		if err := store.InsertDerivedMention(ctx, tx, sourceEntityID, sourceCommentID, targetID, now); err != nil {
			return nil, fmt.Errorf("service: insert mention: %w", err)
		}
	}

	if err := store.DeleteActorMentionsFromSource(ctx, tx, sourceEntityID, sourceCommentID); err != nil {
		return nil, fmt.Errorf("service: clear actor mentions: %w", err)
	}
	var notified []int64
	for _, actorRef := range domain.ScanActorMentions(body) {
		mentionedID, err := store.GetActorIDByRef(ctx, tx, actorRef.Kind, actorRef.Name)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("service: resolve actor mention: %w", err)
		}
		if mentionedID == actorID {
			continue
		}
		if err := store.InsertActorMention(ctx, tx, sourceEntityID, sourceCommentID, mentionedID, now); err != nil {
			return nil, fmt.Errorf("service: insert actor mention: %w", err)
		}
		ok, err := notify(ctx, tx, mentionedID, notificationKindMentioned, sourceEntityID, sourceCommentID, actorID, now)
		if err != nil {
			return nil, err
		}
		if ok {
			notified = append(notified, mentionedID)
		}
	}
	return notified, nil
}

// resolveMentionTarget resolves a scanned reference to its internal
// entity id, or store.ErrNotFound for an unresolvable kind (KindProject
// has no reference token to scan in the first place) or a specific
// record that doesn't exist. Takes store.Querier, not *sql.Tx: called
// both from rescanMentions (inside a write transaction) and, since
// ADR 0019, from Service.Subscribe's read-only resolution path — an
// interface-typed parameter is what lets one function serve both.
func resolveMentionTarget(ctx context.Context, tx store.Querier, ref domain.Reference) (int64, error) {
	switch ref.Kind {
	case domain.KindTicket:
		row, err := store.GetTicketByRef(ctx, tx, ref)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	case domain.KindFeature:
		row, err := store.GetFeatureByRef(ctx, tx, ref)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	case domain.KindDecision:
		row, err := store.GetDecisionByRef(ctx, tx, ref)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	case domain.KindPlan, domain.KindDocument:
		row, err := store.GetContentItemByRef(ctx, tx, ref)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	default:
		return 0, store.ErrNotFound
	}
}

// mentionTargetRef resolves a mention edge's bare target entity id
// back to a public reference, dispatching on the target's kind — a
// mention target is a ticket, feature, decision, plan, or document, the
// only kinds resolveMentionTarget can ever have stored.
func mentionTargetRef(ctx context.Context, q store.Querier, entityID int64) (domain.Reference, error) {
	kind, err := store.GetEntityKindByID(ctx, q, entityID)
	if err != nil {
		return domain.Reference{}, err
	}
	switch kind {
	case domain.KindTicket:
		return store.GetTicketRefByEntityID(ctx, q, entityID)
	case domain.KindFeature:
		return store.GetFeatureRefByEntityID(ctx, q, entityID)
	case domain.KindDecision:
		return store.GetDecisionRefByEntityID(ctx, q, entityID)
	case domain.KindPlan, domain.KindDocument:
		return store.GetContentItemRefByEntityID(ctx, q, entityID)
	default:
		return domain.Reference{}, fmt.Errorf("service: unexpected mention target kind %q", kind)
	}
}

// mentionSourceRefString resolves a mention edge's source entity id to
// its formatted public reference, as a string. A mention's source can
// now be a project's own comment (Phase 6 Step 2: comments exist on
// all six principal kinds, and a project comment's body is scanned by
// rescanMentions the same as any other), which mentionTargetRef cannot
// return — a project has no seq-numbered reference token
// (domain.Format rejects KindProject; see its doc), so this returns
// the project's bare key instead of delegating to mentionTargetRef for
// that one kind, string-formatting the other five via domain.Format.
func mentionSourceRefString(ctx context.Context, q store.Querier, entityID int64) (string, error) {
	kind, err := store.GetEntityKindByID(ctx, q, entityID)
	if err != nil {
		return "", err
	}
	if kind == domain.KindProject {
		return store.GetProjectKeyByEntityID(ctx, q, entityID)
	}
	ref, err := mentionTargetRef(ctx, q, entityID)
	if err != nil {
		return "", err
	}
	return domain.Format(ref)
}

// GetTicketMentions returns the current outgoing mention targets of a
// ticket's description, as public references — for tests (gate 7) and
// any future caller that wants to show a ticket's mentions.
func (s *Service) GetTicketMentions(ctx context.Context, ref domain.Reference) ([]domain.Reference, error) {
	row, err := store.GetTicketByRef(ctx, s.store.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("ticket not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: get ticket: %w", err)
	}
	return s.resolveMentionRefs(ctx, row.ID, sourceOwnBody)
}

// GetCommentMentions is GetTicketMentions' counterpart for a
// comment's body.
func (s *Service) GetCommentMentions(ctx context.Context, commentID int64) ([]domain.Reference, error) {
	row, err := store.GetComment(ctx, s.store.DB(), commentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newNotFoundError("comment not found")
	}
	if err != nil {
		return nil, fmt.Errorf("service: get comment: %w", err)
	}
	return s.resolveMentionRefs(ctx, row.EntityID, commentID)
}

// Backlink is one reverse-mention edge to the entity GetBacklinks was
// asked about: an entity (and, when the mention came from a comment
// rather than the entity's own body, that comment's id) that
// currently mentions it. SourceCommentID is 0 for "the source
// entity's own body" — mirroring store.MentionSourceRow's sentinel —
// so a caller can tell "mentioned in ABC-124's description" from
// "mentioned in a comment on ABC-124."
type Backlink struct {
	// SourceRef is already the formatted public reference (or a bare
	// project key — Phase 6 Step 2) — a domain.Reference can't
	// represent a project source (domain.Format rejects KindProject),
	// so this is resolved to a string once here rather than pushing
	// that special case onto every caller.
	SourceRef       string
	SourceCommentID int64
}

// GetBacklinks returns every entity/comment currently mentioning ref,
// resolved to public references (product spec §6.1: "View backlinks
// generated from Markdown references"). Read-only — derived mentions
// are already computed automatically on every body/comment write
// (rescanMentions above); this only exposes the already-computed
// reverse edge for reading, no new write surface.
func (s *Service) GetBacklinks(ctx context.Context, ref domain.Reference) ([]Backlink, error) {
	endpoint, err := resolveAssociationEndpoint(ctx, s.store.DB(), "ref", ref)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListMentionSourcesToTarget(ctx, s.store.DB(), endpoint.EntityID)
	if err != nil {
		return nil, fmt.Errorf("service: list mention sources: %w", err)
	}
	out := make([]Backlink, len(rows))
	for i, row := range rows {
		srcRef, err := mentionSourceRefString(ctx, s.store.DB(), row.SourceEntityID)
		if err != nil {
			return nil, fmt.Errorf("service: resolve mention source: %w", err)
		}
		out[i] = Backlink{SourceRef: srcRef, SourceCommentID: row.SourceCommentID}
	}
	return out, nil
}

func (s *Service) resolveMentionRefs(ctx context.Context, sourceEntityID, sourceCommentID int64) ([]domain.Reference, error) {
	targetIDs, err := store.ListMentionTargetsFromSource(ctx, s.store.DB(), sourceEntityID, sourceCommentID)
	if err != nil {
		return nil, fmt.Errorf("service: list mentions: %w", err)
	}
	out := make([]domain.Reference, 0, len(targetIDs))
	for _, id := range targetIDs {
		ref, err := mentionTargetRef(ctx, s.store.DB(), id)
		if err != nil {
			return nil, fmt.Errorf("service: resolve mention target: %w", err)
		}
		out = append(out, ref)
	}
	return out, nil
}
