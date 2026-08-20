package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// relationshipViews resolves ref's relationships and formats each
// edge's Other reference for the wire — shared by listRelationships
// and getTicket's ?include=relationships (tickets.go), so the two
// routes can't drift on how an edge gets formatted.
func relationshipViews(ctx context.Context, svc *service.Service, ref domain.Reference) ([]relationshipView, error) {
	views, err := svc.GetTicketRelationships(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := make([]relationshipView, len(views))
	for i, v := range views {
		refStr, err := domain.Format(v.Other)
		if err != nil {
			return nil, fmt.Errorf("httpapi: format relationship endpoint: %w", err)
		}
		out[i] = relationshipView{Type: string(v.Type), Other: refStr}
	}
	return out, nil
}

type addRelationshipRequest struct {
	Target string `json:"target"`
	Type   string `json:"type"`
}

func (s *Server) addRelationship(w http.ResponseWriter, r *http.Request) {
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
	var req addRelationshipRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	targetRef, terr := domain.Parse(req.Target)
	if terr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "target", Message: terr.Error()})
		return
	}

	if err := s.svc.AddRelationship(r.Context(), service.AddRelationshipRequest{
		SourceRef: ref, TargetRef: targetRef, Type: domain.RelationshipType(req.Type),
	}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// removeRelationship's {type}/{target} path segments (not a body) —
// unlike POST, which has meaningful room for a request body, DELETE's
// target is fully identified by its URL alone here: a relationship
// type is a fixed enum value and a ticket ref never contains a slash
// (domain.Format's "PROJECTKEY-SEQ" shape), so both route cleanly as
// path segments without the awkwardness of a DELETE request body.
func (s *Server) removeRelationship(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	targetRef, terr := domain.Parse(r.PathValue("target"))
	if terr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "target", Message: terr.Error()})
		return
	}

	if err := s.svc.RemoveRelationship(r.Context(), service.RemoveRelationshipRequest{
		SourceRef: ref, TargetRef: targetRef, Type: domain.RelationshipType(r.PathValue("type")),
	}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) listRelationships(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseTicketRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	out, err := relationshipViews(r.Context(), s.svc, ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, relationshipsPage{Relationships: out})
}

// parseAssociationRef parses a path {ref} that may name either a
// ticket or a feature — associations are the one edge kind that spans
// both (product spec §5.7), so unlike parseTicketRef/parseFeatureRef
// this deliberately does not restrict Kind. internal/service's
// resolveAssociationEndpoint already rejects any other kind with a
// clear validation_failed, so there is nothing for this layer to
// duplicate.
func parseAssociationRef(s string) (domain.Reference, *service.Error) {
	ref, err := domain.Parse(s)
	if err != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	return ref, nil
}

type addAssociationRequest struct {
	Target string `json:"target"`
}

func (s *Server) addAssociation(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req addAssociationRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	targetRef, svcErr := parseAssociationRef(req.Target)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	if err := s.svc.AddAssociation(r.Context(), service.AddAssociationRequest{
		SourceRef: ref, TargetRef: targetRef,
	}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (s *Server) removeAssociation(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	targetRef, svcErr := parseAssociationRef(r.PathValue("target"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	if err := s.svc.RemoveAssociation(r.Context(), service.RemoveAssociationRequest{
		SourceRef: ref, TargetRef: targetRef,
	}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) listAssociations(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	refs, err := s.svc.GetAssociations(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]string, len(refs))
	for i, assocRef := range refs {
		refStr, ferr := domain.Format(assocRef)
		if ferr != nil {
			writeError(w, r, ferr)
			return
		}
		out[i] = refStr
	}
	writeJSON(w, http.StatusOK, associationsPage{Associated: out})
}
