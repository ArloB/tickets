package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// mcpFingerprint computes an idempotency fingerprint for an in-process
// MCP tool call the same way internal/httpapi computes one for an HTTP
// request (service.Fingerprint), but from the tool's already-decoded
// input instead of a raw request body — there is no HTTP request here
// to hash. tool stands in for method+path, scoping the fingerprint so
// the same content reused under a different tool name isn't mistaken
// for a replay.
func mcpFingerprint(tool string, in any) (string, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("mcpsrv: marshal fingerprint body: %w", err)
	}
	return service.Fingerprint(tool, "", body)
}

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

func (b *InProcessBackend) ListProjects(ctx context.Context, limit int, cursor string) (ProjectsListOutput, error) {
	result, err := b.Svc.ListProjects(ctx, limit, cursor)
	if err != nil {
		return ProjectsListOutput{}, err
	}
	out := ProjectsListOutput{Projects: make([]ProjectCompact, len(result.Projects)), NextCursor: result.NextCursor}
	for i, p := range result.Projects {
		out.Projects[i] = toProjectCompact(p)
	}
	return out, nil
}

func (b *InProcessBackend) CreateProject(ctx context.Context, in CreateProjectInput) (domain.Project, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return domain.Project{}, err
	}
	req := service.CreateProjectRequest{Key: in.Key, Title: in.Title, Description: in.Description}
	var fingerprint string
	if in.IdempotencyKey != "" {
		fingerprint, err = mcpFingerprint("project_create", req)
		if err != nil {
			return domain.Project{}, err
		}
	}
	return b.Svc.CreateProject(ctx, req, actor, service.NewCorrelationID(), in.IdempotencyKey, fingerprint)
}

func (b *InProcessBackend) ListFeatures(ctx context.Context, projectKey string, limit int, cursor string) (FeaturesListOutput, error) {
	result, err := b.Svc.ListFeatures(ctx, projectKey, limit, cursor)
	if err != nil {
		return FeaturesListOutput{}, err
	}
	out := FeaturesListOutput{Features: make([]FeatureCompact, len(result.Features)), NextCursor: result.NextCursor}
	for i, f := range result.Features {
		out.Features[i] = toFeatureCompact(f)
	}
	return out, nil
}

func (b *InProcessBackend) GetTicketRelationships(ctx context.Context, ref string) (RelationshipsOutput, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return RelationshipsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if parsed.Kind != domain.KindTicket {
		return RelationshipsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a ticket reference"}
	}
	views, err := b.Svc.GetTicketRelationships(ctx, parsed)
	if err != nil {
		return RelationshipsOutput{}, err
	}
	out := RelationshipsOutput{Relationships: make([]RelationshipView, len(views))}
	for i, v := range views {
		other, ferr := domain.Format(v.Other)
		if ferr != nil {
			return RelationshipsOutput{}, ferr
		}
		out.Relationships[i] = RelationshipView{Type: string(v.Type), Other: other}
	}
	return out, nil
}

func (b *InProcessBackend) GetAssociations(ctx context.Context, ref string) (AssociationsOutput, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return AssociationsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	refs, err := b.Svc.GetAssociations(ctx, parsed)
	if err != nil {
		return AssociationsOutput{}, err
	}
	out := AssociationsOutput{Associated: make([]string, len(refs))}
	for i, r := range refs {
		formatted, ferr := domain.Format(r)
		if ferr != nil {
			return AssociationsOutput{}, ferr
		}
		out.Associated[i] = formatted
	}
	return out, nil
}

