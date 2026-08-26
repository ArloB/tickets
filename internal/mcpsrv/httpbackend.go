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
		return &service.Error{Code: domain.ErrInternal, Message: "could not reach the Tickets API"}
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
		DeletedAt:   t.DeletedAt,
	}, nil
}

func toDomainFeature(f apiclient.Feature) domain.Feature {
	return domain.Feature{
		Ref: f.Ref, ProjectKey: f.Project, Title: f.Title, Description: f.Description,
		Status: domain.WorkflowStatus(f.Status), Priority: domain.Priority(f.Priority),
		Version: f.Version, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt, DeletedAt: f.DeletedAt,
	}
}

func (b *HTTPBackend) GetFeature(ctx context.Context, ref string, includeDeleted ...bool) (domain.Feature, error) {
	var f apiclient.Feature
	var err error
	if len(includeDeleted) > 0 && includeDeleted[0] {
		f, err = b.Client.GetFeatureIncludingDeleted(ctx, ref)
	} else {
		f, err = b.Client.GetFeature(ctx, ref)
	}
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
	}, in.IdempotencyKey)
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

func (b *HTTPBackend) SetFeatureStatus(ctx context.Context, ref, status string, expectedVersion int64) (FeatureWriteResult, error) {
	f, err := b.Client.SetFeatureStatus(ctx, ref, status, expectedVersion)
	if err != nil {
		return FeatureWriteResult{}, toServiceError(err)
	}
	return toFeatureWriteResult(toDomainFeature(f)), nil
}

func (b *HTTPBackend) ReorderFeature(ctx context.Context, ref string, afterRef *string, expectedVersion int64) (FeatureWriteResult, error) {
	f, err := b.Client.ReorderFeature(ctx, ref, afterRef, expectedVersion)
	if err != nil {
		return FeatureWriteResult{}, toServiceError(err)
	}
	return toFeatureWriteResult(toDomainFeature(f)), nil
}

func (b *HTTPBackend) DeleteFeature(ctx context.Context, ref string, cascade bool, expectedVersion int64) (DeleteWriteResult, error) {
	newVersion, err := b.Client.DeleteFeature(ctx, ref, cascade, expectedVersion)
	if err != nil {
		return DeleteWriteResult{}, toServiceError(err)
	}
	return DeleteWriteResult{Ref: ref, Version: newVersion}, nil
}

func (b *HTTPBackend) RestoreFeature(ctx context.Context, ref string, expectedVersion int64) (FeatureWriteResult, error) {
	f, err := b.Client.RestoreFeature(ctx, ref, expectedVersion)
	if err != nil {
		return FeatureWriteResult{}, toServiceError(err)
	}
	return toFeatureWriteResult(toDomainFeature(f)), nil
}

func toDomainDecision(d apiclient.Decision) domain.Decision {
	return domain.Decision{
		Ref: d.Ref, ProjectKey: d.Project, Title: d.Title, Context: d.Context,
		Decision: d.Decision, Rationale: d.Rationale, Consequences: d.Consequences,
		Status: domain.DecisionStatus(d.Status), SupersededBy: d.SupersededBy,
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
		Title: in.Title, Context: in.Context, Decision: in.Decision, Rationale: in.Rationale, Consequences: in.Consequences,
	}, in.IdempotencyKey)
	if err != nil {
		return DecisionWriteResult{}, toServiceError(err)
	}
	return toDecisionWriteResult(toDomainDecision(d)), nil
}

func (b *HTTPBackend) UpdateDecision(ctx context.Context, in UpdateDecisionInput) (DecisionWriteResult, error) {
	d, err := b.Client.UpdateDecision(ctx, in.Ref, apiclient.UpdateDecisionRequest{
		Title: in.Title, Context: in.Context, Decision: in.Decision, Rationale: in.Rationale,
		Consequences: in.Consequences, Status: in.Status, SupersededBy: in.SupersededBy,
	}, in.ExpectedVersion)
	if err != nil {
		return DecisionWriteResult{}, toServiceError(err)
	}
	return toDecisionWriteResult(toDomainDecision(d)), nil
}

