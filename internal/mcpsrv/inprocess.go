package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

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

func (b *InProcessBackend) ListProjects(ctx context.Context, limit int, cursor string, includeArchivedValues ...bool) (ProjectsListOutput, error) {
	includeArchived := len(includeArchivedValues) > 0 && includeArchivedValues[0]
	result, err := b.Svc.ListProjects(ctx, limit, cursor, includeArchived)
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

// UpdateProject mirrors updateTicketInProcess's merge structure: a
// status move first (if requested), then a fields update (if
// requested), each against whichever version the prior step left
// current — see UpdateProjectInput's doc comment for why the two are
// split rather than merged into one service call.
func (b *InProcessBackend) UpdateProject(ctx context.Context, in UpdateProjectInput) (domain.Project, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return domain.Project{}, err
	}
	corrID := service.NewCorrelationID()
	ifMatch := in.ExpectedVersion
	var result domain.Project
	resultKnown := false

	if in.Status != nil {
		p, err := b.Svc.SetProjectStatus(ctx, service.SetProjectStatusRequest{
			Key: in.Key, NewStatus: domain.ProjectStatus(*in.Status), ExpectedVersion: ifMatch,
		}, actor, corrID)
		if err != nil {
			return domain.Project{}, err
		}
		result, resultKnown = p, true
		ifMatch = p.Version
	}

	if in.Title != nil || in.Description != nil {
		base := result
		if !resultKnown {
			p, err := b.Svc.GetProject(ctx, in.Key)
			if err != nil {
				return domain.Project{}, err
			}
			base = p
		}
		title, desc := base.Title, base.Description
		if in.Title != nil {
			title = *in.Title
		}
		if in.Description != nil {
			desc = *in.Description
		}
		p, err := b.Svc.UpdateProject(ctx, service.UpdateProjectRequest{
			Key: in.Key, Title: title, Description: desc, ExpectedVersion: ifMatch,
		}, actor, corrID)
		if err != nil {
			return domain.Project{}, err
		}
		result, resultKnown = p, true
	}

	if !resultKnown {
		return b.Svc.GetProject(ctx, in.Key)
	}
	return result, nil
}

