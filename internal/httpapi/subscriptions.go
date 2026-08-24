package httpapi

import (
	"net/http"

	"github.com/ArloB/tickets/internal/service"
)

// subscriptionView is GET .../subscription's response — a single
// boolean, mirroring how listLinks/listAssociations expose read-only
// state as a small wrapper object rather than a bare JSON boolean
// (docs/contracts/representations.md's convention of always wrapping
// a top-level response in an object).
type subscriptionView struct {
	Subscribed bool `json:"subscribed"`
}

// subscribe/unsubscribe/getSubscription are POST/DELETE/GET .../subscribe
// on a ticket, feature, decision, plan, or document — one handler set
// reused across every route registration, the same pattern addLink/
// addAssociation use (parseAssociationRef doesn't restrict which
// principal kind ref names).
func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	if err := s.svc.Subscribe(r.Context(), service.SubscribeRequest{Ref: ref}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionView{Subscribed: true})
}

func (s *Server) unsubscribe(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	if err := s.svc.Unsubscribe(r.Context(), service.SubscribeRequest{Ref: ref}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionView{Subscribed: false})
}

func (s *Server) getSubscription(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	subscribed, err := s.svc.IsSubscribed(r.Context(), ref, requestActor(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionView{Subscribed: subscribed})
}
