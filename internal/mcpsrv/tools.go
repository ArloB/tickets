package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// actorRefSchemaOptions overrides jsonschema-go's default struct
// reflection for domain.ActorRef: left alone, reflection describes it
// as an object with kind/name properties, but ActorRef.MarshalJSON
// (internal/domain/actor.go) actually renders it as the plain
// "kind:name" wire string everywhere in this codebase, HTTP responses
// included. mcp.AddTool derives each tool's OutputSchema from its Out
// type argument via this same reflection (github.com/google/jsonschema-go),
// with no knowledge of a custom MarshalJSON — every Out type below that
// embeds an ActorRef (Ticket/Feature/Project's Creator, Ticket's
// Assignee) needs this override passed explicitly, or the MCP client's
// own output-schema validation rejects the tool's real output.
//
// Types: []string{"null", "string"}, not Type: "string" — every use of
// ActorRef here is behind a pointer (Creator, Assignee both nil until
// set), and jsonschema-go's forType (infer.go) returns a TypeSchemas
// override immediately, before the "wrap with null for a pointer" step
// that every other type gets. Type: "string" alone would reject an
// explicit JSON null the moment a nil *ActorRef ever gets remarshaled
// as "null" rather than omitted (the MCP SDK's own
// StructuredContent -> JSON -> validate round trip, not just this
// codebase's own httpapi encoder, which relies on omitempty instead).
var actorRefSchemaOptions = &jsonschema.ForOptions{
	TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[domain.ActorRef](): {Types: []string{"null", "string"}},
	},
}

// outputSchemaFor builds T's output schema with actorRefSchemaOptions
// applied, for the mcp.Tool literals below. Panics on error, same as
// mcp.AddTool itself does for a schema it can't derive — this only
// ever runs at RegisterTools' fixed, non-request-triggered call time,
// so a failure here is a programming mistake, not a runtime condition
// to recover from.
func outputSchemaFor[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](actorRefSchemaOptions)
	if err != nil {
		panic(fmt.Sprintf("mcpsrv: build output schema for %T: %v", *new(T), err))
	}
	return s
}

