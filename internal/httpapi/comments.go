package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// parseCommentID parses the {id} path value comment routes share.
// Comments have no formatted reference (domain.Comment's doc explains
// why: no entities-registry row backs them), so this is a plain
// integer parse, not domain.Parse.
func parseCommentID(s string) (int64, *service.Error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, &service.Error{Code: domain.ErrValidationFailed, Field: "id", Message: "comment id must be an integer"}
	}
	return id, nil
}

type addCommentRequest struct {
	Body string `json:"body"`
}

// parseProjectCommentRef parses the {key} path value the
// /projects/{key}/comments routes share into a project-kind reference
// — a project has no seq-numbered public reference the way the other
// five commentable kinds do (domain.Format rejects KindProject), so
// unlike parseFeatureRef/parseDecisionRef/parseContentItemRef this
// takes the raw key directly rather than parsing a formatted
// reference, mirroring getProject's own key handling.
func parseProjectCommentRef(key string) domain.Reference {
	return domain.Reference{ProjectKey: key, Kind: domain.KindProject}
}

// createCommentOnRef is createComment's shared core, called once ref
// has been parsed by whichever kind-specific route matched (Phase 6
// Step 2: comments are no longer ticket-only, §5.10).
func (s *Server) createCommentOnRef(w http.ResponseWriter, r *http.Request, ref domain.Reference) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req addCommentRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	fp, ferr := service.Fingerprint(r.Method, r.URL.Path, body)
	if ferr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: ferr.Error()})
		return
	}

	comment, err := s.svc.AddComment(r.Context(), service.AddCommentRequest{Ref: ref, Body: req.Body}, requestActor(r), correlationID(r), idempotencyKey(r), fp)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCommentDetail(comment))
}

// listCommentsOnRef is listComments' shared core, called once ref has
// been parsed by whichever kind-specific route matched.
func (s *Server) listCommentsOnRef(w http.ResponseWriter, r *http.Request, ref domain.Reference) {
	comments, err := s.svc.ListComments(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]commentDetail, len(comments))
	for i, c := range comments {
		out[i] = toCommentDetail(c)
	}
	writeJSON(w, http.StatusOK, commentsPage{Comments: out})
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.createCommentOnRef(w, r, ref)
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.listCommentsOnRef(w, r, ref)
}

func (s *Server) createFeatureComment(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseFeatureRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.createCommentOnRef(w, r, ref)
}

func (s *Server) listFeatureComments(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseFeatureRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.listCommentsOnRef(w, r, ref)
}

func (s *Server) createDecisionComment(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseDecisionRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.createCommentOnRef(w, r, ref)
}

func (s *Server) listDecisionComments(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseDecisionRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.listCommentsOnRef(w, r, ref)
}

func (s *Server) createPlanComment(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseContentItemRef(domain.KindPlan, r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.createCommentOnRef(w, r, ref)
}

func (s *Server) listPlanComments(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseContentItemRef(domain.KindPlan, r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.listCommentsOnRef(w, r, ref)
}

func (s *Server) createDocumentComment(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseContentItemRef(domain.KindDocument, r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.createCommentOnRef(w, r, ref)
}

func (s *Server) listDocumentComments(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseContentItemRef(domain.KindDocument, r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.listCommentsOnRef(w, r, ref)
}

func (s *Server) createProjectComment(w http.ResponseWriter, r *http.Request) {
	s.createCommentOnRef(w, r, parseProjectCommentRef(r.PathValue("key")))
}

func (s *Server) listProjectComments(w http.ResponseWriter, r *http.Request) {
	s.listCommentsOnRef(w, r, parseProjectCommentRef(r.PathValue("key")))
}

func (s *Server) getComment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseCommentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	comment, err := s.svc.GetComment(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toCommentDetail(comment))
}

func (s *Server) getCommentHistory(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseCommentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	versions, err := s.svc.GetCommentHistory(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]commentVersionEntry, len(versions))
	for i, v := range versions {
		out[i] = toCommentVersionEntry(v)
	}
	writeJSON(w, http.StatusOK, commentHistoryPage{Versions: out})
}

type editCommentRequest struct {
	Body string `json:"body"`
}

func (s *Server) editComment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseCommentID(r.PathValue("id"))
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
	var req editCommentRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	comment, err := s.svc.EditComment(r.Context(), service.EditCommentRequest{
		CommentID: id, Body: req.Body, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toCommentDetail(comment))
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseCommentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	version, svcErr := parseIfMatch(r)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	if err := s.svc.DeleteComment(r.Context(), service.DeleteCommentRequest{
		CommentID: id, ExpectedVersion: version,
	}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
