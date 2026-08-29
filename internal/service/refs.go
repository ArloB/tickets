package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// ResolvedRef is one answer from ResolveRefs: the token as asked
// about, whether it names a live record, and — when it does — enough
// to render a link to it without a second round trip. Title and
// Status are empty for an unresolved token, and Status is empty for
// the two kinds that have none (plans and documents).
type ResolvedRef struct {
	Ref    string
	Kind   string
	Title  string
	Status string
	Exists bool
}

// maxResolveRefs caps one ResolveRefs batch. A renderer resolving the
// references in one Markdown body is the intended caller (ADR 0025);
// bodies with more distinct references than this are rare enough that
// splitting the batch client-side is the right answer, and an
// unbounded batch would turn one request into an unbounded number of
// point reads.
const maxResolveRefs = 50

// ResolveRefs answers, for each reference token, whether it names a
// record that exists — the read behind rendering a reference in
// Markdown prose as a hyperlink. It accepts every token shape
// docs/contracts/references.md defines, plus a bare project key,
// matching what refs.ts's detailRoute can build a route for.
//
// Deliberately best-effort in the same way domain.ScanReferences is,
// and for the same reason: the input is whatever a scan over prose
// turned up, so a malformed token, an unknown kind letter, or a
// well-formed token naming nothing is Exists=false, not an error.
// Only a genuine storage failure returns one. A soft-deleted record
// resolves as absent — the getters used here already exclude deleted
// rows, and a link to a deleted record would 404 on the way out.
//
// Results are deduplicated in first-occurrence order, so callers may
// pass a raw scan without pre-filtering.
func (s *Service) ResolveRefs(ctx context.Context, refs []string) ([]ResolvedRef, error) {
	if len(refs) > maxResolveRefs {
		return nil, newValidationError("refs", "at most %d references may be resolved in one request", maxResolveRefs)
	}

	seen := make(map[string]bool, len(refs))
	out := make([]ResolvedRef, 0, len(refs))
	for _, token := range refs {
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		resolved, err := s.resolveRef(ctx, token)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

func (s *Service) resolveRef(ctx context.Context, token string) (ResolvedRef, error) {
	miss := ResolvedRef{Ref: token}
	db := s.store.DB()

	if domain.ValidProjectKey(token) {
		row, err := store.GetProjectByKey(ctx, db, token)
		if errors.Is(err, store.ErrNotFound) {
			return miss, nil
		}
		if err != nil {
			return miss, fmt.Errorf("service: resolve project ref: %w", err)
		}
		return ResolvedRef{
			Ref: token, Kind: string(domain.KindProject),
			Title: row.Entity.Title, Status: string(row.Entity.Status), Exists: true,
		}, nil
	}

	ref, err := domain.Parse(token)
	if err != nil {
		return miss, nil
	}

	switch ref.Kind {
	case domain.KindTicket:
		row, err := store.GetTicketByRef(ctx, db, ref)
		if err != nil {
			return miss, resolveRefErr(err)
		}
		return ResolvedRef{Ref: token, Kind: string(ref.Kind), Title: row.Entity.Title, Status: string(row.Entity.Status), Exists: true}, nil
	case domain.KindFeature:
		row, err := store.GetFeatureByRef(ctx, db, ref)
		if err != nil {
			return miss, resolveRefErr(err)
		}
		return ResolvedRef{Ref: token, Kind: string(ref.Kind), Title: row.Entity.Title, Status: string(row.Entity.Status), Exists: true}, nil
	case domain.KindDecision:
		row, err := store.GetDecisionByRef(ctx, db, ref)
		if err != nil {
			return miss, resolveRefErr(err)
		}
		return ResolvedRef{Ref: token, Kind: string(ref.Kind), Title: row.Entity.Title, Status: string(row.Entity.Status), Exists: true}, nil
	case domain.KindPlan, domain.KindDocument:
		row, err := store.GetContentItemByRef(ctx, db, ref)
		if err != nil {
			return miss, resolveRefErr(err)
		}
		return ResolvedRef{Ref: token, Kind: string(ref.Kind), Title: row.Entity.Title, Exists: true}, nil
	default:
		return miss, nil
	}
}

// resolveRefErr collapses store.ErrNotFound to "no error, just not
// found" — resolveRef's caller has already built the absent result —
// and wraps anything else as the storage failure it is.
func resolveRefErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("service: resolve ref: %w", err)
}