func (b *InProcessBackend) ListTickets(ctx context.Context, projectKey, view string, limit int, cursor string) (TicketsListOutput, error) {
	result, err := b.Svc.ListTickets(ctx, projectKey, service.TicketListView(view), limit, cursor)
	if err != nil {
		return TicketsListOutput{}, err
	}
	out := TicketsListOutput{Tickets: make([]TicketCompact, len(result.Tickets)), NextCursor: result.NextCursor}
	for i, t := range result.Tickets {
		out.Tickets[i] = toTicketCompact(t)
	}
	return out, nil
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

func (b *InProcessBackend) UpdateTicket(ctx context.Context, in UpdateTicketInput) (TicketWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return TicketWriteResult{}, err
	}
	ref, perr := domain.Parse(in.Ref)
	if perr != nil {
		return TicketWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if ref.Kind != domain.KindTicket {
		return TicketWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a ticket reference"}
	}
	ticket, err := updateTicketInProcess(ctx, b.Svc, ref, in, actor, service.NewCorrelationID())
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

func (b *InProcessBackend) AddComment(ctx context.Context, ticketRef, body, idempotencyKey string) (CommentWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return CommentWriteResult{}, err
	}
	ref, perr := domain.Parse(ticketRef)
	if perr != nil {
		return CommentWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if ref.Kind != domain.KindTicket {
		return CommentWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a ticket reference"}
	}
	var fingerprint string
	if idempotencyKey != "" {
		fingerprint, err = mcpFingerprint("ticket_comment", struct{ Ref, Body string }{ticketRef, body})
		if err != nil {
			return CommentWriteResult{}, err
		}
	}
	comment, err := b.Svc.AddComment(ctx, service.AddCommentRequest{Ref: ref, Body: body}, actor, service.NewCorrelationID(), idempotencyKey, fingerprint)
	if err != nil {
		return CommentWriteResult{}, err
	}
	return CommentWriteResult{ID: comment.ID, Version: comment.Version, CreatedAt: comment.CreatedAt}, nil
}

func (b *InProcessBackend) AddRelationship(ctx context.Context, sourceRef, relType, targetRef string) error {
	actor, err := mcpActor(ctx)
	if err != nil {
		return err
	}
	src, perr := domain.Parse(sourceRef)
	if perr != nil {
		return &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	tgt, perr := domain.Parse(targetRef)
	if perr != nil {
		return &service.Error{Code: domain.ErrValidationFailed, Field: "target", Message: perr.Error()}
	}
	return b.Svc.AddRelationship(ctx, service.AddRelationshipRequest{
		SourceRef: src, TargetRef: tgt, Type: domain.RelationshipType(relType),
	}, actor, service.NewCorrelationID())
}

func (b *InProcessBackend) AddAssociation(ctx context.Context, sourceRef, targetRef string) error {
	actor, err := mcpActor(ctx)
	if err != nil {
		return err
	}
	src, perr := domain.Parse(sourceRef)
	if perr != nil {
		return &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	tgt, perr := domain.Parse(targetRef)
	if perr != nil {
		return &service.Error{Code: domain.ErrValidationFailed, Field: "target", Message: perr.Error()}
	}
	return b.Svc.AddAssociation(ctx, service.AddAssociationRequest{SourceRef: src, TargetRef: tgt}, actor, service.NewCorrelationID())
}

func (b *InProcessBackend) GetFeature(ctx context.Context, ref string) (domain.Feature, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return domain.Feature{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if parsed.Kind != domain.KindFeature {
		return domain.Feature{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a feature reference"}
	}
	return b.Svc.GetFeature(ctx, parsed)
}

func (b *InProcessBackend) CreateFeature(ctx context.Context, in CreateFeatureInput) (FeatureWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return FeatureWriteResult{}, err
	}
	f, err := b.Svc.CreateFeature(ctx, service.CreateFeatureRequest{
		ProjectKey: in.ProjectKey, Title: in.Title, Description: in.Description, Priority: domain.Priority(in.Priority),
	}, actor, service.NewCorrelationID())
	if err != nil {
		return FeatureWriteResult{}, err
	}
	return toFeatureWriteResult(f), nil
}

func (b *InProcessBackend) UpdateFeature(ctx context.Context, in UpdateFeatureInput) (FeatureWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return FeatureWriteResult{}, err
	}
	ref, perr := domain.Parse(in.Ref)
	if perr != nil {
		return FeatureWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if ref.Kind != domain.KindFeature {
		return FeatureWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a feature reference"}
	}
	f, err := b.Svc.UpdateFeature(ctx, service.UpdateFeatureRequest{
		Ref: ref, Title: in.Title, Description: in.Description, Priority: domain.Priority(in.Priority), ExpectedVersion: in.ExpectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return FeatureWriteResult{}, err
	}
	return toFeatureWriteResult(f), nil
}

func (b *InProcessBackend) GetDecision(ctx context.Context, ref string) (domain.Decision, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return domain.Decision{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if parsed.Kind != domain.KindDecision {
		return domain.Decision{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a decision reference"}
	}
	return b.Svc.GetDecision(ctx, parsed)
}

func (b *InProcessBackend) CreateDecision(ctx context.Context, in CreateDecisionInput) (DecisionWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return DecisionWriteResult{}, err
	}
	req := service.CreateDecisionRequest{
		ProjectKey: in.ProjectKey, Title: in.Title, Context: in.Context, Decision: in.Decision, Rationale: in.Rationale,
		Consequences: in.Consequences,
	}
	var fingerprint string
	if in.IdempotencyKey != "" {
		fingerprint, err = mcpFingerprint("record_create", req)
		if err != nil {
			return DecisionWriteResult{}, err
		}
	}
	d, err := b.Svc.CreateDecision(ctx, req, actor, service.NewCorrelationID(), in.IdempotencyKey, fingerprint)
	if err != nil {
		return DecisionWriteResult{}, err
	}
	return toDecisionWriteResult(d), nil
}

func (b *InProcessBackend) UpdateDecision(ctx context.Context, in UpdateDecisionInput) (DecisionWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return DecisionWriteResult{}, err
	}
	ref, perr := domain.Parse(in.Ref)
	if perr != nil {
		return DecisionWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if ref.Kind != domain.KindDecision {
		return DecisionWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a decision reference"}
	}
	d, err := b.Svc.UpdateDecision(ctx, service.UpdateDecisionRequest{
		Ref: ref, Title: in.Title, Context: in.Context, Decision: in.Decision, Rationale: in.Rationale,
		Consequences: in.Consequences, Status: domain.DecisionStatus(in.Status), SupersededBy: in.SupersededBy,
		ExpectedVersion: in.ExpectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return DecisionWriteResult{}, err
	}
	return toDecisionWriteResult(d), nil
}

func (b *InProcessBackend) GetContentItem(ctx context.Context, ref string) (domain.ContentItem, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return domain.ContentItem{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if parsed.Kind != domain.KindPlan && parsed.Kind != domain.KindDocument {
		return domain.ContentItem{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	return b.Svc.GetContentItem(ctx, parsed)
}

func (b *InProcessBackend) CreateContentItem(ctx context.Context, in CreateContentItemInput) (ContentItemWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return ContentItemWriteResult{}, err
	}
	req := service.CreateContentItemRequest{
		ProjectKey: in.ProjectKey, Kind: domain.EntityKind(in.Kind), Title: in.Title,
		Representation: domain.ContentRepresentation(in.Representation), Body: in.Body,
		PathValue: in.Path, URLValue: in.URL,
	}
	var fingerprint string
	if in.IdempotencyKey != "" {
		fingerprint, err = mcpFingerprint("record_create", req)
		if err != nil {
			return ContentItemWriteResult{}, err
		}
	}
	c, err := b.Svc.CreateContentItem(ctx, req, actor, service.NewCorrelationID(), in.IdempotencyKey, fingerprint)
	if err != nil {
		return ContentItemWriteResult{}, err
	}
	return toContentItemWriteResult(c), nil
}

func (b *InProcessBackend) UpdateContentItem(ctx context.Context, in UpdateContentItemInput) (ContentItemWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return ContentItemWriteResult{}, err
	}
	ref, perr := domain.Parse(in.Ref)
	if perr != nil {
		return ContentItemWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if ref.Kind != domain.KindPlan && ref.Kind != domain.KindDocument {
		return ContentItemWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	c, err := b.Svc.UpdateContentItem(ctx, service.UpdateContentItemRequest{
		Ref: ref, Title: in.Title, Body: in.Body, PathValue: in.Path, URLValue: in.URL, ExpectedVersion: in.ExpectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return ContentItemWriteResult{}, err
	}
	return toContentItemWriteResult(c), nil
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
