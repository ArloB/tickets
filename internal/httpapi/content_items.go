package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// parseContentItemRef mirrors parseDecisionRef: a reference that parses
// but names the wrong kind is validation_failed, not a generic
// not_found. kind is the specific kind this route promises (KindPlan
// for every /plans/... route, KindDocument for every /documents/...
// route) — not just "some content item kind" — so GET/PATCH
// /documents/{ref} can't be used to read or write a plan by passing its
// ABC-P1 ref (and vice versa). A single generic parseContentItemRef
// accepting either kind would let a plan ref succeed against a
// /documents/... route purely because both happen to resolve through
// the same content_items table; binding kind per route closes that.
func parseContentItemRef(kind domain.EntityKind, s string) (domain.Reference, *service.Error) {
	ref, err := domain.Parse(s)
	if err != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if ref.Kind != kind {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a " + string(kind) + " reference"}
	}
	return ref, nil
}

type createContentItemRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// createContentItem returns a handler bound to kind (KindPlan or
// KindDocument) — the create route's URL prefix (/plans vs /documents)
// already says which, so the kind travels as a closure argument rather
// than a request field, matching plan.md Step 3's routing design.
func (s *Server) createContentItem(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectKey := r.PathValue("key")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
			return
		}
		var req createContentItemRequest
		if svcErr := decodeJSON(body, &req); svcErr != nil {
			writeError(w, r, svcErr)
			return
		}

		fp, ferr := service.Fingerprint(r.Method, r.URL.Path, body)
		if ferr != nil {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: ferr.Error()})
			return
		}

		item, err := s.svc.CreateContentItem(r.Context(), service.CreateContentItemRequest{
			ProjectKey: projectKey, Kind: kind, Title: req.Title, Body: req.Body,
		}, requestActor(r), correlationID(r), idempotencyKey(r), fp)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, toContentItemDetail(item))
	}
}

func (s *Server) listContentItems(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectKey := r.PathValue("key")
		q := r.URL.Query()

		limit := 0
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "limit", Message: "limit must be a non-negative integer"})
				return
			}
			limit = n
		}

		result, err := s.svc.ListContentItems(r.Context(), projectKey, kind, limit, q.Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		out := make([]contentItemCompact, len(result.Items))
		for i, item := range result.Items {
			out[i] = toContentItemCompact(item)
		}
		writeJSON(w, http.StatusOK, contentItemsPage{Items: out, NextCursor: result.NextCursor})
	}
}

func (s *Server) getContentItem(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		item, err := s.svc.GetContentItem(r.Context(), ref)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toContentItemDetail(item))
	}
}

type updateContentItemRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *Server) updateContentItem(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		version, svcErr := parseIfMatch(r)
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
			return
		}
		var req updateContentItemRequest
		if svcErr := decodeJSON(body, &req); svcErr != nil {
			writeError(w, r, svcErr)
			return
		}

		item, err := s.svc.UpdateContentItem(r.Context(), service.UpdateContentItemRequest{
			Ref: ref, Title: req.Title, Body: req.Body, ExpectedVersion: version,
		}, requestActor(r), correlationID(r))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toContentItemDetail(item))
	}
}

func (s *Server) listContentItemVersions(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		versions, err := s.svc.ListContentItemVersions(r.Context(), ref)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out := make([]contentItemVersionEntry, len(versions))
		for i, v := range versions {
			out[i] = toContentItemVersionEntry(v)
		}
		writeJSON(w, http.StatusOK, contentItemVersionsPage{Versions: out})
	}
}

func (s *Server) getContentItemDiff(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		q := r.URL.Query()
		from, svcErr := parseDiffVersionParam(q.Get("from"), "from")
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		to, svcErr := parseDiffVersionParam(q.Get("to"), "to")
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}

		diff, err := s.svc.GetContentItemDiff(r.Context(), ref, from, to)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toContentItemDiff(diff))
	}
}