func (b *HTTPBackend) ListDecisions(ctx context.Context, projectKey string, limit int, cursor string) (RecordsListOutput, error) {
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return RecordsListOutput{}, errMissingProjectKey()
	}
	page, err := b.Client.ListDecisions(ctx, projectKey, limit, cursor)
	if err != nil {
		return RecordsListOutput{}, toServiceError(err)
	}
	out := RecordsListOutput{Records: make([]RecordCompact, len(page.Decisions)), NextCursor: page.NextCursor}
	for i, d := range page.Decisions {
		out.Records[i] = RecordCompact{Ref: d.Ref, Kind: "decision", Title: d.Title, Status: d.Status, Version: d.Version, UpdatedAt: d.UpdatedAt}
	}
	return out, nil
}

func (b *HTTPBackend) GetDecisionVersions(ctx context.Context, ref string) (RecordVersionsOutput, error) {
	page, err := b.Client.ListDecisionVersions(ctx, ref)
	if err != nil {
		return RecordVersionsOutput{}, toServiceError(err)
	}
	out := RecordVersionsOutput{Versions: make([]RecordVersion, len(page.Versions))}
	for i, v := range page.Versions {
		out.Versions[i] = RecordVersion{
			Version: v.Version, Title: v.Title, Context: v.Context, Decision: v.Decision,
			Rationale: v.Rationale, Consequences: v.Consequences, Status: v.Status,
			EditedBy: v.EditedBy, CreatedAt: v.CreatedAt,
		}
	}
	return out, nil
}

func (b *HTTPBackend) GetDecisionDiff(ctx context.Context, ref string, from, to int64) (RecordDiff, error) {
	diff, err := b.Client.GetDecisionDiff(ctx, ref, from, to)
	if err != nil {
		return RecordDiff{}, toServiceError(err)
	}
	return RecordDiff{
		FromVersion: diff.FromVersion, ToVersion: diff.ToVersion,
		Title: toAPIDiffLineViews(diff.Title), Context: toAPIDiffLineViews(diff.Context),
		Decision: toAPIDiffLineViews(diff.Decision), Rationale: toAPIDiffLineViews(diff.Rationale),
		Consequences: toAPIDiffLineViews(diff.Consequences),
		StatusFrom:   diff.StatusFrom, StatusTo: diff.StatusTo,
	}, nil
}

// toAPIDiffLineViews converts apiclient.DiffLine (the HTTP-bridge's
// own copy of the same shape, per apiclient/decisions.go's doc) to
// DiffLineView — HTTPBackend's edge-translation counterpart to
// InProcessBackend's toDiffLineViews (domain.DiffLine).
func toAPIDiffLineViews(lines []apiclient.DiffLine) []DiffLineView {
	out := make([]DiffLineView, len(lines))
	for i, l := range lines {
		out[i] = DiffLineView{Op: l.Op, Text: l.Text}
	}
	return out
}

