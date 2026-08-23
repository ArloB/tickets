package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// parseFeatureRef mirrors parseTicketRef (tickets.go): a reference
// that parses but names the wrong kind is validation_failed, not a
// generic not_found — the caller supplied well-formed input for the
// wrong resource type, which is a client-fixable mistake, not a
// missing-record condition.
func parseFeatureRef(s string) (domain.Reference, *service.Error) {
	ref, err := domain.Parse(s)
	if err != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if ref.Kind != domain.KindFeature {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a feature reference"}
	}
	return ref, nil
}

type createFeatureRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

func (s *Server) createFeature(w http.ResponseWriter, r *http.Request) {
	projectKey := r.PathValue("key")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createFeatureRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	feature, err := s.svc.CreateFeature(r.Context(), service.CreateFeatureRequest{
		ProjectKey:  projectKey,
		Title:       req.Title,
		Description: req.Description,
		Priority:    domain.Priority(req.Priority),
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toFeatureDetail(feature))
}

// listFeatures supports ?limit=/?cursor=, mirroring listTickets
// (tickets.go) — Phase 3 Step 5 added real pagination here; Phase 1
// shipped this endpoint returning every feature at once.
func (s *Server) listFeatures(w http.ResponseWriter, r *http.Request) {
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

	result, err := s.svc.ListFeatures(r.Context(), projectKey, limit, q.Get("cursor"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]featureCompact, len(result.Features))
	for i, f := range result.Features {
		out[i] = toFeatureCompact(f)
	}
	writeJSON(w, http.StatusOK, featuresPage{Features: out, NextCursor: result.NextCursor})
}

func (s *Server) getFeature(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseFeatureRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	feature, err := s.svc.GetFeature(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toFeatureDetail(feature))
}

type updateFeatureRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

func (s *Server) updateFeature(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseFeatureRef(r.PathValue("ref"))
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
	var req updateFeatureRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	feature, err := s.svc.UpdateFeature(r.Context(), service.UpdateFeatureRequest{
		Ref: ref, Title: req.Title, Description: req.Description, Priority: domain.Priority(req.Priority), ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toFeatureDetail(feature))
}

type reorderFeatureRequest struct {
	AfterRef *string `json:"after_ref"`
}

func (s *Server) reorderFeature(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseFeatureRef(r.PathValue("ref"))
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
	var req reorderFeatureRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	var afterRef *domain.Reference
	if req.AfterRef != nil {
		parsed, svcErr := parseFeatureRef(*req.AfterRef)
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		afterRef = &parsed
	}

	feature, err := s.svc.ReorderFeature(r.Context(), service.ReorderFeatureRequest{
		Ref: ref, AfterRef: afterRef, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toFeatureDetail(feature))
}

func (s *Server) deleteFeature(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseFeatureRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	version, svcErr := parseIfMatch(r)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	cascade := r.URL.Query().Get("cascade") == "true"

	newVersion, err := s.svc.DeleteFeature(r.Context(), service.DeleteFeatureRequest{
		Ref: ref, Cascade: cascade, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteResponse{Version: newVersion})
}

func (s *Server) restoreFeature(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseFeatureRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	version, svcErr := parseIfMatch(r)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	feature, err := s.svc.RestoreFeature(r.Context(), service.RestoreFeatureRequest{
		Ref: ref, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toFeatureDetail(feature))
}
