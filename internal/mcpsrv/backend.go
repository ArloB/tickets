package mcpsrv

import (
	"context"

	"github.com/ArloB/tickets/internal/domain"
)

// Backend is what RegisterTools calls — not *service.Service directly.
// This is the resolution to a real tension in ADR 0006: the HTTP-
// mounted MCP endpoint should share internal/service in-process, but
// the `tickets mcp` stdio bridge must talk to the configured Tickets
// HTTP API, never open SQLite directly (product spec §8.1). One tool
// registration function, two Backend implementations:
//
//   - InProcessBackend (this package) wraps *service.Service directly,
//     used by the server's own mcp.NewStreamableHTTPHandler mount.
//   - HTTPBackend (this package) wraps an HTTP client against the
//     Tickets API, used by the `tickets mcp` stdio bridge.
//
// Both ultimately execute the same internal/service business logic —
// HTTPBackend just reaches it through internal/httpapi instead of a
// direct call — so §8.1's "neither interface duplicates business
// logic" holds either way tools are registered.
type Backend interface {
	GetProject(ctx context.Context, key string) (domain.Project, error)
	ListProjects(ctx context.Context, limit int, cursor string) (ProjectsListOutput, error)
	CreateProject(ctx context.Context, in CreateProjectInput) (domain.Project, error)
	CreateTicket(ctx context.Context, req CreateTicketInput) (domain.Ticket, error)
	GetTicket(ctx context.Context, ref string) (domain.Ticket, error)
	ListTickets(ctx context.Context, projectKey, view string, limit int, cursor string) (TicketsListOutput, error)
	UpdateTicket(ctx context.Context, in UpdateTicketInput) (TicketWriteResult, error)
	AddComment(ctx context.Context, ticketRef, body, idempotencyKey string) (CommentWriteResult, error)
	AddRelationship(ctx context.Context, sourceRef, relType, targetRef string) error
	AddAssociation(ctx context.Context, sourceRef, targetRef string) error
	GetTicketRelationships(ctx context.Context, ref string) (RelationshipsOutput, error)
	GetAssociations(ctx context.Context, ref string) (AssociationsOutput, error)

	GetFeature(ctx context.Context, ref string) (domain.Feature, error)
	ListFeatures(ctx context.Context, projectKey string, limit int, cursor string) (FeaturesListOutput, error)
	CreateFeature(ctx context.Context, in CreateFeatureInput) (FeatureWriteResult, error)
	UpdateFeature(ctx context.Context, in UpdateFeatureInput) (FeatureWriteResult, error)

	GetDecision(ctx context.Context, ref string) (domain.Decision, error)
	CreateDecision(ctx context.Context, in CreateDecisionInput) (DecisionWriteResult, error)
	UpdateDecision(ctx context.Context, in UpdateDecisionInput) (DecisionWriteResult, error)
}

// CreateProjectInput mirrors CreateTicketInput's shape/reasoning.
// IdempotencyKey is optional, same convention as CreateDecisionInput.
type CreateProjectInput struct {
	Key            string
	Title          string
	Description    string
	IdempotencyKey string
}

// CreateDecisionInput mirrors CreateFeatureInput's shape/reasoning.
// IdempotencyKey is optional — see AddComment's doc comment on
// InProcessBackend for why the caller must supply and reuse it, rather
// than the backend generating one per call.
type CreateDecisionInput struct {
	ProjectKey     string
	Title          string
	Context        string
	Decision       string
	Rationale      string
	Consequences   string
	IdempotencyKey string
}

// UpdateDecisionInput mirrors UpdateFeatureInput: every field is
// required (full-representation update, no partial merge — PATCH
// /decisions/{ref} has the same contract as PATCH /features/{ref}).
// SupersededBy "" clears an existing supersession link, the same
// full-representation contract every other field here has.
type UpdateDecisionInput struct {
	Ref             string
	Title           string
	Context         string
	Decision        string
	Rationale       string
	Consequences    string
	Status          string
	SupersededBy    string
	ExpectedVersion int64
}

// CreateFeatureInput mirrors CreateTicketInput's shape/reasoning.
type CreateFeatureInput struct {
	ProjectKey  string
	Title       string
	Description string
	Priority    string
}

// UpdateFeatureInput is feature_update's input — unlike
// UpdateTicketInput, every field is required (no nil-means-unchanged
// pointers): PATCH /features/{ref} is a full-representation update
// with no partial-field route the way tickets have PATCH (status) vs.
// PUT (fields), so there's nothing to merge and no unset-field case to
// represent.
type UpdateFeatureInput struct {
	Ref             string
	Title           string
	Description     string
	Priority        string
	ExpectedVersion int64
}

// CreateTicketInput mirrors service.CreateTicketRequest but with string
// fields, since it's the shape both Backend implementations (typed
// service call vs. JSON over HTTP) can produce uniformly.
type CreateTicketInput struct {
	ProjectKey  string
	Type        string
	Title       string
	Description string
	Priority    string
	Severity    string
}