// toDomainContentItem converts apiclient.ContentItem into
// domain.ContentItem — the same edge-translation role
// toDomainDecision/toDomainTicket play for HTTPBackend.
func toDomainContentItem(c apiclient.ContentItem) domain.ContentItem {
	return domain.ContentItem{
		Ref: c.Ref, ProjectKey: c.Project, Kind: domain.EntityKind(c.Kind), Title: c.Title,
		Representation: c.Representation, Body: c.Body,
		FileName: c.FileName, FileSize: c.FileSize, MediaType: c.MediaType, Checksum: c.Checksum,
		PathValue: c.PathValue, URLValue: c.URLValue,
		Version: c.Version, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// contentItemURLKind maps an entity kind to the URL segment
// apiclient's kind-parameterized ContentItem methods expect ("plans"/
// "documents"), or "" if kind is neither — the one place this mapping
// lives, rather than a Plan/Document case in every caller.
func contentItemURLKind(kind domain.EntityKind) string {
	switch kind {
	case domain.KindPlan:
		return "plans"
	case domain.KindDocument:
		return "documents"
	default:
		return ""
	}
}

func (b *HTTPBackend) GetContentItem(ctx context.Context, ref string) (domain.ContentItem, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return domain.ContentItem{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	urlKind := contentItemURLKind(parsed.Kind)
	if urlKind == "" {
		return domain.ContentItem{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	item, err := b.Client.GetContentItem(ctx, urlKind, ref)
	if err != nil {
		return domain.ContentItem{}, toServiceError(err)
	}
	return toDomainContentItem(item), nil
}

func (b *HTTPBackend) CreateContentItem(ctx context.Context, in CreateContentItemInput) (ContentItemWriteResult, error) {
	projectKey := in.ProjectKey
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return ContentItemWriteResult{}, errMissingProjectKey()
	}
	urlKind := contentItemURLKind(domain.EntityKind(in.Kind))
	if urlKind == "" {
		return ContentItemWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "kind", Message: "kind must be \"plan\" or \"document\""}
	}
	item, err := b.Client.CreateContentItem(ctx, urlKind, projectKey, apiclient.CreateContentItemRequest{
		Title: in.Title, Representation: in.Representation, Body: in.Body, Path: in.Path, URL: in.URL,
	}, in.IdempotencyKey)
	if err != nil {
		return ContentItemWriteResult{}, toServiceError(err)
	}
	return toContentItemWriteResult(toDomainContentItem(item)), nil
}

func (b *HTTPBackend) UpdateContentItem(ctx context.Context, in UpdateContentItemInput) (ContentItemWriteResult, error) {
	parsed, perr := domain.Parse(in.Ref)
	if perr != nil {
		return ContentItemWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	urlKind := contentItemURLKind(parsed.Kind)
	if urlKind == "" {
		return ContentItemWriteResult{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	item, err := b.Client.UpdateContentItem(ctx, urlKind, in.Ref, apiclient.UpdateContentItemRequest{
		Title: in.Title, Body: in.Body, Path: in.Path, URL: in.URL,
	}, in.ExpectedVersion)
	if err != nil {
		return ContentItemWriteResult{}, toServiceError(err)
	}
	return toContentItemWriteResult(toDomainContentItem(item)), nil
}

func (b *HTTPBackend) ListContentItems(ctx context.Context, projectKey, kind string, limit int, cursor string) (RecordsListOutput, error) {
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return RecordsListOutput{}, errMissingProjectKey()
	}
	urlKind := contentItemURLKind(domain.EntityKind(kind))
	if urlKind == "" {
		return RecordsListOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "kind", Message: "kind must be \"plan\" or \"document\""}
	}
	page, err := b.Client.ListContentItems(ctx, urlKind, projectKey, limit, cursor)
	if err != nil {
		return RecordsListOutput{}, toServiceError(err)
	}
	out := RecordsListOutput{Records: make([]RecordCompact, len(page.Items)), NextCursor: page.NextCursor}
	for i, c := range page.Items {
		out.Records[i] = RecordCompact{Ref: c.Ref, Kind: c.Kind, Title: c.Title, Version: c.Version, UpdatedAt: c.UpdatedAt}
	}
	return out, nil
}

func (b *HTTPBackend) GetContentItemVersions(ctx context.Context, ref string) (RecordVersionsOutput, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return RecordVersionsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	urlKind := contentItemURLKind(parsed.Kind)
	if urlKind == "" {
		return RecordVersionsOutput{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	page, err := b.Client.ListContentItemVersions(ctx, urlKind, ref)
	if err != nil {
		return RecordVersionsOutput{}, toServiceError(err)
	}
	out := RecordVersionsOutput{Versions: make([]RecordVersion, len(page.Versions))}
	for i, v := range page.Versions {
		out.Versions[i] = RecordVersion{
			Version: v.Version, Title: v.Title, Representation: v.Representation, Body: v.Body,
			FileName: v.FileName, FileSize: v.FileSize, MediaType: v.MediaType, Checksum: v.Checksum,
			PathValue: v.PathValue, URLValue: v.URLValue,
			EditedBy: v.EditedBy, CreatedAt: v.CreatedAt,
		}
	}
	return out, nil
}

func (b *HTTPBackend) GetContentItemDiff(ctx context.Context, ref string, from, to int64) (RecordDiff, error) {
	parsed, perr := domain.Parse(ref)
	if perr != nil {
		return RecordDiff{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: perr.Error()}
	}
	urlKind := contentItemURLKind(parsed.Kind)
	if urlKind == "" {
		return RecordDiff{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a plan or document reference"}
	}
	diff, err := b.Client.GetContentItemDiff(ctx, urlKind, ref, from, to)
	if err != nil {
		return RecordDiff{}, toServiceError(err)
	}
	return RecordDiff{
		FromVersion: diff.FromVersion, ToVersion: diff.ToVersion,
		Title: toAPIDiffLineViews(diff.Title), Body: toAPIDiffLineViews(diff.Body),
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

func (b *HTTPBackend) ListProjects(ctx context.Context, limit int, cursor string, includeArchivedValues ...bool) (ProjectsListOutput, error) {
	includeArchived := len(includeArchivedValues) > 0 && includeArchivedValues[0]
	page, err := b.Client.ListProjects(ctx, limit, cursor, includeArchived)
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

// UpdateProject mirrors InProcessBackend.UpdateProject's status-then-
// fields merge, but over apiclient's SetProjectStatus/UpdateProject
// (PATCH .../{key} needs a full title+description body, so a
// fields-only request with Description omitted merge-fetches the
// current one first — the same reason the CLI's `project update`
// does).
func (b *HTTPBackend) UpdateProject(ctx context.Context, in UpdateProjectInput) (domain.Project, error) {
	ifMatch := in.ExpectedVersion
	var result apiclient.Project
	resultKnown := false

	if in.Status != nil {
		p, err := b.Client.SetProjectStatus(ctx, in.Key, *in.Status, ifMatch)
		if err != nil {
			return domain.Project{}, toServiceError(err)
		}
		result, resultKnown = p, true
		ifMatch = p.Version
	}

	if in.Title != nil || in.Description != nil {
		base := result
		if !resultKnown {
			p, err := b.Client.GetProject(ctx, in.Key)
			if err != nil {
				return domain.Project{}, toServiceError(err)
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
		p, err := b.Client.UpdateProject(ctx, in.Key, title, desc, ifMatch)
		if err != nil {
			return domain.Project{}, toServiceError(err)
		}
		result, resultKnown = p, true
	}

	if !resultKnown {
		p, err := b.Client.GetProject(ctx, in.Key)
		if err != nil {
			return domain.Project{}, toServiceError(err)
		}
		result = p
	}
	return toDomainProject(result), nil
}

func (b *HTTPBackend) ListFeatures(ctx context.Context, projectKey string, filters FeatureListFilters, limit int, cursor string) (FeaturesListOutput, error) {
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return FeaturesListOutput{}, errMissingProjectKey()
	}
	page, err := b.Client.ListFeaturesFiltered(ctx, projectKey, apiclient.FeatureListFilters{
		Status: filters.Status, Priority: filters.Priority, Creator: filters.Creator, UpdatedSince: filters.UpdatedSince,
	}, limit, cursor)
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

func (b *HTTPBackend) ListTickets(ctx context.Context, projectKey, view string, filters TicketListFilters, limit int, cursor string) (TicketsListOutput, error) {
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return TicketsListOutput{}, errMissingProjectKey()
	}
	page, err := b.Client.ListTickets(ctx, projectKey, view, apiclient.TicketListFilters{
		Status: filters.Status, Type: filters.Type, Severity: filters.Severity, Priority: filters.Priority,
		FeatureRef: filters.FeatureRef, Assignee: filters.Assignee, Creator: filters.Creator,
		UpdatedSince: filters.UpdatedSince,
	}, limit, cursor)
	if err != nil {
		return TicketsListOutput{}, toServiceError(err)
	}
	out := TicketsListOutput{Tickets: make([]TicketCompact, len(page.Tickets)), NextCursor: page.NextCursor}
	for i, t := range page.Tickets {
		out.Tickets[i] = fromAPITicketCompact(t)
	}
	return out, nil
}

func (b *HTTPBackend) GetTicket(ctx context.Context, ref string, includeDeleted ...bool) (domain.Ticket, error) {
	var t apiclient.Ticket
	var err error
	if len(includeDeleted) > 0 && includeDeleted[0] {
		t, err = b.Client.GetTicketIncludingDeleted(ctx, ref)
	} else {
		t, err = b.Client.GetTicket(ctx, ref)
	}
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

func (b *HTTPBackend) MoveTicketFeature(ctx context.Context, ref, featureRef string, expectedVersion int64) (domain.Ticket, error) {
	t, err := b.Client.MoveTicket(ctx, ref, featureRef, expectedVersion)
	if err != nil {
		return domain.Ticket{}, toServiceError(err)
	}
	return toDomainTicket(t)
}

func (b *HTTPBackend) AssignTicket(ctx context.Context, ref string, assignee *string, expectedVersion int64) (TicketWriteResult, error) {
	t, err := b.Client.AssignTicket(ctx, ref, assignee, expectedVersion)
	if err != nil {
		return TicketWriteResult{}, toServiceError(err)
	}
	ticket, err := toDomainTicket(t)
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

func (b *HTTPBackend) ReorderTicket(ctx context.Context, ref string, afterRef *string, expectedVersion int64) (TicketWriteResult, error) {
	t, err := b.Client.ReorderTicket(ctx, ref, afterRef, expectedVersion)
	if err != nil {
		return TicketWriteResult{}, toServiceError(err)
	}
	ticket, err := toDomainTicket(t)
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

func (b *HTTPBackend) DeleteTicket(ctx context.Context, ref string, expectedVersion int64) (DeleteWriteResult, error) {
	newVersion, err := b.Client.DeleteTicket(ctx, ref, expectedVersion)
	if err != nil {
		return DeleteWriteResult{}, toServiceError(err)
	}
	return DeleteWriteResult{Ref: ref, Version: newVersion}, nil
}

func (b *HTTPBackend) RestoreTicket(ctx context.Context, ref string, expectedVersion int64) (TicketWriteResult, error) {
	t, err := b.Client.RestoreTicket(ctx, ref, expectedVersion)
	if err != nil {
		return TicketWriteResult{}, toServiceError(err)
	}
	ticket, err := toDomainTicket(t)
	if err != nil {
		return TicketWriteResult{}, err
	}
	return toTicketWriteResult(ticket), nil
}

func (b *HTTPBackend) AddComment(ctx context.Context, ref, body, idempotencyKey string) (CommentWriteResult, error) {
	c, err := b.Client.CreateComment(ctx, ref, body, idempotencyKey)
	if err != nil {
		return CommentWriteResult{}, toServiceError(err)
	}
	return CommentWriteResult{ID: c.ID, Version: c.Version, CreatedAt: c.CreatedAt}, nil
}

// toDomainComment converts apiclient.Comment to domain.Comment,
// parsing Author back into an ActorRef the way toDomainTicket parses
// Assignee/Creator.
func toDomainComment(c apiclient.Comment) (domain.Comment, error) {
	author, err := domain.ParseActorRef(c.Author)
	if err != nil {
		return domain.Comment{}, err
	}
	return domain.Comment{
		ID: c.ID, Author: author, Body: c.Body, Version: c.Version,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, DeletedAt: c.DeletedAt,
	}, nil
}

func (b *HTTPBackend) GetComment(ctx context.Context, id int64) (domain.Comment, error) {
	c, err := b.Client.GetComment(ctx, id)
	if err != nil {
		return domain.Comment{}, toServiceError(err)
	}
	return toDomainComment(c)
}

func (b *HTTPBackend) ListComments(ctx context.Context, ref string) (CommentsListOutput, error) {
	page, err := b.Client.ListComments(ctx, ref)
	if err != nil {
		return CommentsListOutput{}, toServiceError(err)
	}
	out := CommentsListOutput{Comments: make([]CommentCompact, len(page.Comments))}
	for i, c := range page.Comments {
		comment, err := toDomainComment(c)
		if err != nil {
			return CommentsListOutput{}, err
		}
		out.Comments[i] = toCommentCompact(comment)
	}
	return out, nil
}

func (b *HTTPBackend) UpdateComment(ctx context.Context, id, expectedVersion int64, body string) (CommentWriteResult, error) {
	c, err := b.Client.EditComment(ctx, id, expectedVersion, body)
	if err != nil {
		return CommentWriteResult{}, toServiceError(err)
	}
	return CommentWriteResult{ID: c.ID, Version: c.Version, CreatedAt: c.CreatedAt}, nil
}

func (b *HTTPBackend) DeleteComment(ctx context.Context, id, expectedVersion int64) (CommentDeleteResult, error) {
	if err := b.Client.DeleteComment(ctx, id, expectedVersion); err != nil {
		return CommentDeleteResult{}, toServiceError(err)
	}
	return CommentDeleteResult{ID: id}, nil
}

func (b *HTTPBackend) GetCommentHistory(ctx context.Context, id int64) (CommentHistoryOutput, error) {
	page, err := b.Client.GetCommentHistory(ctx, id)
	if err != nil {
		return CommentHistoryOutput{}, toServiceError(err)
	}
	out := CommentHistoryOutput{Versions: make([]domain.CommentVersion, len(page.Versions))}
	for i, v := range page.Versions {
		editedBy, err := domain.ParseActorRef(v.EditedBy)
		if err != nil {
			return CommentHistoryOutput{}, err
		}
		out.Versions[i] = domain.CommentVersion{Version: v.Version, Body: v.Body, EditedBy: editedBy, CreatedAt: v.CreatedAt}
	}
	return out, nil
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

func (b *HTTPBackend) RemoveRelationship(ctx context.Context, sourceRef, relType, targetRef string) error {
	if err := b.Client.RemoveRelationship(ctx, sourceRef, relType, targetRef); err != nil {
		return toServiceError(err)
	}
	return nil
}

func (b *HTTPBackend) RemoveAssociation(ctx context.Context, sourceRef, targetRef string) error {
	if err := b.Client.RemoveAssociation(ctx, sourceRef, targetRef); err != nil {
		return toServiceError(err)
	}
	return nil
}

func (b *HTTPBackend) AddLink(ctx context.Context, ref, title, url string) (LinkView, error) {
	l, err := b.Client.AddLink(ctx, ref, apiclient.AddLinkRequest{Title: title, URL: url})
	if err != nil {
		return LinkView{}, toServiceError(err)
	}
	return LinkView{ID: l.ID, Title: l.Title, URL: l.URL}, nil
}

func (b *HTTPBackend) ListLinks(ctx context.Context, ref string) ([]LinkView, error) {
	links, err := b.Client.ListLinks(ctx, ref)
	if err != nil {
		return nil, toServiceError(err)
	}
	out := make([]LinkView, len(links))
	for i, l := range links {
		out[i] = LinkView{ID: l.ID, Title: l.Title, URL: l.URL}
	}
	return out, nil
}

func (b *HTTPBackend) RemoveLink(ctx context.Context, ref string, id int64) error {
	if err := b.Client.RemoveLink(ctx, ref, id); err != nil {
		return toServiceError(err)
	}
	return nil
}

func (b *HTTPBackend) GetBacklinks(ctx context.Context, ref string) ([]BacklinkView, error) {
	backlinks, err := b.Client.ListBacklinks(ctx, ref)
	if err != nil {
		return nil, toServiceError(err)
	}
	out := make([]BacklinkView, len(backlinks))
	for i, bl := range backlinks {
		v := BacklinkView{Ref: bl.Ref}
		if bl.CommentID != nil {
			v.CommentID = *bl.CommentID
		}
		out[i] = v
	}
	return out, nil
}

func (b *HTTPBackend) GetAttachment(ctx context.Context, id int64) (AttachmentView, error) {
	a, err := b.Client.GetAttachment(ctx, id)
	if err != nil {
		return AttachmentView{}, toServiceError(err)
	}
	return attachmentViewFromAPI(a), nil
}

func (b *HTTPBackend) ListAttachments(ctx context.Context, ref string, commentID int64) ([]AttachmentView, error) {
	page, err := b.Client.ListAttachments(ctx, ref, commentID)
	if err != nil {
		return nil, toServiceError(err)
	}
	out := make([]AttachmentView, len(page.Attachments))
	for i, a := range page.Attachments {
		out[i] = attachmentViewFromAPI(a)
	}
	return out, nil
}

func (b *HTTPBackend) ListAttachmentVersions(ctx context.Context, id int64) ([]AttachmentVersionView, error) {
	page, err := b.Client.ListAttachmentVersions(ctx, id)
	if err != nil {
		return nil, toServiceError(err)
	}
	out := make([]AttachmentVersionView, len(page.Versions))
	for i, v := range page.Versions {
		out[i] = AttachmentVersionView{
			Version: v.Version, Kind: v.Kind, FileName: v.FileName, FileSize: v.FileSize,
			MediaType: v.MediaType, Checksum: v.Checksum, PathValue: v.PathValue,
			UploadedBy: v.UploadedBy, CreatedAt: v.CreatedAt,
		}
	}
	return out, nil
}

func attachmentViewFromAPI(a apiclient.Attachment) AttachmentView {
	return AttachmentView{
		ID: a.ID, OwnerRef: a.OwnerRef, CommentID: a.CommentID, Kind: a.Kind, Title: a.Title,
		CurrentVersion: a.CurrentVersion, FileName: a.FileName, FileSize: a.FileSize, MediaType: a.MediaType,
		Checksum: a.Checksum, PathValue: a.PathValue, CreatedAt: a.CreatedAt, Creator: a.Creator, DeletedAt: a.DeletedAt,
	}
}

func (b *HTTPBackend) Search(ctx context.Context, in SearchInput) (SearchOutput, error) {
	page, err := b.Client.Search(ctx, in.Query, apiclient.SearchOptions{
		Project: in.Project, Kinds: in.Kind, Status: in.Status,
	}, in.Limit, in.Cursor)
	if err != nil {
		return SearchOutput{}, toServiceError(err)
	}
	out := SearchOutput{Hits: make([]SearchHit, len(page.Hits)), NextCursor: page.NextCursor}
	for i, h := range page.Hits {
		out.Hits[i] = SearchHit{Kind: h.Kind, Ref: h.Ref, CommentID: h.CommentID, Title: h.Title, Snippet: h.Snippet}
	}
	return out, nil
}

func (b *HTTPBackend) ListNotifications(ctx context.Context, unreadOnly bool, limit int, cursor string) (NotificationsListOutput, error) {
	page, err := b.Client.ListNotifications(ctx, unreadOnly, limit, cursor)
	if err != nil {
		return NotificationsListOutput{}, toServiceError(err)
	}
	out := NotificationsListOutput{Notifications: make([]NotificationCompact, len(page.Notifications)), NextCursor: page.NextCursor}
	for i, n := range page.Notifications {
		out.Notifications[i] = NotificationCompact{
			ID: n.ID, Kind: n.Kind, Entity: n.Entity, EntityKind: n.EntityKind,
			CommentID: n.CommentID, TriggeredBy: n.TriggeredBy, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
		}
	}
	return out, nil
}

func (b *HTTPBackend) GetProjectBrief(ctx context.Context, key string) (ProjectBrief, error) {
	if key == "" {
		key = b.DefaultProject
	}
	if key == "" {
		return ProjectBrief{}, errMissingProjectKey()
	}
	brief, err := b.Client.GetProjectBrief(ctx, key)
	if err != nil {
		return ProjectBrief{}, toServiceError(err)
	}
	return fromAPIProjectBrief(brief), nil
}

func (b *HTTPBackend) ListActivity(ctx context.Context, projectKey, actor, entityKind, eventType string, limit int, cursor string) (ActivityListOutput, error) {
	if projectKey == "" {
		projectKey = b.DefaultProject
	}
	if projectKey == "" {
		return ActivityListOutput{}, errMissingProjectKey()
	}
	page, err := b.Client.ListActivity(ctx, projectKey, apiclient.ActivityListOptions{
		Actor: actor, EntityKind: entityKind, EventType: eventType,
	}, limit, cursor)
	if err != nil {
		return ActivityListOutput{}, toServiceError(err)
	}
	out := ActivityListOutput{Events: make([]ActivityEventView, len(page.Events)), NextCursor: page.NextCursor}
	for i, e := range page.Events {
		v := ActivityEventView{
			ID: e.ID, Entity: e.Entity, EntityKind: e.EntityKind,
			Actor: e.Actor, EventType: e.EventType, CreatedAt: e.CreatedAt,
		}
		if e.CommentID != nil {
			v.CommentID = *e.CommentID
		}
		if e.CommentExcerpt != nil {
			v.CommentExcerpt = *e.CommentExcerpt
		}
		out.Events[i] = v
	}
	return out, nil
}

func (b *HTTPBackend) MarkNotificationsRead(ctx context.Context, ids []int64, all bool) (int64, error) {
	n, err := b.Client.MarkNotificationsRead(ctx, ids, all)
	if err != nil {
		return 0, toServiceError(err)
	}
	return n, nil
}

func (b *HTTPBackend) SetSubscription(ctx context.Context, ref string, subscribed bool) error {
	var err error
	if subscribed {
		_, err = b.Client.Subscribe(ctx, ref)
	} else {
		_, err = b.Client.Unsubscribe(ctx, ref)
	}
	if err != nil {
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
		Feature: in.Feature, General: in.General,
	}, in.IdempotencyKey)
	if err != nil {
		return domain.Ticket{}, toServiceError(err)
	}
	ticket, err := toDomainTicket(t)
	if err != nil {
		return domain.Ticket{}, err
	}
	return ticket, nil
}
