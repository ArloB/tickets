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
		Name:         "project_brief",
		Description:  "Get oriented in a project: in-progress/upcoming tickets, issue-register highlights, the feature list with ticket-progress counts, recent activity, and recent accepted decisions and plans, each capped at 20 compact rows. Call this FIRST when starting work in a project — it's the recommended orientation read before project_get/ticket_get/record_get narrow in on any one record.",
		OutputSchema: outputSchemaFor[ProjectBrief](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectGetInput) (*mcp.CallToolResult, ProjectBrief, error) {
		brief, err := backend.GetProjectBrief(ctx, in.Key)
		if err != nil {
			return nil, ProjectBrief{}, toolError(err)
		}
		return nil, brief, nil
	})

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
		Name:         "project_update",
		Description:  "Update a project's title/description and/or archive/unarchive status (ADR 0021). Only fields you set are changed; omitted fields are left as-is. Archiving is visibility only — the project drops out of default projects_list/search results, but its tickets, features, and knowledge records stay fully readable and writable. expected_version must be the version from a prior project_get/project_create/projects_list call.",
		OutputSchema: outputSchemaFor[domain.Project](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in projectUpdateInput) (*mcp.CallToolResult, domain.Project, error) {
		proj, err := backend.UpdateProject(withCallerActor(ctx, req), UpdateProjectInput(in))
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
		Description:  "Add a Markdown comment to a ticket, feature, decision, plan, document, or project — ref accepts any of their public references, or a bare project key (e.g. ABC) to comment on the project itself. Mentioning another entity's reference (e.g. #ABC-124) creates a backlink but not a dependency — use ticket_link for an explicit relationship.",
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
		Description:  "Get a decision, plan, or document by its public reference (e.g. ABC-D1, ABC-P1, ABC-DOC1) — product spec §5.8/§5.9.",
		OutputSchema: outputSchemaFor[RecordDetail](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recordGetInput) (*mcp.CallToolResult, RecordDetail, error) {
		kind, kerr := recordRefKind(in.Ref)
		if kerr != nil {
			return nil, RecordDetail{}, toolError(kerr)
		}
		switch kind {
		case domain.KindDecision:
			d, err := backend.GetDecision(ctx, in.Ref)
			if err != nil {
				return nil, RecordDetail{}, toolError(err)
			}
			return nil, toRecordDetailFromDecision(d), nil
		case domain.KindPlan, domain.KindDocument:
			c, err := backend.GetContentItem(ctx, in.Ref)
			if err != nil {
				return nil, RecordDetail{}, toolError(err)
			}
			return nil, toRecordDetailFromContentItem(c), nil
		default:
			return nil, RecordDetail{}, toolError(errRecordRefKind())
		}
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "record_create",
		Description:  "Create a decision, plan, or document in a project (product spec §5.8/§5.9). kind is \"decision\" (default), \"plan\", or \"document\". Decisions use context/decision/rationale/consequences; plans and documents use representation (\"markdown\" default, \"path\", or \"url\") to pick which of body/path/url applies — there is no file-upload representation over MCP, since a tool call has no multipart transport.",
		OutputSchema: outputSchemaFor[RecordWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in recordCreateInput) (*mcp.CallToolResult, RecordWriteResult, error) {
		ctx = withCallerActor(ctx, req)
		kind := in.Kind
		if kind == "" {
			kind = "decision"
		}
		switch kind {
		case "decision":
			out, err := backend.CreateDecision(ctx, CreateDecisionInput{
				ProjectKey: in.ProjectKey, Title: in.Title, Context: in.Context, Decision: in.Decision,
				Rationale: in.Rationale, Consequences: in.Consequences, IdempotencyKey: in.IdempotencyKey,
			})
			if err != nil {
				return nil, RecordWriteResult{}, toolError(err)
			}
			return nil, recordWriteResultFromDecision(out), nil
		case "plan", "document":
			out, err := backend.CreateContentItem(ctx, CreateContentItemInput{
				ProjectKey: in.ProjectKey, Kind: kind, Title: in.Title, Representation: in.Representation,
				Body: in.Body, Path: in.Path, URL: in.URL, IdempotencyKey: in.IdempotencyKey,
			})
			if err != nil {
				return nil, RecordWriteResult{}, toolError(err)
			}
			return nil, recordWriteResultFromContentItem(out, kind), nil
		default:
			return nil, RecordWriteResult{}, toolError(&service.Error{Code: domain.ErrValidationFailed, Field: "kind", Message: "kind must be \"decision\", \"plan\", or \"document\""})
		}
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "record_update",
		Description:  "Replace a decision/plan/document's fields — a full-representation update (send every field that applies to this record's kind, even ones you're not changing, or they'll be cleared). Decisions: title/context/decision/rationale/consequences/status/superseded_by — omitting any of context/decision/rationale/consequences/status on a decision update is rejected as an error, not treated as a clear. Plans/documents: title, plus whichever of body/path/url matches the item's own (immutable) representation — there is no representation field here, and a file representation can't be replaced over MCP (use the HTTP API or CLI). expected_version must be the version from a prior record_get/record_create/record_update call.",
		OutputSchema: outputSchemaFor[RecordWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in recordUpdateInput) (*mcp.CallToolResult, RecordWriteResult, error) {
		kind, kerr := recordRefKind(in.Ref)
		if kerr != nil {
			return nil, RecordWriteResult{}, toolError(kerr)
		}
		ctx = withCallerActor(ctx, req)
		switch kind {
		case domain.KindDecision:
			decContext, decDecision, decRationale, decConsequences, decStatus, ferr := requireDecisionUpdateFields(in)
			if ferr != nil {
				return nil, RecordWriteResult{}, toolError(ferr)
			}
			out, err := backend.UpdateDecision(ctx, UpdateDecisionInput{
				Ref: in.Ref, Title: in.Title, Context: decContext, Decision: decDecision, Rationale: decRationale,
				Consequences: decConsequences, Status: decStatus, SupersededBy: in.SupersededBy, ExpectedVersion: in.ExpectedVersion,
			})
			if err != nil {
				return nil, RecordWriteResult{}, toolError(err)
			}
			return nil, recordWriteResultFromDecision(out), nil
		case domain.KindPlan, domain.KindDocument:
			out, err := backend.UpdateContentItem(ctx, UpdateContentItemInput{
				Ref: in.Ref, Title: in.Title, Body: in.Body, Path: in.Path, URL: in.URL, ExpectedVersion: in.ExpectedVersion,
			})
			if err != nil {
				return nil, RecordWriteResult{}, toolError(err)
			}
			return nil, recordWriteResultFromContentItem(out, string(kind)), nil
		default:
			return nil, RecordWriteResult{}, toolError(errRecordRefKind())
		}
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

	mcp.AddTool(s, &mcp.Tool{
		Name:         "search",
		Description:  "Full-text search over tickets, features, decisions, plans, documents, comments, attachment names, and external link titles/URLs, ranked by relevance. project/kind/status narrow an otherwise cross-project search. Paginated — pass the previous call's next_cursor to continue.",
		OutputSchema: outputSchemaFor[SearchOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, SearchOutput, error) {
		out, err := backend.Search(ctx, SearchInput(in))
		if err != nil {
			return nil, SearchOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "notifications_list",
		Description:  "List the calling actor's own notifications (product spec §6.4), newest first. Paginated — pass the previous call's next_cursor to continue.",
		OutputSchema: outputSchemaFor[NotificationsListOutput](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in notificationsListInput) (*mcp.CallToolResult, NotificationsListOutput, error) {
		out, err := backend.ListNotifications(withCallerActor(ctx, req), in.Unread, in.Limit, in.Cursor)
		if err != nil {
			return nil, NotificationsListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:         "notifications_mark_read",
		Description:  "Mark the calling actor's own notifications read, by id or (all: true) every currently-unread one.",
		OutputSchema: outputSchemaFor[notificationsMarkReadOutput](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in notificationsMarkReadInput) (*mcp.CallToolResult, notificationsMarkReadOutput, error) {
		n, err := backend.MarkNotificationsRead(withCallerActor(ctx, req), in.IDs, in.All)
		if err != nil {
			return nil, notificationsMarkReadOutput{}, toolError(err)
		}
		return nil, notificationsMarkReadOutput{Marked: n}, nil
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

type projectUpdateInput struct {
	Key             string  `json:"key" jsonschema:"the project key, e.g. ABC"`
	Title           *string `json:"title,omitempty" jsonschema:"new title; omit to leave unchanged"`
	Description     *string `json:"description,omitempty" jsonschema:"new Markdown description; omit to leave unchanged"`
	Status          *string `json:"status,omitempty" jsonschema:"active or archived; omit to leave unchanged"`
	ExpectedVersion int64   `json:"expected_version" jsonschema:"the project's current version, from a prior project_get/project_create/projects_list call"`
}

type ticketGetInput struct {
	Ref string `json:"ref" jsonschema:"the ticket's public reference, e.g. ABC-123"`
}

type searchInput struct {
	Query   string   `json:"query" jsonschema:"the search text"`
	Project string   `json:"project,omitempty" jsonschema:"restrict to one project's key; omitted searches every project"`
	Kind    []string `json:"kind,omitempty" jsonschema:"restrict to these kinds: ticket, feature, decision, plan, document, comment, attachment, link; omitted searches every kind"`
	Status  string   `json:"status,omitempty" jsonschema:"restrict to one status value (workflow status for tickets/features, decision status for decisions); plans/documents/comments have no status, so this never matches them"`
	Limit   int      `json:"limit,omitempty" jsonschema:"max rows to return (server default 20, max 100)"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous call's next_cursor; never construct or parse this yourself"`
}

type notificationsListInput struct {
	Unread bool   `json:"unread,omitempty" jsonschema:"only return unread notifications"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max rows to return (server default 20, max 100)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous call's next_cursor; never construct or parse this yourself"`
}

type notificationsMarkReadInput struct {
	IDs []int64 `json:"ids,omitempty" jsonschema:"notification ids to mark read"`
	All bool    `json:"all,omitempty" jsonschema:"mark every currently-unread notification read, ignoring ids"`
}

type notificationsMarkReadOutput struct {
	Marked int64 `json:"marked"`
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
	Ref            string `json:"ref" jsonschema:"the target's public reference (e.g. ABC-123 for a ticket, ABC-F2 for a feature, ABC-D1 for a decision, ABC-P1 for a plan, ABC-DOC1 for a document) or a bare project key (e.g. ABC) to comment on the project itself"`
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
	Ref string `json:"ref" jsonschema:"the record's public reference, e.g. ABC-D1, ABC-P1, or ABC-DOC1"`
}

type recordCreateInput struct {
	ProjectKey     string `json:"project_key" jsonschema:"the project key, e.g. ABC"`
	Kind           string `json:"kind,omitempty" jsonschema:"\"decision\" (default), \"plan\", or \"document\""`
	Title          string `json:"title" jsonschema:"the record's title"`
	Context        string `json:"context,omitempty" jsonschema:"decision only. Markdown: the situation prompting this decision"`
	Decision       string `json:"decision,omitempty" jsonschema:"decision only. Markdown: what was decided"`
	Rationale      string `json:"rationale,omitempty" jsonschema:"decision only. Markdown: why"`
	Consequences   string `json:"consequences,omitempty" jsonschema:"decision only. Markdown: what this decision leads to"`
	Representation string `json:"representation,omitempty" jsonschema:"plan/document only. \"markdown\" (default), \"path\", or \"url\" — there is no file-upload path over MCP (no multipart transport); upload a file representation via the HTTP API or CLI instead"`
	Body           string `json:"body,omitempty" jsonschema:"plan/document only, representation=markdown. The Markdown body"`
	Path           string `json:"path,omitempty" jsonschema:"plan/document only, representation=path. A path string only — never resolved or read by the server (ADR 0007)"`
	URL            string `json:"url,omitempty" jsonschema:"plan/document only, representation=url. An external URL"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional: a client-chosen key to make a retried call safe. Reusing the same key with the same content returns the original record instead of creating a duplicate; reusing it with different content is rejected as idempotency_key_reused."`
}

// recordUpdateInput's decision-only fields are *string, not string:
// before record_update covered plans/documents too, these had no
// omitempty tag at all, so the JSON schema itself forced an MCP client
// to send every one of them on every decision update (the "full-
// representation, or it's cleared" contract enforced at the schema
// level). Sharing this one struct across three kinds means the schema
// can no longer require them unconditionally — a plan/document update
// legitimately omits them — so the presence check moves from the
// schema (server.go: rejected before this handler even runs) to this
// handler (requireDecisionUpdateFields below): a nil pointer on a
// decision update is a validation_failed error, not "omitted, so wipe
// the field", the same outcome the old schema-level requiredness gave,
// just enforced one layer later. A code-review pass caught the
// alternative (plain `,omitempty` strings) as a real regression: an
// MCP client omitting e.g. context on a decision update would have
// silently cleared it instead of erroring.
type recordUpdateInput struct {
	Ref             string  `json:"ref" jsonschema:"the record's public reference, e.g. ABC-D1, ABC-P1, or ABC-DOC1"`
	Title           string  `json:"title" jsonschema:"the record's title — full-representation update, always required"`
	Context         *string `json:"context,omitempty" jsonschema:"decision only, and required for a decision update (omitting it on a decision update is an error, not a clear). Markdown: the situation prompting this decision — full-representation update; resend the current value if unchanged"`
	Decision        *string `json:"decision,omitempty" jsonschema:"decision only, and required for a decision update. Markdown: what was decided — full-representation update; resend the current value if unchanged"`
	Rationale       *string `json:"rationale,omitempty" jsonschema:"decision only, and required for a decision update. Markdown: why — full-representation update; resend the current value if unchanged"`
	Consequences    *string `json:"consequences,omitempty" jsonschema:"decision only, and required for a decision update. Markdown: what this decision leads to — full-representation update; resend the current value if unchanged"`
	Status          *string `json:"status,omitempty" jsonschema:"decision only, and required for a decision update. proposed, accepted, rejected, or superseded"`
	SupersededBy    string  `json:"superseded_by,omitempty" jsonschema:"decision only. Reference of the decision that supersedes this one, e.g. ABC-D9 — full-representation update; resend the current value if unchanged, or omit/empty to clear it"`
	Body            string  `json:"body,omitempty" jsonschema:"plan/document only, when its representation is markdown. The Markdown body — full-representation update; resend the current value if unchanged"`
	Path            string  `json:"path,omitempty" jsonschema:"plan/document only, when its representation is path. The new path value — full-representation update; resend the current value if unchanged"`
	URL             string  `json:"url,omitempty" jsonschema:"plan/document only, when its representation is url. The new URL value — full-representation update; resend the current value if unchanged"`
	ExpectedVersion int64   `json:"expected_version" jsonschema:"the record's current version, from a prior record_get/record_create/record_update call"`
}

// requireDecisionUpdateFields checks that every decision-only field
// record_update needs is actually present (see recordUpdateInput's doc)
// before building a decision update request — a nil field is
// validation_failed, never silently treated as "".
func requireDecisionUpdateFields(in recordUpdateInput) (context, decisionText, rationale, consequences, status string, err error) {
	for _, f := range []struct {
		name string
		val  *string
	}{
		{"context", in.Context}, {"decision", in.Decision}, {"rationale", in.Rationale},
		{"consequences", in.Consequences}, {"status", in.Status},
	} {
		if f.val == nil {
			return "", "", "", "", "", &service.Error{
				Code: domain.ErrValidationFailed, Field: f.name,
				Message: f.name + " is required for a decision update (full-representation update — resend the current value if unchanged)",
			}
		}
	}
	return *in.Context, *in.Decision, *in.Rationale, *in.Consequences, *in.Status, nil
}

// recordRefKind parses ref and returns its kind, restricted to the
// three kinds record_* answers — the tool handlers' single dispatch
// point (docs/adr/0017-content-items.md: "kind-specific branching lives
// once, in the tool handler").
func recordRefKind(ref string) (domain.EntityKind, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return "", &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	switch parsed.Kind {
	case domain.KindDecision, domain.KindPlan, domain.KindDocument:
		return parsed.Kind, nil
	default:
		return "", errRecordRefKind()
	}
}

func errRecordRefKind() error {
	return &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a decision, plan, or document reference"}
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