// RegisterTools is the single tool-registration function shared by the
// server's HTTP-mounted MCP endpoint and the `tickets mcp` stdio
// bridge (ADR 0006) — the spike at docs/spikes/mcp proved one
// registration function can feed both transports. Phase 0 shipped
// three read/create tools mirroring the vertical slice's three HTTP
// endpoints; Phase 3 Step 1 adds projects_list/tickets_list, the
// read-only vertical slice's list side (product spec §7.2).
func RegisterTools(s *mcp.Server, backend Backend) {
	mcp.AddTool(s, &mcp.Tool{
		Name:         "project_get",
		Description:  "Get a project by its key (e.g. ABC).",
		OutputSchema: outputSchemaFor[domain.Project](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectGetInput) (*mcp.CallToolResult, domain.Project, error) {
		proj, err := backend.GetProject(ctx, in.Key)
		if err != nil {
			return nil, domain.Project{}, toolError(err)
		}
		return nil, proj, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "projects_list",
		Description:  "List projects, compact rows only (no description). Paginated — pass the previous call's next_cursor to continue.",
		OutputSchema: outputSchemaFor[ProjectsListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectsListInput) (*mcp.CallToolResult, ProjectsListOutput, error) {
		out, err := backend.ListProjects(ctx, in.Limit, in.Cursor)
		if err != nil {
			return nil, ProjectsListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "project_create",
		Description:  "Create a project. It always creates a General feature alongside it (ADR 0001).",
		OutputSchema: outputSchemaFor[domain.Project](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in projectCreateInput) (*mcp.CallToolResult, domain.Project, error) {
		proj, err := backend.CreateProject(withCallerActor(ctx, req), CreateProjectInput(in))
		if err != nil {
			return nil, domain.Project{}, toolError(err)
		}
		return nil, proj, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "tickets_list",
		Description:  "List tickets in a project, compact rows only (no description). view is priority_queue (default) or issue_register (bug/security tickets ordered by severity). Paginated — pass the previous call's next_cursor to continue.",
		OutputSchema: outputSchemaFor[TicketsListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketsListInput) (*mcp.CallToolResult, TicketsListOutput, error) {
		out, err := backend.ListTickets(ctx, in.ProjectKey, in.View, in.Limit, in.Cursor)
		if err != nil {
			return nil, TicketsListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_get",
		Description:  "Get a ticket by its public reference (e.g. ABC-123).",
		OutputSchema: outputSchemaFor[domain.Ticket](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketGetInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.GetTicket(ctx, in.Ref)
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_update",
		Description:  "Update a ticket's status and/or fields (type, title, description, priority, severity). Only fields you set are changed; omitted fields are left as-is. expected_version must be the version from a prior ticket_get/ticket_create/tickets_list call — a stale value is rejected with version_conflict rather than silently overwriting a concurrent change.",
		OutputSchema: outputSchemaFor[TicketWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketUpdateInput) (*mcp.CallToolResult, TicketWriteResult, error) {
		out, err := backend.UpdateTicket(withCallerActor(ctx, req), UpdateTicketInput(in))
		if err != nil {
			return nil, TicketWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_comment",
		Description:  "Add a Markdown comment to a ticket. Mentioning another entity's reference (e.g. #ABC-124) creates a backlink but not a dependency — use ticket_link for an explicit relationship.",
		OutputSchema: outputSchemaFor[CommentWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketCommentInput) (*mcp.CallToolResult, CommentWriteResult, error) {
		out, err := backend.AddComment(withCallerActor(ctx, req), in.Ref, in.Body, in.IdempotencyKey)
		if err != nil {
			return nil, CommentWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_link",
		Description:  "Link a ticket to another entity. type is either \"associated_with\" (a loose reference for context, where a dependency relationship wouldn't make sense — product spec §5.7) or one of the 8 explicit relationship types: parent_of, child_of, blocks, blocked_by, related_to, duplicate_of, supersedes, superseded_by. Explicit relationships are ticket-to-ticket only; associated_with also accepts a feature reference as target.",
		OutputSchema: outputSchemaFor[LinkWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketLinkInput) (*mcp.CallToolResult, LinkWriteResult, error) {
		ctx = withCallerActor(ctx, req)
		var err error
		if in.Type == "associated_with" {
			err = backend.AddAssociation(ctx, in.Ref, in.Target)
		} else {
			err = backend.AddRelationship(ctx, in.Ref, in.Type, in.Target)
		}
		if err != nil {
			return nil, LinkWriteResult{}, toolError(err)
		}
		return nil, LinkWriteResult(in), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_relationships",
		Description:  "List a ticket's explicit relationships (parent_of, child_of, blocks, blocked_by, related_to, duplicate_of, supersedes, superseded_by) — both ends included, from this ticket's perspective. Does not include associated_with links; see ticket_associations for those.",
		OutputSchema: outputSchemaFor[RelationshipsOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketRelationshipsInput) (*mcp.CallToolResult, RelationshipsOutput, error) {
		out, err := backend.GetTicketRelationships(ctx, in.Ref)
		if err != nil {
			return nil, RelationshipsOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_associations",
		Description:  "List the other entities a ticket or feature is associated_with (product spec §5.7) — a loose reference for context, not a dependency. Does not include explicit relationships; see ticket_relationships for those.",
		OutputSchema: outputSchemaFor[AssociationsOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketAssociationsInput) (*mcp.CallToolResult, AssociationsOutput, error) {
		out, err := backend.GetAssociations(ctx, in.Ref)
		if err != nil {
			return nil, AssociationsOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "feature_get",
		Description:  "Get a feature by its public reference (e.g. ABC-F1).",
		OutputSchema: outputSchemaFor[domain.Feature](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in featureGetInput) (*mcp.CallToolResult, domain.Feature, error) {
		f, err := backend.GetFeature(ctx, in.Ref)
		if err != nil {
			return nil, domain.Feature{}, toolError(err)
		}
		return nil, f, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "features_list",
		Description:  "List features in a project, compact rows only (no description). Paginated — pass the previous call's next_cursor to continue.",
		OutputSchema: outputSchemaFor[FeaturesListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in featuresListInput) (*mcp.CallToolResult, FeaturesListOutput, error) {
		out, err := backend.ListFeatures(ctx, in.ProjectKey, in.Limit, in.Cursor)
		if err != nil {
			return nil, FeaturesListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "feature_create",
		Description:  "Create a feature in a project.",
		OutputSchema: outputSchemaFor[FeatureWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureCreateInput) (*mcp.CallToolResult, FeatureWriteResult, error) {
		out, err := backend.CreateFeature(withCallerActor(ctx, req), CreateFeatureInput(in))
		if err != nil {
			return nil, FeatureWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "feature_update",
		Description:  "Replace a feature's title, description, and priority — a full-representation update (send every field, even ones you're not changing), unlike ticket_update. expected_version must be the version from a prior feature_get/feature_create/feature_update call.",
		OutputSchema: outputSchemaFor[FeatureWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureUpdateInput) (*mcp.CallToolResult, FeatureWriteResult, error) {
		out, err := backend.UpdateFeature(withCallerActor(ctx, req), UpdateFeatureInput(in))
		if err != nil {
			return nil, FeatureWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "record_get",
		Description:  "Get a decision by its public reference (e.g. ABC-D1). Scoped to decisions in Phase 3 — plans and documents (product spec §5.9) join once Phase 5 builds them.",
		OutputSchema: outputSchemaFor[domain.Decision](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recordGetInput) (*mcp.CallToolResult, domain.Decision, error) {
		d, err := backend.GetDecision(ctx, in.Ref)
		if err != nil {
			return nil, domain.Decision{}, toolError(err)
		}
		return nil, d, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "record_create",
		Description:  "Create a decision in a project (product spec §5.8): context, decision, and rationale. Scoped to decisions in Phase 3 — see record_get.",
		OutputSchema: outputSchemaFor[DecisionWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in recordCreateInput) (*mcp.CallToolResult, DecisionWriteResult, error) {
		out, err := backend.CreateDecision(withCallerActor(ctx, req), CreateDecisionInput(in))
		if err != nil {
			return nil, DecisionWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "record_update",
		Description:  "Replace a decision's title/context/decision/rationale/status — a full-representation update (send every field, even ones you're not changing). expected_version must be the version from a prior record_get/record_create/record_update call. Scoped to decisions in Phase 3 — see record_get.",
		OutputSchema: outputSchemaFor[DecisionWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in recordUpdateInput) (*mcp.CallToolResult, DecisionWriteResult, error) {
		out, err := backend.UpdateDecision(withCallerActor(ctx, req), UpdateDecisionInput(in))
		if err != nil {
			return nil, DecisionWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "ticket_create",
		Description:  "Create a ticket in a project. It always lands in the project's General feature.",
		OutputSchema: outputSchemaFor[domain.Ticket](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketCreateInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.CreateTicket(withCallerActor(ctx, req), CreateTicketInput(in))
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})
}

// withCallerActor attaches the calling agent's Principal to ctx, based
// on the bearer token auth.RequireBearerToken (NewStreamableHTTPHandler)
// verified before this tool call ever reached here (ADR 0006/0004) —
// req.Extra.TokenInfo.UserID is the "kind:name" wire form
// tokenVerifier (auth.go) put there. InProcessBackend.mcpActor is the
// only reader of this; HTTPBackend never touches ctx's Principal at
// all, since the stdio bridge attributes nothing itself — it forwards
// an Authorization header, and the *server* on the other end resolves
// the actor when that HTTP request arrives (see internal/httpapi's
// requestActor). req.Extra or its TokenInfo can be nil for exactly
// that stdio path (no HTTP request, no bearer-token middleware exists
// there at all) — ctx is returned unchanged in that case, which is
// harmless precisely because nothing downstream reads it then.
func withCallerActor(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return ctx
	}
	actor, err := domain.ParseActorRef(req.Extra.TokenInfo.UserID)
	if err != nil {
		return ctx
	}
	return auth.WithPrincipal(ctx, auth.Principal{Actor: actor, Permission: auth.PermissionEditor, AuthMethod: "bearer"})
}

type projectGetInput struct {
	Key string `json:"key" jsonschema:"the project key, e.g. ABC"`
}

type projectsListInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"max rows to return (server default 20, max 100)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous call's next_cursor; never construct or parse this yourself"`
}

type projectCreateInput struct {
	Key            string `json:"key" jsonschema:"the project key, e.g. ABC: uppercase letters/digits, 2-10 characters, starting with a letter"`
	Title          string `json:"title" jsonschema:"the project title"`
	Description    string `json:"description,omitempty" jsonschema:"optional Markdown description"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional: a client-chosen key to make a retried call safe. Reusing the same key with the same content returns the original project instead of creating a duplicate; reusing it with different content is rejected as idempotency_key_reused."`
}

type ticketGetInput struct {
	Ref string `json:"ref" jsonschema:"the ticket's public reference, e.g. ABC-123"`
}

type ticketsListInput struct {
	ProjectKey string `json:"project_key,omitempty" jsonschema:"the project key, e.g. ABC; falls back to the connection's configured default project if omitted"`
	View       string `json:"view,omitempty" jsonschema:"priority_queue (default) or issue_register"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max rows to return (server default 20, max 100)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous call's next_cursor; never construct or parse this yourself, and never reuse it across a different view"`
}

type ticketUpdateInput struct {
	Ref             string  `json:"ref" jsonschema:"the ticket's public reference, e.g. ABC-123"`
	Status          *string `json:"status,omitempty" jsonschema:"new workflow status: backlog, ready, in_progress, blocked, review, done, or cancelled"`
	Type            *string `json:"type,omitempty" jsonschema:"task, bug, security, or chore"`
	Title           *string `json:"title,omitempty" jsonschema:"new title"`
	Description     *string `json:"description,omitempty" jsonschema:"new Markdown description"`
	Priority        *string `json:"priority,omitempty" jsonschema:"critical, high, medium, or low"`
	Severity        *string `json:"severity,omitempty" jsonschema:"critical, high, medium, or low (bug/security tickets only)"`
	ExpectedVersion int64   `json:"expected_version" jsonschema:"the ticket's current version, from a prior ticket_get/ticket_create/tickets_list call"`
}

type ticketCommentInput struct {
	Ref            string `json:"ref" jsonschema:"the ticket's public reference, e.g. ABC-123"`
	Body           string `json:"body" jsonschema:"the comment's Markdown body"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional: a client-chosen key to make a retried call safe. Reusing the same key with the same ref/body returns the original comment instead of creating a duplicate; reusing it with different content is rejected as idempotency_key_reused."`
}

type ticketLinkInput struct {
	Ref    string `json:"ref" jsonschema:"the ticket's public reference, e.g. ABC-123 (the relationship/association source)"`
	Type   string `json:"type" jsonschema:"associated_with, or an explicit relationship type: parent_of, child_of, blocks, blocked_by, related_to, duplicate_of, supersedes, superseded_by"`
	Target string `json:"target" jsonschema:"the other entity's public reference, e.g. ABC-124 or ABC-F2"`
}

type ticketRelationshipsInput struct {
	Ref string `json:"ref" jsonschema:"the ticket's public reference, e.g. ABC-123"`
}

type ticketAssociationsInput struct {
	Ref string `json:"ref" jsonschema:"the ticket or feature's public reference, e.g. ABC-123 or ABC-F1"`
}

type featureGetInput struct {
	Ref string `json:"ref" jsonschema:"the feature's public reference, e.g. ABC-F1"`
}

type featuresListInput struct {
	ProjectKey string `json:"project_key,omitempty" jsonschema:"the project key, e.g. ABC; falls back to the connection's configured default project if omitted"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max rows to return (server default 20, max 100)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous call's next_cursor; never construct or parse this yourself"`
}

type featureCreateInput struct {
	ProjectKey  string `json:"project_key" jsonschema:"the project key, e.g. ABC"`
	Title       string `json:"title" jsonschema:"the feature title"`
	Description string `json:"description,omitempty" jsonschema:"optional Markdown description"`
	Priority    string `json:"priority,omitempty" jsonschema:"optional priority: critical, high, medium, or low (default medium)"`
}

type featureUpdateInput struct {
	Ref             string `json:"ref" jsonschema:"the feature's public reference, e.g. ABC-F1"`
	Title           string `json:"title" jsonschema:"the feature's title — full-representation update, always required"`
	Description     string `json:"description" jsonschema:"the feature's Markdown description — full-representation update; resend the current value if unchanged"`
	Priority        string `json:"priority" jsonschema:"critical, high, medium, or low"`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"the feature's current version, from a prior feature_get/feature_create/feature_update call"`
}

type recordGetInput struct {
	Ref string `json:"ref" jsonschema:"the decision's public reference, e.g. ABC-D1"`
}

type recordCreateInput struct {
	ProjectKey     string `json:"project_key" jsonschema:"the project key, e.g. ABC"`
	Title          string `json:"title" jsonschema:"the decision's title"`
	Context        string `json:"context,omitempty" jsonschema:"Markdown: the situation prompting this decision"`
	Decision       string `json:"decision,omitempty" jsonschema:"Markdown: what was decided"`
	Rationale      string `json:"rationale,omitempty" jsonschema:"Markdown: why"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional: a client-chosen key to make a retried call safe. Reusing the same key with the same content returns the original decision instead of creating a duplicate; reusing it with different content is rejected as idempotency_key_reused."`
}

type recordUpdateInput struct {
	Ref             string `json:"ref" jsonschema:"the decision's public reference, e.g. ABC-D1"`
	Title           string `json:"title" jsonschema:"the decision's title — full-representation update, always required"`
	Context         string `json:"context" jsonschema:"Markdown: the situation prompting this decision — full-representation update; resend the current value if unchanged"`
	Decision        string `json:"decision" jsonschema:"Markdown: what was decided — full-representation update; resend the current value if unchanged"`
	Rationale       string `json:"rationale" jsonschema:"Markdown: why — full-representation update; resend the current value if unchanged"`
	Status          string `json:"status" jsonschema:"proposed, accepted, rejected, or superseded"`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"the decision's current version, from a prior record_get/record_create/record_update call"`
}

type ticketCreateInput struct {
	ProjectKey  string `json:"project_key" jsonschema:"the project key, e.g. ABC"`
	Type        string `json:"type" jsonschema:"ticket type: task, bug, security, or chore"`
	Title       string `json:"title" jsonschema:"the ticket title"`
	Description string `json:"description,omitempty" jsonschema:"optional Markdown description"`
	Priority    string `json:"priority,omitempty" jsonschema:"optional priority: critical, high, medium, or low (default medium)"`
	Severity    string `json:"severity,omitempty" jsonschema:"optional severity for bug/security tickets: critical, high, medium, or low"`
}

// toolError formats a *service.Error as "<code>: <message>" so the
// domain.ErrorCode vocabulary (ADR 0006) survives being flattened into
// the plain-text error content the MCP SDK packs into a tool-error
// result. Any other error is reported as internal_error without
// leaking internals, matching docs/contracts/errors.md's HTTP
// behavior.
func toolError(err error) error {
	var svcErr *service.Error
	if errors.As(err, &svcErr) {
		return fmt.Errorf("%s: %s", svcErr.Code, svcErr.Message)
	}
	return fmt.Errorf("%s: an unexpected error occurred", domain.ErrInternal)
}
