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

func toDomainFeature(f apiclient.Feature) domain.Feature {
	return domain.Feature{
		Ref: f.Ref, ProjectKey: f.Project, Title: f.Title, Description: f.Description,
		Status: domain.WorkflowStatus(f.Status), Priority: domain.Priority(f.Priority),
		Version: f.Version, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
	}
}

func (b *HTTPBackend) GetFeature(ctx context.Context, ref string) (domain.Feature, error) {
	f, err := b.Client.GetFeature(ctx, ref)
	if err != nil {
		return domain.Feature{}, toServiceError(err)
	}
	return toDomainFeature(f), nil
}

func (b *HTTPBackend) CreateFeature(ctx context.Context, in CreateFeatureInput) (FeatureWriteResult, error) {
	projectKey := in.ProjectKey
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return FeatureWriteResult{}, errMissingProjectKey()
	}
	f, err := b.Client.CreateFeature(ctx, projectKey, apiclient.CreateFeatureRequest{
		Title: in.Title, Description: in.Description, Priority: in.Priority,
	})
	if err != nil {
		return FeatureWriteResult{}, toServiceError(err)
	}
	return toFeatureWriteResult(toDomainFeature(f)), nil
}

func (b *HTTPBackend) UpdateFeature(ctx context.Context, in UpdateFeatureInput) (FeatureWriteResult, error) {
	f, err := b.Client.UpdateFeature(ctx, in.Ref, apiclient.UpdateFeatureRequest{
		Title: in.Title, Description: in.Description, Priority: in.Priority,
	}, in.ExpectedVersion)
	if err != nil {
		return FeatureWriteResult{}, toServiceError(err)
	}
	return toFeatureWriteResult(toDomainFeature(f)), nil
}

