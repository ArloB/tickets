package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// parseDecisionRef mirrors parseFeatureRef/parseTicketRef: a reference
// that parses but names the wrong kind is validation_failed, not a
// generic not_found.
func parseDecisionRef(s string) (domain.Reference, *service.Error) {
	ref, err := domain.Parse(s)
	if err != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if ref.Kind != domain.KindDecision {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a decision reference"}
	}
	return ref, nil
}

type createDecisionRequest struct {
	Title        string `json:"title"`
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Rationale    string `json:"rationale"`
	Consequences string `json:"consequences"`
}

func (s *Server) createDecision(w http.ResponseWriter, r *http.Request) {
	projectKey := r.PathValue("key")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createDecisionRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	fp, ferr := service.Fingerprint(r.Method, r.URL.Path, body)
	if ferr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: ferr.Error()})
		return
	}

	decision, err := s.svc.CreateDecision(r.Context(), service.CreateDecisionRequest{
		ProjectKey: projectKey, Title: req.Title, Context: req.Context, Decision: req.Decision, Rationale: req.Rationale,
		Consequences: req.Consequences,
	}, requestActor(r), correlationID(r), idempotencyKey(r), fp)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDecisionDetail(decision))
}

func (s *Server) listDecisions(w http.ResponseWriter, r *http.Request) {
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

	result, err := s.svc.ListDecisions(r.Context(), projectKey, limit, q.Get("cursor"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]decisionCompact, len(result.Decisions))
	for i, d := range result.Decisions {
		out[i] = toDecisionCompact(d)
	}
	writeJSON(w, http.StatusOK, decisionsPage{Decisions: out, NextCursor: result.NextCursor})
}

func (s *Server) getDecision(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseDecisionRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	decision, err := s.svc.GetDecision(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDecisionDetail(decision))
}

type updateDecisionRequest struct {
	Title        string `json:"title"`
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Rationale    string `json:"rationale"`
	Consequences string `json:"consequences"`
	Status       string `json:"status"`
	SupersededBy string `json:"superseded_by"`
}

func (s *Server) updateDecision(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseDecisionRef(r.PathValue("ref"))
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
	var req updateDecisionRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	decision, err := s.svc.UpdateDecision(r.Context(), service.UpdateDecisionRequest{
		Ref: ref, Title: req.Title, Context: req.Context, Decision: req.Decision, Rationale: req.Rationale,
		Consequences: req.Consequences, Status: domain.DecisionStatus(req.Status), SupersededBy: req.SupersededBy,
		ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDecisionDetail(decision))
}

func (s *Server) listDecisionVersions(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseDecisionRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	versions, err := s.svc.ListDecisionVersions(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]decisionVersionEntry, len(versions))
	for i, v := range versions {
		out[i] = toDecisionVersionEntry(v)
	}
	writeJSON(w, http.StatusOK, decisionVersionsPage{Versions: out})
}

func parseDiffVersionParam(q, field string) (int64, *service.Error) {
	n, err := strconv.ParseInt(q, 10, 64)
	if err != nil || n < 1 {
		return 0, &service.Error{Code: domain.ErrValidationFailed, Field: field, Message: field + " must be a positive integer"}
	}
	return n, nil
}

func (s *Server) getDecisionDiff(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseDecisionRef(r.PathValue("ref"))
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

	diff, err := s.svc.GetDecisionDiff(r.Context(), ref, from, to)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDecisionDiff(diff))
}
