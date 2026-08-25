package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

type createAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

type accountDetail struct {
	Username  string     `json:"username"`
	IsAdmin   bool       `json:"is_admin"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// createAccount is POST /api/v1/accounts, routeAdmin (Phase 7 —
// product spec §4.2/§13's account management, previously only
// reachable through `tickets setup`'s one-time first-run bootstrap).
func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createAccountRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	actorRef, err := s.svc.CreateHumanAccount(r.Context(), service.CreateHumanAccountRequest{
		Username: req.Username, Password: req.Password, IsAdmin: req.IsAdmin,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, accountDetail{Username: actorRef.Name, IsAdmin: req.IsAdmin})
}

// listAccounts is GET /api/v1/accounts, routeAdmin.
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.svc.ListHumanAccounts(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]accountDetail, len(accounts))
	for i, a := range accounts {
		createdAt := a.CreatedAt
		out[i] = accountDetail{Username: a.Username, IsAdmin: a.IsAdmin, CreatedAt: &createdAt}
	}
	writeJSON(w, http.StatusOK, struct {
		Accounts []accountDetail `json:"accounts"`
	}{Accounts: out})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password"`
}

// changePassword is POST /api/v1/accounts/{username}/password,
// routeEditor with an in-handler check: the caller must either be the
// target account itself (self-service — old_password required and
// verified) or an admin (reset — old_password ignored). Plain Editor
// is the route-table permission because a non-admin human must be
// able to reach this for their own account; the self-or-admin
// narrowing happens here, the same way project comment ownership
// checks happen in-handler rather than via a route-table permission
// level (there is no route class between Editor and Admin).
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	principal := auth.FromContext(r.Context())
	isSelf := principal.Actor.Kind == domain.ActorHuman && principal.Actor.Name == username
	if !isSelf && !principal.IsAdmin {
		writeError(w, r, &service.Error{Code: domain.ErrForbidden, Message: "you may only change your own password unless you are an admin"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req changePasswordRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	err = s.svc.ChangePassword(r.Context(), service.ChangePasswordRequest{
		Username: username, OldPassword: req.OldPassword, NewPassword: req.NewPassword, SelfService: isSelf,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