func toDomainDecision(d apiclient.Decision) domain.Decision {
	return domain.Decision{
		Ref: d.Ref, ProjectKey: d.Project, Title: d.Title, Context: d.Context,
		Decision: d.Decision, Rationale: d.Rationale, Status: domain.DecisionStatus(d.Status),
		Version: d.Version, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func (b *HTTPBackend) GetDecision(ctx context.Context, ref string) (domain.Decision, error) {
	d, err := b.Client.GetDecision(ctx, ref)
	if err != nil {
		return domain.Decision{}, toServiceError(err)
	}
	return toDomainDecision(d), nil
}

func (b *HTTPBackend) CreateDecision(ctx context.Context, in CreateDecisionInput) (DecisionWriteResult, error) {
	projectKey := in.ProjectKey
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return DecisionWriteResult{}, errMissingProjectKey()
	}
	d, err := b.Client.CreateDecision(ctx, projectKey, apiclient.CreateDecisionRequest{
		Title: in.Title, Context: in.Context, Decision: in.Decision, Rationale: in.Rationale,
	}, in.IdempotencyKey)
	if err != nil {
		return DecisionWriteResult{}, toServiceError(err)
	}
	return toDecisionWriteResult(toDomainDecision(d)), nil
}

func (b *HTTPBackend) UpdateDecision(ctx context.Context, in UpdateDecisionInput) (DecisionWriteResult, error) {
	d, err := b.Client.UpdateDecision(ctx, in.Ref, apiclient.UpdateDecisionRequest{
		Title: in.Title, Context: in.Context, Decision: in.Decision, Rationale: in.Rationale, Status: in.Status,
	}, in.ExpectedVersion)
	if err != nil {
		return DecisionWriteResult{}, toServiceError(err)
	}
	return toDecisionWriteResult(toDomainDecision(d)), nil
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

func (b *HTTPBackend) ListProjects(ctx context.Context, limit int, cursor string) (ProjectsListOutput, error) {
	page, err := b.Client.ListProjects(ctx, limit, cursor)
	if err != nil {
		return ProjectsListOutput{}, toServiceError(err)
	}
	out := ProjectsListOutput{Projects: make([]ProjectCompact, len(page.Projects)), NextCursor: page.NextCursor}
	for i, p := range page.Projects {
		out.Projects[i] = fromAPIProjectCompact(p)
	}
	return out, nil
}

func (b *HTTPBackend) CreateProject(ctx context.Context, in CreateProjectInput) (domain.Project, error) {
	p, err := b.Client.CreateProject(ctx, apiclient.CreateProjectRequest{
		Key: in.Key, Title: in.Title, Description: in.Description,
	}, in.IdempotencyKey)
	if err != nil {
		return domain.Project{}, toServiceError(err)
	}
	return toDomainProject(p), nil
}

func (b *HTTPBackend) ListFeatures(ctx context.Context, projectKey string, limit int, cursor string) (FeaturesListOutput, error) {
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return FeaturesListOutput{}, errMissingProjectKey()
	}
	page, err := b.Client.ListFeatures(ctx, projectKey, limit, cursor)
	if err != nil {
		return FeaturesListOutput{}, toServiceError(err)
	}
	out := FeaturesListOutput{Features: make([]FeatureCompact, len(page.Features)), NextCursor: page.NextCursor}
	for i, f := range page.Features {
		out.Features[i] = fromAPIFeatureCompact(f)
	}
	return out, nil
}

func (b *HTTPBackend) GetTicketRelationships(ctx context.Context, ref string) (RelationshipsOutput, error) {
	page, err := b.Client.ListRelationships(ctx, ref)
	if err != nil {
		return RelationshipsOutput{}, toServiceError(err)
	}
	out := RelationshipsOutput{Relationships: make([]RelationshipView, len(page.Relationships))}
	for i, r := range page.Relationships {
		out.Relationships[i] = RelationshipView{Type: string(r.Type), Other: r.Other}
	}
	return out, nil
}

func (b *HTTPBackend) GetAssociations(ctx context.Context, ref string) (AssociationsOutput, error) {
	page, err := b.Client.ListAssociations(ctx, ref)
	if err != nil {
		return AssociationsOutput{}, toServiceError(err)
	}
	return AssociationsOutput{Associated: page.Associated}, nil
}

func (b *HTTPBackend) ListTickets(ctx context.Context, projectKey, view string, limit int, cursor string) (TicketsListOutput, error) {
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return TicketsListOutput{}, errMissingProjectKey()
	}
	page, err := b.Client.ListTickets(ctx, projectKey, view, limit, cursor)
	if err != nil {
		return TicketsListOutput{}, toServiceError(err)
	}
	out := TicketsListOutput{Tickets: make([]TicketCompact, len(page.Tickets)), NextCursor: page.NextCursor}
	for i, t := range page.Tickets {
		out.Tickets[i] = fromAPITicketCompact(t)
	}
	return out, nil
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

func (b *HTTPBackend) UpdateTicket(ctx context.Context, in UpdateTicketInput) (TicketWriteResult, error) {
	expectedVersion := in.ExpectedVersion
	t, err := b.Client.UpdateTicket(ctx, in.Ref, apiclient.UpdateTicketOptions{
		Status: in.Status, Type: in.Type, Title: in.Title, Description: in.Description,
		Priority: in.Priority, Severity: in.Severity, ExpectedVersion: &expectedVersion,
	})
	if err != nil {
		return TicketWriteResult{}, toServiceError(err)
	}
	ticket, err := toDomainTicket(t)
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

func (b *HTTPBackend) AddComment(ctx context.Context, ticketRef, body, idempotencyKey string) (CommentWriteResult, error) {
	c, err := b.Client.CreateComment(ctx, ticketRef, body, idempotencyKey)
	if err != nil {
		return CommentWriteResult{}, toServiceError(err)
	}
	return CommentWriteResult{ID: c.ID, Version: c.Version, CreatedAt: c.CreatedAt}, nil
}

func (b *HTTPBackend) AddRelationship(ctx context.Context, sourceRef, relType, targetRef string) error {
	if err := b.Client.AddRelationship(ctx, sourceRef, relType, targetRef); err != nil {
		return toServiceError(err)
	}
	return nil
}

func (b *HTTPBackend) AddAssociation(ctx context.Context, sourceRef, targetRef string) error {
	if err := b.Client.AddAssociation(ctx, sourceRef, targetRef); err != nil {
		return toServiceError(err)
	}
	return nil
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
