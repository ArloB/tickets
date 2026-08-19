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
	return b.Svc.CreateTicket(ctx, req, "", "")
}
