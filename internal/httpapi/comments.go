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

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

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

	comment, err := s.svc.AddComment(r.Context(), service.AddCommentRequest{Ref: ref, Body: req.Body}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCommentDetail(comment))
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
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
