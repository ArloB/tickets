package mcpsrv

import (
	"context"

	"github.com/ArloB/tickets/internal/auth"
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
// calls (ADR 0012). It reads whatever tools.go's withCallerActor
// attached to ctx from the verified bearer token's TokenInfo — the
// same context-read pattern internal/httpapi's requestActor uses,
// applied to MCP's own auth wiring (ADR 0006). The stdio bridge never
// reaches this: it uses HTTPBackend, not InProcessBackend, so its
// calls never carry a Principal on ctx at all, and this function is
// simply never called there.
//
// A zero-value ActorRef (no Principal was ever attached — e.g. an
// unauthenticated in-memory transport call) is rejected explicitly
// here, rather than being handed to store.GetActorIDByRef, which
// would fail the lookup and surface as an opaque internal_error. An
// unauthenticated write should read back as "unauthorized", not as an
// unexplained database failure.
func mcpActor(ctx context.Context) (domain.ActorRef, error) {
	actor := auth.FromContext(ctx).Actor
	if actor == (domain.ActorRef{}) {
		return domain.ActorRef{}, &service.Error{Code: domain.ErrUnauthorized, Message: "no authenticated actor for this call"}
	}
	return actor, nil
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
	actor, err := mcpActor(ctx)
	if err != nil {
		return domain.Ticket{}, err
	}
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
	return b.Svc.CreateTicket(ctx, req, actor, service.NewCorrelationID(), "", "")
}
