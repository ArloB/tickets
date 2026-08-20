package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// agentDetail is the admin agent-management response shape (product
// spec §4.1: name, description, owning human, creation time).
type agentDetail struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Owner       string    `json:"owner,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func toAgentDetail(d service.AgentDetail) agentDetail {
	out := agentDetail{Name: d.Ref.Name, Description: d.Description, CreatedAt: d.CreatedAt}
	if d.Owner != nil {
		out.Owner = d.Owner.String()
	}
	return out
}

type createAgentRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type agentsPage struct {
	Agents []agentDetail `json:"agents"`
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createAgentRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	detail, err := s.svc.CreateAgent(r.Context(),
		service.CreateAgentRequest{Name: req.Name, Description: req.Description},
		requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAgentDetail(detail))
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.svc.ListAgents(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]agentDetail, len(agents))
	for i, a := range agents {
		out[i] = toAgentDetail(a)
	}
	writeJSON(w, http.StatusOK, agentsPage{Agents: out})
}

type createAgentTokenRequest struct {
	Description string     `json:"description"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

// agentTokenCreated is the only response shape that ever carries a raw
// token value (ADR 0004: shown once, at creation). It is structurally
// distinct from agentTokenSummary below, which every later read of the
// same token returns instead — a token value can never leak onto a
// second response by construction, not just by handler discipline.
type agentTokenCreated struct {
	ID          int64      `json:"id"`
	Token       string     `json:"token"`
	Description string     `json:"description"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type agentTokenSummary struct {
	ID          int64      `json:"id"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

func toAgentTokenSummary(t service.AgentTokenSummary) agentTokenSummary {
	return agentTokenSummary{
		ID: t.ID, Description: t.Description, CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt,
	}
}

type agentTokensPage struct {
	Tokens []agentTokenSummary `json:"tokens"`
}

func (s *Server) createAgentToken(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createAgentTokenRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	raw, tokenID, err := s.svc.CreateAgentToken(r.Context(),
		domain.ActorRef{Kind: domain.ActorAgent, Name: name}, req.Description, req.ExpiresAt,
		requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, agentTokenCreated{
		ID: tokenID, Token: raw, Description: req.Description, ExpiresAt: req.ExpiresAt,
	})
}

func (s *Server) listAgentTokens(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tokens, err := s.svc.ListAgentTokens(r.Context(), domain.ActorRef{Kind: domain.ActorAgent, Name: name})
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]agentTokenSummary, len(tokens))
	for i, t := range tokens {
		out[i] = toAgentTokenSummary(t)
	}
	writeJSON(w, http.StatusOK, agentTokensPage{Tokens: out})
}

func (s *Server) revokeAgentToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "id", Message: "token id must be an integer"})
		return
	}
	if err := s.svc.RevokeAgentToken(r.Context(), id, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
