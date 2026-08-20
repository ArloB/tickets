package mcpsrv

import (
	"context"
	"errors"
	"fmt"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// HTTPBackend implements Backend by calling the configured Tickets
// HTTP API through apiclient.Client — this is what `tickets mcp` (the
// stdio bridge) uses, so it never opens SQLite directly (product spec
// §8.1, ADR 0006). HTTPBackend's own job is translation at the two
// edges apiclient deliberately knows nothing about: apiclient's wire
// DTOs (Project, Ticket) into this package's Backend interface
// (domain.Project, domain.Ticket), and apiclient.Error into
// *service.Error, since Backend's callers (tools.go's toolError)
// already type-switch on *service.Error the same way InProcessBackend
// produces it — apiclient itself doesn't import internal/service (a
// client package has no business depending on the server's internal
// package), so that conversion has to happen here, the one place that
// already imports both.
type HTTPBackend struct {
	Client *apiclient.Client
	// DefaultProject fills in an omitted project key on an outgoing
	// tool call — the plan's Step 15 "--project/TICKETS_PROJECT"
	// convenience default (cmd/tickets/mcp.go), resolving §7.4's
	// multi-project scoping question as a client-side convenience, not
	// a server-side authorization concept: the server never sees this
	// field or knows it exists.
	DefaultProject string
}

func toServiceError(err error) error {
	var cerr *apiclient.Error
	if !errors.As(err, &cerr) {
		// A transport failure (connection refused, unparseable body) —
		// not something the server sent. Reported through the same
		// *service.Error path as everything else so toolError
		// (tools.go) surfaces this message to the agent instead of
		// collapsing it to the generic "internal_error: an unexpected
		// error occurred": "can't reach the server" and "the server
		// rejected the request" read very differently to whoever is
		// debugging a `tickets mcp` invocation.
		return &service.Error{Code: domain.ErrInternal, Message: fmt.Sprintf("could not reach the Tickets API: %v", err)}
	}
	return &service.Error{
		Code:           cerr.Code,
		Message:        cerr.Message,
		Field:          cerr.Field,
		CurrentVersion: cerr.CurrentVersion,
	}
}

// errMissingProjectKey is GetProject/CreateTicket's answer when a tool
// call omits a project key and DefaultProject has nothing to fill in
// with — a clear validation error, not an empty key silently reaching
// apiclient.Client and producing "GET /projects/" (a request that
// either 404s confusingly or, worse, hits the wrong route).
func errMissingProjectKey() error {
	return &service.Error{
		Code:    domain.ErrValidationFailed,
		Field:   "project_key",
		Message: "no project key given and no --project/TICKETS_PROJECT default configured",
	}
}

func toDomainProject(p apiclient.Project) domain.Project {
	return domain.Project{
		Key:         p.Key,
		Title:       p.Title,
		Description: p.Description,
		Status:      domain.ProjectStatus(p.Status),
		Version:     p.Version,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// toDomainTicket parses Assignee/Creator's wire "kind:name" strings
// back into domain.ActorRef — the one step that can fail, since
// apiclient.Ticket carries them as bare strings precisely so apiclient
// itself never needs to import internal/domain's ActorRef parsing.
func toDomainTicket(t apiclient.Ticket) (domain.Ticket, error) {
	var severity *domain.Severity
	if t.Severity != nil {
		v := domain.Severity(*t.Severity)
		severity = &v
	}
	var assignee *domain.ActorRef
	if t.Assignee != nil {
		v, err := domain.ParseActorRef(*t.Assignee)
		if err != nil {
			return domain.Ticket{}, fmt.Errorf("mcpsrv: parse ticket assignee: %w", err)
		}
		assignee = &v
	}
	var creator *domain.ActorRef
	if t.Creator != nil {
		v, err := domain.ParseActorRef(*t.Creator)
		if err != nil {
			return domain.Ticket{}, fmt.Errorf("mcpsrv: parse ticket creator: %w", err)
		}
		creator = &v
	}
	return domain.Ticket{
		Ref:         t.Ref,
		ProjectKey:  t.Project,
		FeatureRef:  t.Feature,
		Type:        domain.TicketType(t.Type),
		Title:       t.Title,
		Description: t.Description,
		Status:      domain.WorkflowStatus(t.Status),
		Priority:    domain.Priority(t.Priority),
		Severity:    severity,
		Assignee:    assignee,
		Creator:     creator,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}, nil
}

func (b *HTTPBackend) GetProject(ctx context.Context, key string) (domain.Project, error) {
	if key == "" {
		key = b.DefaultProject
	}
	if key == "" {
		return domain.Project{}, errMissingProjectKey()
	}
	p, err := b.Client.GetProject(ctx, key)
	if err != nil {
		return domain.Project{}, toServiceError(err)
	}
	return toDomainProject(p), nil
}

func (b *HTTPBackend) GetTicket(ctx context.Context, ref string) (domain.Ticket, error) {
	t, err := b.Client.GetTicket(ctx, ref)
	if err != nil {
		return domain.Ticket{}, toServiceError(err)
	}
	ticket, err := toDomainTicket(t)
	if err != nil {
		return domain.Ticket{}, err
	}
	return ticket, nil
}

func (b *HTTPBackend) CreateTicket(ctx context.Context, in CreateTicketInput) (domain.Ticket, error) {
	projectKey := in.ProjectKey
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return domain.Ticket{}, errMissingProjectKey()
	}
	t, err := b.Client.CreateTicket(ctx, projectKey, apiclient.CreateTicketRequest{
		Type: in.Type, Title: in.Title, Description: in.Description, Priority: in.Priority, Severity: in.Severity,
	})
	if err != nil {
		return domain.Ticket{}, toServiceError(err)
	}
	ticket, err := toDomainTicket(t)
	if err != nil {
		return domain.Ticket{}, err
	}
	return ticket, nil
}
