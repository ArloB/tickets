package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateAgentAndListAgents(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	detail, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex", Description: "CI agent"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if detail.Ref.Kind != domain.ActorAgent || detail.Ref.Name != "codex" || detail.Description != "CI agent" {
		t.Errorf("CreateAgent returned %+v, want ref=agent:codex description=%q", detail, "CI agent")
	}
	if detail.Owner == nil || *detail.Owner != testActor {
		t.Errorf("CreateAgent owner = %v, want %v (the calling actor)", detail.Owner, testActor)
	}

	agents, err := s.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 || agents[0].Ref != detail.Ref {
		t.Errorf("ListAgents = %+v, want [%v]", agents, detail.Ref)
	}
}

func TestCreateAgentRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID); err != nil {
		t.Fatalf("first CreateAgent: %v", err)
	}
	_, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrAlreadyExists {
		t.Fatalf("duplicate agent name error = %v, want already_exists", err)
	}
}

func TestCreateAgentRequiresName(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "   "}, testActor, testCorrelationID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("CreateAgent with blank name error = %v, want validation_failed", err)
	}
}

func TestGetAgentDetailNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.GetAgentDetail(ctx, "does-not-exist")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrNotFound {
		t.Fatalf("GetAgentDetail(does-not-exist) error = %v, want not_found", err)
	}
}

func TestCreateAgentTokenAndVerify(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	agent, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	raw, tokenID, err := s.CreateAgentToken(ctx, agent.Ref, "ci token", nil, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}
	if raw == "" || tokenID == 0 {
		t.Fatalf("CreateAgentToken returned raw=%q tokenID=%d, want both non-zero", raw, tokenID)
	}

	verified, err := s.VerifyBearerToken(ctx, raw)
	if err != nil {
		t.Fatalf("VerifyBearerToken: %v", err)
	}
	if verified != agent.Ref {
		t.Errorf("VerifyBearerToken = %v, want %v", verified, agent.Ref)
	}

	tokens, err := s.ListAgentTokens(ctx, agent.Ref)
	if err != nil {
		t.Fatalf("ListAgentTokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != tokenID {
		t.Fatalf("ListAgentTokens = %+v, want one token with id %d", tokens, tokenID)
	}
}

func TestVerifyBearerTokenRejectsUnknownToken(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.VerifyBearerToken(ctx, "not-a-real-token")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrUnauthorized {
		t.Fatalf("VerifyBearerToken(unknown) error = %v, want unauthorized", err)
	}
}

func TestRevokeAgentTokenRejectsFurtherUse(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	agent, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	raw, tokenID, err := s.CreateAgentToken(ctx, agent.Ref, "", nil, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	if err := s.RevokeAgentToken(ctx, tokenID, testActor, testCorrelationID); err != nil {
		t.Fatalf("RevokeAgentToken: %v", err)
	}

	_, err = s.VerifyBearerToken(ctx, raw)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrUnauthorized {
		t.Fatalf("VerifyBearerToken after revoke error = %v, want unauthorized", err)
	}
}

func TestVerifyBearerTokenRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	agent, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	raw, _, err := s.CreateAgentToken(ctx, agent.Ref, "", &past, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	_, err = s.VerifyBearerToken(ctx, raw)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrUnauthorized {
		t.Fatalf("VerifyBearerToken(expired) error = %v, want unauthorized", err)
	}
}

func TestVerifyBearerTokenAcceptsUnexpiredToken(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	agent, err := s.CreateAgent(ctx, CreateAgentRequest{Name: "codex"}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	future := time.Now().Add(time.Hour)
	raw, _, err := s.CreateAgentToken(ctx, agent.Ref, "", &future, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}

	verified, err := s.VerifyBearerToken(ctx, raw)
	if err != nil {
		t.Fatalf("VerifyBearerToken(not yet expired): %v", err)
	}
	if verified != agent.Ref {
		t.Errorf("VerifyBearerToken = %v, want %v", verified, agent.Ref)
	}

	tokens, err := s.ListAgentTokens(ctx, agent.Ref)
	if err != nil {
		t.Fatalf("ListAgentTokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ExpiresAt == nil {
		t.Fatalf("ListAgentTokens = %+v, want one token with ExpiresAt set", tokens)
	}
	if diff := tokens[0].ExpiresAt.Sub(future); diff > time.Second || diff < -time.Second {
		t.Errorf("ListAgentTokens ExpiresAt = %v, want ~%v", *tokens[0].ExpiresAt, future)
	}
}

func TestCreateAgentTokenRejectsNonAgentTarget(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, _, err := s.CreateAgentToken(ctx, testActor, "", nil, testActor, testCorrelationID) // testActor is human:local
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrValidationFailed {
		t.Fatalf("CreateAgentToken targeting a human actor: error = %v, want validation_failed", err)
	}
}
