package httpapi

import (
	"io"
	"net/http"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

type createTicketRequest struct {
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	Severity    *string `json:"severity"`
}

func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	projectKey := r.PathValue("key")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createTicketRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	fp, ferr := service.Fingerprint(r.Method, r.URL.Path, body)
	if ferr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: ferr.Error()})
		return
	}

	var severity *domain.Severity
	if req.Severity != nil {
		sev := domain.Severity(*req.Severity)
		severity = &sev
	}

	ticket, err := s.svc.CreateTicket(r.Context(), service.CreateTicketRequest{
		ProjectKey:  projectKey,
		Type:        domain.TicketType(req.Type),
		Title:       req.Title,
		Description: req.Description,
		Priority:    domain.Priority(req.Priority),
		Severity:    severity,
	}, requestActor(r), correlationID(r), idempotencyKey(r), fp)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ticket)
}

func (s *Server) getTicket(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	ticket, err := s.svc.GetTicket(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

type updateTicketStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) updateTicketStatus(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
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
	var req updateTicketStatusRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	ticket, err := s.svc.UpdateTicketStatus(r.Context(), service.UpdateTicketStatusRequest{
		Ref: ref, NewStatus: domain.WorkflowStatus(req.Status), ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func parseTicketRef(s string) (domain.Reference, *service.Error) {
	ref, err := domain.Parse(s)
	if err != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if ref.Kind != domain.KindTicket {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a ticket reference"}
	}
	return ref, nil
}
