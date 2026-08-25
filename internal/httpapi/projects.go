package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

type createProjectRequest struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type projectsPage struct {
	Projects   []projectCompact `json:"projects"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createProjectRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	fp, ferr := service.Fingerprint(r.Method, r.URL.Path, body)
	if ferr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: ferr.Error()})
		return
	}

	proj, err := s.svc.CreateProject(r.Context(), service.CreateProjectRequest{
		Key: req.Key, Title: req.Title, Description: req.Description,
	}, requestActor(r), correlationID(r), idempotencyKey(r), fp)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProjectDetail(proj))
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
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
	includeArchived := q.Get("include_archived") == "true"

	result, err := s.svc.ListProjects(r.Context(), limit, q.Get("cursor"), includeArchived)
	if err != nil {
		writeError(w, r, err)
		return
	}
	projects := make([]projectCompact, len(result.Projects))
	for i, p := range result.Projects {
		projects[i] = toProjectCompact(p)
	}
	writeJSON(w, http.StatusOK, projectsPage{Projects: projects, NextCursor: result.NextCursor})
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	proj, err := s.svc.GetProject(r.Context(), key)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProjectDetail(proj))
}

type updateProjectRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// updateProject is PATCH /projects/{key} — title/description only
// (ADR 0021); see updateProjectStatus for archive/unarchive, kept
// separate for the same reason updateFeature and updateFeatureStatus
// are split.
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
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
	var req updateProjectRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	proj, err := s.svc.UpdateProject(r.Context(), service.UpdateProjectRequest{
		Key: key, Title: req.Title, Description: req.Description, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProjectDetail(proj))
}

type updateProjectStatusRequest struct {
	Status string `json:"status"`
}

// updateProjectStatus is POST /projects/{key}/status — archive or
// unarchive, mirroring updateFeatureStatus.
func (s *Server) updateProjectStatus(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
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
	var req updateProjectStatusRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	proj, err := s.svc.SetProjectStatus(r.Context(), service.SetProjectStatusRequest{
		Key: key, NewStatus: domain.ProjectStatus(req.Status), ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProjectDetail(proj))
}
