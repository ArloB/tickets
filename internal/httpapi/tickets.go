package httpapi

import (
	"io"
	"net/http"
	"strconv"

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
	writeJSON(w, http.StatusCreated, toTicketDetail(ticket))
}

// listTickets is GET /projects/{key}/tickets (product spec §5.6):
// priority-queue order by default, issue-register order via
// ?view=issue_register (§5.5). ?fields= narrows each list item to the
// named ticketCompact fields. Phase 4 added ?status=/?type=/?severity=
// /?priority=/?feature_ref=/?assignee=/?creator=/?updated_since=
// (docs/contracts/list-filters.md): each is optional and AND-composed
// with the others and with whichever ?view= selected the base
// ordering, and — like ?cursor= — must be resupplied on every page of
// a filtered listing (service.TicketListFilters' doc comment explains
// why the cursor doesn't encode them). The backlog view (product spec
// §6.1) is the default priority-queue ordering with filters layered
// on; there is no separate third ?view= value for it.
func (s *Server) listTickets(w http.ResponseWriter, r *http.Request) {
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

	filters := service.TicketListFilters{
		Status:       domain.WorkflowStatus(q.Get("status")),
		Type:         domain.TicketType(q.Get("type")),
		Severity:     domain.Severity(q.Get("severity")),
		Priority:     domain.Priority(q.Get("priority")),
		FeatureRef:   q.Get("feature_ref"),
		Assignee:     q.Get("assignee"),
		Creator:      q.Get("creator"),
		UpdatedSince: q.Get("updated_since"),
	}
	result, err := s.svc.ListTicketsFiltered(r.Context(), projectKey, service.TicketListView(q.Get("view")), limit, q.Get("cursor"), filters)
	if err != nil {
		writeError(w, r, err)
		return
	}
	compacted := make([]ticketCompact, len(result.Tickets))
	for i, t := range result.Tickets {
		compacted[i] = toTicketCompact(t)
	}

	fields := fieldNames(r)
	if len(fields) == 0 {
		writeJSON(w, http.StatusOK, ticketsPage{Tickets: compacted, NextCursor: result.NextCursor})
		return
	}
	if svcErr := validateFieldNames(allowedTicketCompactFields, fields); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	projected := make([]map[string]any, len(compacted))
	for i, c := range compacted {
		p, svcErr := projectFields(c, fields)
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		projected[i] = p
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": projected, "next_cursor": result.NextCursor})
}

// getTicket supports ?fields= (narrowing to the named ticketDetail
// fields) and ?include=comments,relationships (expanding the response
// with those sub-resources, docs/contracts/representations.md).
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
	detail := toTicketDetail(ticket)

	include := includeNames(r)
	if include["comments"] {
		comments, err := s.svc.ListComments(r.Context(), ref)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out := make([]commentDetail, len(comments))
		for i, c := range comments {
			out[i] = toCommentDetail(c)
		}
		detail.Comments = &out
	}
	if include["relationships"] {
		out, err := relationshipViews(r.Context(), s.svc, ref)
		if err != nil {
			writeError(w, r, err)
			return
		}
		detail.Relationships = &out
	}

	fields := fieldNames(r)
	if len(fields) == 0 {
		writeJSON(w, http.StatusOK, detail)
		return
	}
	if svcErr := validateFieldNames(allowedTicketDetailFields, fields); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	projected, svcErr := projectFields(detail, fields)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	writeJSON(w, http.StatusOK, projected)
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
	writeJSON(w, http.StatusOK, toTicketDetail(ticket))
}

type updateTicketFieldsRequest struct {
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	Severity    *string `json:"severity"`
}

// updateTicketFields is PUT /tickets/{ref}: a full-representation
// update of every mutable ticket field except status (product spec
// §5.6, the plan's Step 13). Deliberately a distinct route from PATCH
// /tickets/{ref} rather than replacing it: that PATCH is the
// established, narrowly-scoped status transition endpoint
// (updateTicketStatus above), and merging the two would mean every
// drag-and-drop status change now has to also resend type/title/
// description/priority just to avoid clobbering them — PUT's "send
// the full representation" contract is the right fit for this
// operation, PATCH's "send the one thing that changed" contract is the
// right fit for that one.
func (s *Server) updateTicketFields(w http.ResponseWriter, r *http.Request) {
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
	var req updateTicketFieldsRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	var severity *domain.Severity
	if req.Severity != nil {
		sev := domain.Severity(*req.Severity)
		severity = &sev
	}

	ticket, err := s.svc.UpdateTicketFields(r.Context(), service.UpdateTicketFieldsRequest{
		Ref: ref, Type: domain.TicketType(req.Type), Title: req.Title, Description: req.Description,
		Priority: domain.Priority(req.Priority), Severity: severity, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTicketDetail(ticket))
}

type assignTicketRequest struct {
	Assignee *string `json:"assignee"`
}

func (s *Server) assignTicket(w http.ResponseWriter, r *http.Request) {
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
	var req assignTicketRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	var assignee *domain.ActorRef
	if req.Assignee != nil {
		parsed, perr := domain.ParseActorRef(*req.Assignee)
		if perr != nil {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "assignee", Message: perr.Error()})
			return
		}
		assignee = &parsed
	}

	ticket, err := s.svc.AssignTicket(r.Context(), service.AssignTicketRequest{
		Ref: ref, Assignee: assignee, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTicketDetail(ticket))
}

type moveTicketRequest struct {
	Feature string `json:"feature"`
}

func (s *Server) moveTicket(w http.ResponseWriter, r *http.Request) {
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
	var req moveTicketRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	featureRef, svcErr := parseFeatureRef(req.Feature)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	ticket, err := s.svc.MoveTicketFeature(r.Context(), service.MoveTicketFeatureRequest{
		Ref: ref, NewFeatureRef: featureRef, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTicketDetail(ticket))
}

type reorderTicketRequest struct {
	AfterRef *string `json:"after_ref"`
}

func (s *Server) reorderTicket(w http.ResponseWriter, r *http.Request) {
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
	var req reorderTicketRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	var afterRef *domain.Reference
	if req.AfterRef != nil {
		parsed, svcErr := parseTicketRef(*req.AfterRef)
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		afterRef = &parsed
	}

	ticket, err := s.svc.ReorderTicket(r.Context(), service.ReorderTicketRequest{
		Ref: ref, AfterRef: afterRef, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTicketDetail(ticket))
}

func (s *Server) deleteTicket(w http.ResponseWriter, r *http.Request) {
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

	newVersion, err := s.svc.DeleteTicket(r.Context(), service.DeleteTicketRequest{
		Ref: ref, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteResponse{Version: newVersion})
}

func (s *Server) restoreTicket(w http.ResponseWriter, r *http.Request) {
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

	ticket, err := s.svc.RestoreTicket(r.Context(), service.RestoreTicketRequest{
		Ref: ref, ExpectedVersion: version,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTicketDetail(ticket))
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