func (b *InProcessBackend) ListFeatures(ctx context.Context, projectKey string, filters FeatureListFilters, limit int, cursor string) (FeaturesListOutput, error) {
	result, err := b.Svc.ListFeaturesFiltered(ctx, projectKey, limit, cursor, service.FeatureListFilters{
		Status: domain.WorkflowStatus(filters.Status), Priority: domain.Priority(filters.Priority),
		Creator: filters.Creator, UpdatedSince: filters.UpdatedSince,
	})
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

func (b *InProcessBackend) ListTickets(ctx context.Context, projectKey, view string, filters TicketListFilters, limit int, cursor string) (TicketsListOutput, error) {
	result, err := b.Svc.ListTicketsFiltered(ctx, projectKey, service.TicketListView(view), limit, cursor, service.TicketListFilters{
		Status: domain.WorkflowStatus(filters.Status), Type: domain.TicketType(filters.Type),
		Severity: domain.Severity(filters.Severity), Priority: domain.Priority(filters.Priority),
		FeatureRef: filters.FeatureRef, Assignee: filters.Assignee, Creator: filters.Creator,
		UpdatedSince: filters.UpdatedSince,
	})
	if err != nil {
		return TicketsListOutput{}, err
	}
	out := TicketsListOutput{Tickets: make([]TicketCompact, len(result.Tickets)), NextCursor: result.NextCursor}
	for i, t := range result.Tickets {
		out.Tickets[i] = toTicketCompact(t)
	}
	return out, nil
}

func (b *InProcessBackend) GetTicket(ctx context.Context, ref string, includeDeleted ...bool) (domain.Ticket, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if parsed.Kind != domain.KindTicket {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a ticket reference"}
	}
	if len(includeDeleted) > 0 && includeDeleted[0] {
		return b.Svc.GetTicketIncludingDeleted(ctx, parsed)
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

// MoveTicketFeature moves ref to a different feature within the same
// project (ADR 0001's cross-project guard lives in service). Returns
// the full domain.Ticket, not a TicketWriteResult, since the changed
// field (feature) isn't one of TicketWriteResult's — mirroring
// ticket_create's same reasoning for returning full detail.
func (b *InProcessBackend) MoveTicketFeature(ctx context.Context, ref, featureRef string, expectedVersion int64) (domain.Ticket, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return domain.Ticket{}, err
	}
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if parsedRef.Kind != domain.KindTicket {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a ticket reference"}
	}
	parsedFeature, perr := domain.Parse(featureRef)
	if perr != nil {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "feature", Message: perr.Error()}
	}
	if parsedFeature.Kind != domain.KindFeature {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "feature", Message: "reference must be a feature reference"}
	}
	return b.Svc.MoveTicketFeature(ctx, service.MoveTicketFeatureRequest{
		Ref: parsedRef, NewFeatureRef: parsedFeature, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
}

func (b *InProcessBackend) AssignTicket(ctx context.Context, ref string, assignee *string, expectedVersion int64) (TicketWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return TicketWriteResult{}, err
	}
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return TicketWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	var actorRef *domain.ActorRef
	if assignee != nil {
		parsed, aerr := domain.ParseActorRef(*assignee)
		if aerr != nil {
			return TicketWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "assignee", Message: aerr.Error()}
		}
		actorRef = &parsed
	}
	ticket, err := b.Svc.AssignTicket(ctx, service.AssignTicketRequest{
		Ref: parsedRef, Assignee: actorRef, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

func (b *InProcessBackend) ReorderTicket(ctx context.Context, ref string, afterRef *string, expectedVersion int64) (TicketWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return TicketWriteResult{}, err
	}
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return TicketWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	var parsedAfter *domain.Reference
	if afterRef != nil {
		p, perr := domain.Parse(*afterRef)
		if perr != nil {
			return TicketWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "after_ref", Message: perr.Error()}
		}
		parsedAfter = &p
	}
	ticket, err := b.Svc.ReorderTicket(ctx, service.ReorderTicketRequest{
		Ref: parsedRef, AfterRef: parsedAfter, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

func (b *InProcessBackend) DeleteTicket(ctx context.Context, ref string, expectedVersion int64) (DeleteWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return DeleteWriteResult{}, err
	}
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return DeleteWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	newVersion, err := b.Svc.DeleteTicket(ctx, service.DeleteTicketRequest{
		Ref: parsedRef, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return DeleteWriteResult{}, err
	}
	return DeleteWriteResult{Ref: ref, Version: newVersion}, nil
}

func (b *InProcessBackend) RestoreTicket(ctx context.Context, ref string, expectedVersion int64) (TicketWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return TicketWriteResult{}, err
	}
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return TicketWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	ticket, err := b.Svc.RestoreTicket(ctx, service.RestoreTicketRequest{
		Ref: parsedRef, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

// parseCommentRef parses ref for AddComment (Phase 6 Step 2): any of
// the five referenceable commentable kinds via domain.Parse, or a bare
// project key via domain.ValidProjectKey — a project has no
// seq-numbered reference token domain.Parse recognizes (see its doc),
// mirroring apiclient.commentsPathPrefix's same two-step check for the
// HTTP-bridge backend.
func parseCommentRef(ref string) (domain.Reference, *service.Error) {
	if domain.ValidProjectKey(ref) {
		return domain.Reference{ProjectKey: ref, Kind: domain.KindProject}, nil
	}
	parsed, err := domain.Parse(ref)
	if err != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	switch parsed.Kind {
	case domain.KindTicket, domain.KindFeature, domain.KindDecision, domain.KindPlan, domain.KindDocument:
		return parsed, nil
	default:
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "comments are not supported for a " + string(parsed.Kind) + " reference"}
	}
}

func (b *InProcessBackend) AddComment(ctx context.Context, commentRef, body, idempotencyKey string) (CommentWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return CommentWriteResult{}, err
	}
	ref, svcErr := parseCommentRef(commentRef)
	if svcErr != nil {
		return CommentWriteResult{}, svcErr
	}
	var fingerprint string
	if idempotencyKey != "" {
		fingerprint, err = mcpFingerprint("comment_create", struct{ Ref, Body string }{commentRef, body})
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

func (b *InProcessBackend) GetComment(ctx context.Context, id int64) (domain.Comment, error) {
	return b.Svc.GetComment(ctx, id)
}

func (b *InProcessBackend) ListComments(ctx context.Context, commentRef string) (CommentsListOutput, error) {
	ref, svcErr := parseCommentRef(commentRef)
	if svcErr != nil {
		return CommentsListOutput{}, svcErr
	}
	comments, err := b.Svc.ListComments(ctx, ref)
	if err != nil {
		return CommentsListOutput{}, err
	}
	out := CommentsListOutput{Comments: make([]CommentCompact, len(comments))}
	for i, c := range comments {
		out.Comments[i] = toCommentCompact(c)
	}
	return out, nil
}

func (b *InProcessBackend) UpdateComment(ctx context.Context, id, expectedVersion int64, body string) (CommentWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return CommentWriteResult{}, err
	}
	comment, err := b.Svc.EditComment(ctx, service.EditCommentRequest{
		CommentID: id, Body: body, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return CommentWriteResult{}, err
	}
	return CommentWriteResult{ID: comment.ID, Version: comment.Version, CreatedAt: comment.CreatedAt}, nil
}

func (b *InProcessBackend) DeleteComment(ctx context.Context, id, expectedVersion int64) (CommentDeleteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return CommentDeleteResult{}, err
	}
	if err := b.Svc.DeleteComment(ctx, service.DeleteCommentRequest{
		CommentID: id, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID()); err != nil {
		return CommentDeleteResult{}, err
	}
	return CommentDeleteResult{ID: id}, nil
}

func (b *InProcessBackend) GetCommentHistory(ctx context.Context, id int64) (CommentHistoryOutput, error) {
	versions, err := b.Svc.GetCommentHistory(ctx, id)
	if err != nil {
		return CommentHistoryOutput{}, err
	}
	return CommentHistoryOutput{Versions: versions}, nil
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

func (b *InProcessBackend) RemoveRelationship(ctx context.Context, sourceRef, relType, targetRef string) error {
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
	return b.Svc.RemoveRelationship(ctx, service.RemoveRelationshipRequest{
		SourceRef: src, TargetRef: tgt, Type: domain.RelationshipType(relType),
	}, actor, service.NewCorrelationID())
}

func (b *InProcessBackend) RemoveAssociation(ctx context.Context, sourceRef, targetRef string) error {
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
	return b.Svc.RemoveAssociation(ctx, service.RemoveAssociationRequest{SourceRef: src, TargetRef: tgt}, actor, service.NewCorrelationID())
}

func (b *InProcessBackend) AddLink(ctx context.Context, ref, title, url string) (LinkView, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return LinkView{}, err
	}
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return LinkView{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	link, err := b.Svc.AddExternalLink(ctx, service.AddExternalLinkRequest{
		Ref: parsedRef, Title: title, URL: url,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return LinkView{}, err
	}
	return LinkView{ID: link.ID, Title: link.Title, URL: link.URL}, nil
}

func (b *InProcessBackend) ListLinks(ctx context.Context, ref string) ([]LinkView, error) {
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return nil, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	links, err := b.Svc.GetExternalLinks(ctx, parsedRef)
	if err != nil {
		return nil, err
	}
	out := make([]LinkView, len(links))
	for i, l := range links {
		out[i] = LinkView{ID: l.ID, Title: l.Title, URL: l.URL}
	}
	return out, nil
}

func (b *InProcessBackend) RemoveLink(ctx context.Context, ref string, id int64) error {
	actor, err := mcpActor(ctx)
	if err != nil {
		return err
	}
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	return b.Svc.RemoveExternalLink(ctx, service.RemoveExternalLinkRequest{
		Ref: parsedRef, LinkID: id,
	}, actor, service.NewCorrelationID())
}

func (b *InProcessBackend) GetBacklinks(ctx context.Context, ref string) ([]BacklinkView, error) {
	parsedRef, perr := domain.Parse(ref)
	if perr != nil {
		return nil, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	backlinks, err := b.Svc.GetBacklinks(ctx, parsedRef)
	if err != nil {
		return nil, err
	}
	out := make([]BacklinkView, len(backlinks))
	for i, bl := range backlinks {
		out[i] = BacklinkView{Ref: bl.SourceRef, CommentID: bl.SourceCommentID}
	}
	return out, nil
}

func (b *InProcessBackend) GetAttachment(ctx context.Context, id int64) (AttachmentView, error) {
	a, err := b.Svc.GetAttachment(ctx, id)
	if err != nil {
		return AttachmentView{}, err
	}
	return attachmentViewFromDomain(a), nil
}

func (b *InProcessBackend) ListAttachments(ctx context.Context, ref string, commentID int64) ([]AttachmentView, error) {
	var attachments []domain.Attachment
	var err error
	if commentID != 0 {
		attachments, err = b.Svc.ListAttachmentsForComment(ctx, commentID)
	} else {
		parsed, parseErr := domain.Parse(ref)
		if parseErr != nil {
			return nil, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: parseErr.Error()}
		}
		attachments, err = b.Svc.ListAttachmentsForRef(ctx, parsed)
	}
	if err != nil {
		return nil, err
	}
	out := make([]AttachmentView, len(attachments))
	for i, a := range attachments {
		out[i] = attachmentViewFromDomain(a)
	}
	return out, nil
}

func (b *InProcessBackend) ListAttachmentVersions(ctx context.Context, id int64) ([]AttachmentVersionView, error) {
	versions, err := b.Svc.ListAttachmentVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]AttachmentVersionView, len(versions))
	for i, v := range versions {
		out[i] = AttachmentVersionView{
			Version: v.Version, Kind: string(v.Kind), FileName: v.FileName, FileSize: v.FileSize,
			MediaType: v.MediaType, Checksum: v.Checksum, PathValue: v.PathValue,
			UploadedBy: string(v.UploadedBy.Kind) + ":" + v.UploadedBy.Name, CreatedAt: v.CreatedAt,
		}
	}
	return out, nil
}

func attachmentViewFromDomain(a domain.Attachment) AttachmentView {
	return AttachmentView{
		ID: a.ID, OwnerRef: a.OwnerRef, CommentID: a.CommentID, Kind: string(a.Kind), Title: a.Title,
		CurrentVersion: a.CurrentVersion, FileName: a.FileName, FileSize: a.FileSize, MediaType: a.MediaType,
		Checksum: a.Checksum, PathValue: a.PathValue, CreatedAt: a.CreatedAt,
		Creator: string(a.Creator.Kind) + ":" + a.Creator.Name, DeletedAt: a.DeletedAt,
	}
}

func (b *InProcessBackend) GetFeature(ctx context.Context, ref string, includeDeleted ...bool) (domain.Feature, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return domain.Feature{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if parsed.Kind != domain.KindFeature {
		return domain.Feature{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a feature reference"}
	}
	if len(includeDeleted) > 0 && includeDeleted[0] {
		return b.Svc.GetFeatureIncludingDeleted(ctx, parsed)
	}
	return b.Svc.GetFeature(ctx, parsed)
}

func (b *InProcessBackend) CreateFeature(ctx context.Context, in CreateFeatureInput) (FeatureWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return FeatureWriteResult{}, err
	}
	if in.ProjectKey == "" {
		return FeatureWriteResult{}, errMissingProjectKey()
	}
	req := service.CreateFeatureRequest{
		ProjectKey: in.ProjectKey, Title: in.Title, Description: in.Description, Priority: domain.Priority(in.Priority),
	}
	var fingerprint string
	if in.IdempotencyKey != "" {
		fingerprint, err = mcpFingerprint("feature_create", req)
		if err != nil {
			return FeatureWriteResult{}, err
		}
	}
	f, err := b.Svc.CreateFeature(ctx, req, actor, service.NewCorrelationID(), in.IdempotencyKey, fingerprint)
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
	ref, perr := parseFeatureRefMCP(in.Ref)
	if perr != nil {
		return FeatureWriteResult{}, perr
	}
	f, err := b.Svc.UpdateFeature(ctx, service.UpdateFeatureRequest{
		Ref: ref, Title: in.Title, Description: in.Description, Priority: domain.Priority(in.Priority), ExpectedVersion: in.ExpectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return FeatureWriteResult{}, err
	}
	return toFeatureWriteResult(f), nil
}

func parseFeatureRefMCP(ref string) (domain.Reference, *service.Error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if parsed.Kind != domain.KindFeature {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a feature reference"}
	}
	return parsed, nil
}

func (b *InProcessBackend) SetFeatureStatus(ctx context.Context, ref, status string, expectedVersion int64) (FeatureWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return FeatureWriteResult{}, err
	}
	parsedRef, perr := parseFeatureRefMCP(ref)
	if perr != nil {
		return FeatureWriteResult{}, perr
	}
	f, err := b.Svc.UpdateFeatureStatus(ctx, service.UpdateFeatureStatusRequest{
		Ref: parsedRef, NewStatus: domain.WorkflowStatus(status), ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return FeatureWriteResult{}, err
	}
	return toFeatureWriteResult(f), nil
}

func (b *InProcessBackend) ReorderFeature(ctx context.Context, ref string, afterRef *string, expectedVersion int64) (FeatureWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return FeatureWriteResult{}, err
	}
	parsedRef, perr := parseFeatureRefMCP(ref)
	if perr != nil {
		return FeatureWriteResult{}, perr
	}
	var parsedAfter *domain.Reference
	if afterRef != nil {
		p, aerr := domain.Parse(*afterRef)
		if aerr != nil {
			return FeatureWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "after_ref", Message: aerr.Error()}
		}
		parsedAfter = &p
	}
	f, err := b.Svc.ReorderFeature(ctx, service.ReorderFeatureRequest{
		Ref: parsedRef, AfterRef: parsedAfter, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return FeatureWriteResult{}, err
	}
	return toFeatureWriteResult(f), nil
}

func (b *InProcessBackend) DeleteFeature(ctx context.Context, ref string, cascade bool, expectedVersion int64) (DeleteWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return DeleteWriteResult{}, err
	}
	parsedRef, perr := parseFeatureRefMCP(ref)
	if perr != nil {
		return DeleteWriteResult{}, perr
	}
	newVersion, err := b.Svc.DeleteFeature(ctx, service.DeleteFeatureRequest{
		Ref: parsedRef, Cascade: cascade, ExpectedVersion: expectedVersion,
	}, actor, service.NewCorrelationID())
	if err != nil {
		return DeleteWriteResult{}, err
	}
	return DeleteWriteResult{Ref: ref, Version: newVersion}, nil
}

func (b *InProcessBackend) RestoreFeature(ctx context.Context, ref string, expectedVersion int64) (FeatureWriteResult, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return FeatureWriteResult{}, err
	}
	parsedRef, perr := parseFeatureRefMCP(ref)
	if perr != nil {
		return FeatureWriteResult{}, perr
	}
	f, err := b.Svc.RestoreFeature(ctx, service.RestoreFeatureRequest{
		Ref: parsedRef, ExpectedVersion: expectedVersion,
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
	if in.ProjectKey == "" {
		return DecisionWriteResult{}, errMissingProjectKey()
	}
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
	if in.ProjectKey == "" {
		return ContentItemWriteResult{}, errMissingProjectKey()
	}
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

// UpdateContentItem applies an optional status move first (ADR 0028),
// then the field replacement record_update always carries (Title is
// unconditionally required for a content item, unlike UpdateProject's
// optional Title/Description — so unlike UpdateProject, the field step
// here is never skipped, only ever preceded by a status move). See
// UpdateContentItemInput's doc comment for why the two are split
// rather than merged into one service call.
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
	corrID := service.NewCorrelationID()
	ifMatch := in.ExpectedVersion

	if in.Status != nil {
		c, err := b.Svc.SetContentItemStatus(ctx, service.SetContentItemStatusRequest{
			Ref: ref, NewStatus: domain.ContentItemStatus(*in.Status), ExpectedVersion: ifMatch,
		}, actor, corrID)
		if err != nil {
			return ContentItemWriteResult{}, err
		}
		ifMatch = c.Version
	}

	c, err := b.Svc.UpdateContentItem(ctx, service.UpdateContentItemRequest{
		Ref: ref, Title: in.Title, Body: in.Body, PathValue: in.Path, URLValue: in.URL, ExpectedVersion: ifMatch,
	}, actor, corrID)
	if err != nil {
		return ContentItemWriteResult{}, err
	}
	return toContentItemWriteResult(c), nil
}

func (b *InProcessBackend) ListDecisions(ctx context.Context, projectKey string, limit int, cursor string) (RecordsListOutput, error) {
	result, err := b.Svc.ListDecisions(ctx, projectKey, limit, cursor)
	if err != nil {
		return RecordsListOutput{}, err
	}
	out := RecordsListOutput{Records: make([]RecordCompact, len(result.Decisions)), NextCursor: result.NextCursor}
	for i, d := range result.Decisions {
		out.Records[i] = RecordCompact{Ref: d.Ref, Kind: "decision", Title: d.Title, Status: string(d.Status), Version: d.Version, UpdatedAt: d.UpdatedAt}
	}
	return out, nil
}

func (b *InProcessBackend) GetDecisionVersions(ctx context.Context, ref string) (RecordVersionsOutput, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return RecordVersionsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if parsed.Kind != domain.KindDecision {
		return RecordVersionsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a decision reference"}
	}
	versions, err := b.Svc.ListDecisionVersions(ctx, parsed)
	if err != nil {
		return RecordVersionsOutput{}, err
	}
	out := RecordVersionsOutput{Versions: make([]RecordVersion, len(versions))}
	for i, v := range versions {
		out.Versions[i] = RecordVersion{
			Version: v.Version, Title: v.Title, Context: v.Context, Decision: v.Decision,
			Rationale: v.Rationale, Consequences: v.Consequences, Status: string(v.Status),
			EditedBy: string(v.EditedBy.Kind) + ":" + v.EditedBy.Name, CreatedAt: v.CreatedAt,
		}
	}
	return out, nil
}

func (b *InProcessBackend) GetDecisionDiff(ctx context.Context, ref string, from, to int64) (RecordDiff, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return RecordDiff{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if parsed.Kind != domain.KindDecision {
		return RecordDiff{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a decision reference"}
	}
	diff, err := b.Svc.GetDecisionDiff(ctx, parsed, from, to)
	if err != nil {
		return RecordDiff{}, err
	}
	return RecordDiff{
		FromVersion: diff.FromVersion, ToVersion: diff.ToVersion,
		Title: toDiffLineViews(diff.Fields.Title), Context: toDiffLineViews(diff.Fields.Context),
		Decision: toDiffLineViews(diff.Fields.Decision), Rationale: toDiffLineViews(diff.Fields.Rationale),
		Consequences: toDiffLineViews(diff.Fields.Consequences),
		StatusFrom:   string(diff.StatusFrom), StatusTo: string(diff.StatusTo),
	}, nil
}

func (b *InProcessBackend) ListContentItems(ctx context.Context, projectKey, kind string, limit int, cursor string, includeArchivedValues ...bool) (RecordsListOutput, error) {
	includeArchived := len(includeArchivedValues) > 0 && includeArchivedValues[0]
	result, err := b.Svc.ListContentItems(ctx, projectKey, domain.EntityKind(kind), limit, cursor, includeArchived)
	if err != nil {
		return RecordsListOutput{}, err
	}
	out := RecordsListOutput{Records: make([]RecordCompact, len(result.Items)), NextCursor: result.NextCursor}
	for i, c := range result.Items {
		out.Records[i] = RecordCompact{Ref: c.Ref, Kind: string(c.Kind), Title: c.Title, Status: string(c.Status), Version: c.Version, UpdatedAt: c.UpdatedAt}
	}
	return out, nil
}

func (b *InProcessBackend) GetContentItemVersions(ctx context.Context, ref string) (RecordVersionsOutput, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return RecordVersionsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if parsed.Kind != domain.KindPlan && parsed.Kind != domain.KindDocument {
		return RecordVersionsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	versions, err := b.Svc.ListContentItemVersions(ctx, parsed)
	if err != nil {
		return RecordVersionsOutput{}, err
	}
	out := RecordVersionsOutput{Versions: make([]RecordVersion, len(versions))}
	for i, v := range versions {
		out.Versions[i] = RecordVersion{
			Version: v.Version, Title: v.Title, Representation: v.Representation, Body: v.Body,
			FileName: v.FileName, FileSize: v.FileSize, MediaType: v.MediaType, Checksum: v.Checksum,
			PathValue: v.PathValue, URLValue: v.URLValue,
			EditedBy: string(v.EditedBy.Kind) + ":" + v.EditedBy.Name, CreatedAt: v.CreatedAt,
		}
	}
	return out, nil
}

func (b *InProcessBackend) GetContentItemDiff(ctx context.Context, ref string, from, to int64) (RecordDiff, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return RecordDiff{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	if parsed.Kind != domain.KindPlan && parsed.Kind != domain.KindDocument {
		return RecordDiff{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	diff, err := b.Svc.GetContentItemDiff(ctx, parsed, from, to)
	if err != nil {
		return RecordDiff{}, err
	}
	return RecordDiff{
		FromVersion: diff.FromVersion, ToVersion: diff.ToVersion,
		Title: toDiffLineViews(diff.Title), Body: toDiffLineViews(diff.Body),
	}, nil
}

func (b *InProcessBackend) Search(ctx context.Context, in SearchInput) (SearchOutput, error) {
	result, err := b.Svc.Search(ctx, service.SearchRequest{
		Query: in.Query, ProjectKey: in.Project, Kinds: in.Kind, Status: in.Status, Limit: in.Limit, Cursor: in.Cursor,
	})
	if err != nil {
		return SearchOutput{}, err
	}
	out := SearchOutput{Hits: make([]SearchHit, len(result.Hits)), NextCursor: result.NextCursor}
	for i, h := range result.Hits {
		out.Hits[i] = SearchHit{Kind: h.Kind, Ref: h.Ref, CommentID: h.CommentID, Title: h.Title, Snippet: h.Snippet}
	}
	return out, nil
}

func (b *InProcessBackend) ListNotifications(ctx context.Context, unreadOnly bool, limit int, cursor string) (NotificationsListOutput, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return NotificationsListOutput{}, err
	}
	result, err := b.Svc.ListNotifications(ctx, actor, unreadOnly, limit, cursor)
	if err != nil {
		return NotificationsListOutput{}, err
	}
	out := NotificationsListOutput{Notifications: make([]NotificationCompact, len(result.Notifications)), NextCursor: result.NextCursor}
	for i, n := range result.Notifications {
		nc := NotificationCompact{
			ID: n.ID, Kind: n.Kind, Entity: n.Entity, EntityKind: string(n.EntityKind),
			CommentID: n.CommentID, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
		}
		if n.TriggeredBy != nil {
			nc.TriggeredBy = n.TriggeredBy.String()
		}
		out.Notifications[i] = nc
	}
	return out, nil
}

func (b *InProcessBackend) GetProjectBrief(ctx context.Context, key string) (ProjectBrief, error) {
	brief, err := b.Svc.ProjectBrief(ctx, key)
	if err != nil {
		return ProjectBrief{}, err
	}
	return toProjectBrief(brief), nil
}

// activityCommentExcerptLimit mirrors internal/httpapi/activity.go's
// activityCommentExcerptLimit — this backend bypasses that handler
// entirely, so it needs its own copy of the same truncation.
const activityCommentExcerptLimit = 200

// truncateActivityExcerpt mirrors internal/httpapi/activity.go's
// toActivityEvent truncation: cut by rune (never mid-UTF-8-byte), then
// back up to the last word boundary within that cut so the excerpt
// never ends mid-word, and mark a real truncation with an ellipsis.
func truncateActivityExcerpt(body string) string {
	runes := []rune(body)
	if len(runes) <= activityCommentExcerptLimit {
		return body
	}
	cut := string(runes[:activityCommentExcerptLimit])
	if boundary := strings.LastIndexFunc(cut, unicode.IsSpace); boundary > 0 {
		cut = cut[:boundary]
	}
	return strings.TrimRight(cut, " \t\n\r") + "…"
}

func (b *InProcessBackend) ListActivity(ctx context.Context, projectKey, actor, entityKind, eventType string, limit int, cursor string) (ActivityListOutput, error) {
	result, err := b.Svc.ListActivity(ctx, projectKey, service.ActivityListFilters{
		Actor: actor, EntityKind: entityKind, EventType: eventType,
	}, limit, cursor)
	if err != nil {
		return ActivityListOutput{}, err
	}
	out := ActivityListOutput{Events: make([]ActivityEventView, len(result.Events)), NextCursor: result.NextCursor}
	for i, e := range result.Events {
		v := ActivityEventView{
			ID: e.ID, Entity: e.EntityRef, EntityKind: string(e.EntityKind),
			Actor: e.Actor.String(), EventType: e.EventType, CreatedAt: e.CreatedAt,
		}
		if e.CommentID != nil {
			v.CommentID = *e.CommentID
		}
		if e.CommentBody != nil {
			v.CommentExcerpt = truncateActivityExcerpt(*e.CommentBody)
		}
		out.Events[i] = v
	}
	return out, nil
}

func (b *InProcessBackend) MarkNotificationsRead(ctx context.Context, ids []int64, all bool) (int64, error) {
	actor, err := mcpActor(ctx)
	if err != nil {
		return 0, err
	}
	return b.Svc.MarkNotificationsRead(ctx, service.MarkNotificationsReadRequest{IDs: ids, All: all}, actor, service.NewCorrelationID())
}

func (b *InProcessBackend) SetSubscription(ctx context.Context, ref string, subscribed bool) error {
	actor, err := mcpActor(ctx)
	if err != nil {
		return err
	}
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	req := service.SubscribeRequest{Ref: parsed}
	if subscribed {
		return b.Svc.Subscribe(ctx, req, actor, service.NewCorrelationID())
	}
	return b.Svc.Unsubscribe(ctx, req, actor, service.NewCorrelationID())
}

// CreateTicket requires exactly one of in.Feature/in.General, the same
// requirement the HTTP API's createTicket handler enforces
// (internal/httpapi/tickets.go) — this backend calls service directly,
// bypassing that handler, so it can't rely on it to reject an
// ambiguous or fully-default request.
func (b *InProcessBackend) CreateTicket(ctx context.Context, in CreateTicketInput) (domain.Ticket, error) {
	if in.ProjectKey == "" {
		return domain.Ticket{}, errMissingProjectKey()
	}
	actor, err := mcpActor(ctx)
	if err != nil {
		return domain.Ticket{}, err
	}
	if in.Feature != "" && in.General {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "feature", Message: "specify feature or general, not both"}
	}
	if in.Feature == "" && !in.General {
		return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "feature", Message: "feature or general is required"}
	}
	var featureRef domain.Reference
	if in.Feature != "" {
		featureRef, err = domain.Parse(in.Feature)
		if err != nil {
			return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "feature", Message: err.Error()}
		}
		if featureRef.Kind != domain.KindFeature {
			return domain.Ticket{}, &service.Error{Code: domain.ErrValidationFailed, Field: "feature", Message: "reference must be a feature reference"}
		}
	}
	var severity *domain.Severity
	if in.Severity != "" {
		s := domain.Severity(in.Severity)
		severity = &s
	}
	req := service.CreateTicketRequest{
		ProjectKey:        in.ProjectKey,
		Type:              domain.TicketType(in.Type),
		Title:             in.Title,
		Description:       in.Description,
		Priority:          domain.Priority(in.Priority),
		Severity:          severity,
		FeatureRef:        featureRef,
		UseGeneralFeature: in.General,
	}
	var fingerprint string
	if in.IdempotencyKey != "" {
		fingerprint, err = mcpFingerprint("ticket_create", req)
		if err != nil {
			return domain.Ticket{}, err
		}
	}
	return b.Svc.CreateTicket(ctx, req, actor, service.NewCorrelationID(), in.IdempotencyKey, fingerprint)
}
