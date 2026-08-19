package mcpsrv

import (
	"context"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// InProcessBackend wraps *service.Service directly for the HTTP-
// mounted MCP endpoint, which runs in the same process as the rest of
// the server.
type InProcessBackend struct {
	Svc *service.Service
}

// mcpActor is every mutating tool call's actor for internal/service
// calls (ADR 0012). Phase 1 has no authentication (ADR 0004 lands in
// Phase 2 — spike 2.2's auth.RequireBearerToken assertion is proven
// but not wired here), so every tool call is attributed to the single
// seeded 'local' actor (migration 0002_core_domain.sql), the same
// placeholder internal/httpapi's requestActor uses. Distinguishing
// individual agent identities (product spec §16's "separately
// attributed agent identities") is what Phase 2's token-to-actor
// resolution adds.
func mcpActor() domain.ActorRef {
	return domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
}

func (b *InProcessBackend) GetProject(ctx context.Context, key string) (domain.Project, error) {
	return b.Svc.GetProject(ctx, key)
}

func (b *InProcessBackend) GetTicket(ctx context.Context, ref string) (domain.Ticket, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if parsed.Kind != domain.KindTicket {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a ticket reference"}
	}
	return b.Svc.GetTicket(ctx, parsed)
}

func (b *InProcessBackend) CreateTicket(ctx context.Context, in CreateTicketInput) (domain.Ticket, error) {
	var severity *domain.Severity
	if in.Severity != "" {
		s := domain.Severity(in.Severity)
		severity = &s
	}
	req := service.CreateTicketRequest{
		ProjectKey:  in.ProjectKey,
		Type:        domain.TicketType(in.Type),
		Title:       in.Title,
		Description: in.Description,
		Priority:    domain.Priority(in.Priority),
		Severity:    severity,
	}
	return b.Svc.CreateTicket(ctx, req, mcpActor(), service.NewCorrelationID(), "", "")
}
