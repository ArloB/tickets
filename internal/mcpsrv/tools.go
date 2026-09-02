package mcpsrv

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strconv"

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

var readOnlyTools = map[string]bool{
	"project_brief": true, "project_get": true, "projects_list": true,
	"tickets_list": true, "ticket_get": true, "comments_list": true,
	"comment_get": true, "comment_history": true, "relationships_list": true,
	"associations_list": true, "external_links_list": true, "backlinks_list": true,
	"attachment_get": true, "attachments_list": true, "attachment_versions": true,
	"feature_get": true, "features_list": true, "record_get": true,
	"records_list": true, "record_versions": true, "record_diff": true,
	"search": true, "project_activity": true, "notifications_list": true,
}

var additiveTools = map[string]bool{
	"project_create": true, "ticket_create": true, "feature_create": true,
	"comment_create": true, "relationship_add": true, "association_add": true,
	"external_link_add": true,
}

func boolPointer(value bool) *bool {
	return &value
}

func paginate[T any](items []T, limit int, cursor string) ([]T, string, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, "", &service.Error{Code: domain.ErrValidationFailed, Field: "limit", Message: "limit must be between 1 and 100"}
	}
	offset := 0
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", &service.Error{Code: domain.ErrValidationFailed, Field: "cursor", Message: "cursor is invalid"}
		}
		offset, err = strconv.Atoi(string(raw))
		if err != nil || offset < 0 || offset > len(items) {
			return nil, "", &service.Error{Code: domain.ErrValidationFailed, Field: "cursor", Message: "cursor is invalid"}
		}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	return items[offset:end], next, nil
}

func addTool[In, Out any](s *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	readOnly := readOnlyTools[tool.Name]
	tool.Annotations = &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
		DestructiveHint: boolPointer(!readOnly && !additiveTools[tool.Name]),
		IdempotentHint:  readOnly,
		OpenWorldHint:   boolPointer(false),
	}
	mcp.AddTool(s, tool, handler)
}

// RegisterTools is the single tool-registration function shared by the
// server's HTTP-mounted MCP endpoint and the `tickets mcp` stdio
// bridge (ADR 0006) — the spike at docs/spikes/mcp proved one
// registration function can feed both transports. Phase 0 shipped
// three read/create tools mirroring the vertical slice's three HTTP
// endpoints; Phase 3 Step 1 adds projects_list/tickets_list, the
// read-only vertical slice's list side (product spec §7.2).
func RegisterTools(s *mcp.Server, backend Backend) {
	addTool(s, &mcp.Tool{
		Name:         "project_brief",
		Description:  "Summarize a project for orientation: project details plus up to 20 compact rows each for active or upcoming tickets, issue-register tickets, features with progress counts, recent activity, accepted decisions, and plans.",
		OutputSchema: outputSchemaFor[ProjectBrief](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectGetInput) (*mcp.CallToolResult, ProjectBrief, error) {
		brief, err := backend.GetProjectBrief(ctx, in.Key)
		if err != nil {
			return nil, ProjectBrief{}, toolError(err)
		}
		return nil, brief, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "project_get",
		Description:  "Get a project's full details by key.",
		OutputSchema: outputSchemaFor[domain.Project](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectGetInput) (*mcp.CallToolResult, domain.Project, error) {
		proj, err := backend.GetProject(ctx, in.Key)
		if err != nil {
			return nil, domain.Project{}, toolError(err)
		}
		return nil, proj, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "projects_list",
		Description:  "List projects as compact rows, oldest first. Archived projects are excluded unless include_archived is true. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[ProjectsListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectsListInput) (*mcp.CallToolResult, ProjectsListOutput, error) {
		out, err := backend.ListProjects(ctx, in.Limit, in.Cursor, in.IncludeArchived)
		if err != nil {
			return nil, ProjectsListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "project_create",
		Description:  "Create a project and its General feature.",
		OutputSchema: outputSchemaFor[domain.Project](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in projectCreateInput) (*mcp.CallToolResult, domain.Project, error) {
		proj, err := backend.CreateProject(withCallerActor(ctx, req), CreateProjectInput(in))
		if err != nil {
			return nil, domain.Project{}, toolError(err)
		}
		return nil, proj, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "project_update",
		Description:  "Partially update a project's title, description, or active/archived status. Archived projects are excluded from default project lists and search but remain readable and writable by key. Send status separately from title or description.",
		OutputSchema: outputSchemaFor[domain.Project](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in projectUpdateInput) (*mcp.CallToolResult, domain.Project, error) {
		fields := in.Title != nil || in.Description != nil
		if (in.Status != nil) == fields {
			return nil, domain.Project{}, toolError(&service.Error{Code: domain.ErrValidationFailed, Message: "set exactly one operation group: status or title/description"})
		}
		proj, err := backend.UpdateProject(withCallerActor(ctx, req), UpdateProjectInput(in))
		if err != nil {
			return nil, domain.Project{}, toolError(err)
		}
		return nil, proj, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "tickets_list",
		Description:  "List a project's tickets as compact rows. priority_queue is the default; issue_register contains bug and security tickets ordered by severity. Filters are AND-composed. Reuse the same view and filters with next_cursor.",
		OutputSchema: outputSchemaFor[TicketsListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketsListInput) (*mcp.CallToolResult, TicketsListOutput, error) {
		out, err := backend.ListTickets(ctx, in.ProjectKey, in.View, TicketListFilters{
			Status: in.Status, Type: in.Type, Severity: in.Severity, Priority: in.Priority,
			FeatureRef: in.FeatureRef, Assignee: in.Assignee, Creator: in.Creator, UpdatedSince: in.UpdatedSince,
		}, in.Limit, in.Cursor)
		if err != nil {
			return nil, TicketsListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "ticket_get",
		Description:  "Get a ticket's full details by reference. Set include_deleted:true to retrieve a soft-deleted ticket and discover the version required by ticket_restore.",
		OutputSchema: outputSchemaFor[domain.Ticket](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketGetInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.GetTicket(ctx, in.Ref, in.IncludeDeleted)
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "ticket_update",
		Description:  "Partially update a ticket's status, type, title, description, priority, severity, assignee, or feature. Omitted fields are unchanged. Send only one operation group: status; content fields; assignee; or feature. Send an empty severity or assignee to clear it. Changing priority moves the ticket to the end of its new priority group.",
		OutputSchema: outputSchemaFor[TicketWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketUpdateInput) (*mcp.CallToolResult, TicketWriteResult, error) {
		fields := in.Type != nil || in.Title != nil || in.Description != nil || in.Priority != nil || in.Severity != nil
		groups := 0
		for _, set := range []bool{in.Status != nil, fields, in.Assignee != nil, in.Feature != nil} {
			if set {
				groups++
			}
		}
		if groups != 1 {
			return nil, TicketWriteResult{}, toolError(&service.Error{Code: domain.ErrValidationFailed, Message: "set exactly one operation group: status; content fields; assignee; or feature"})
		}
		ctx = withCallerActor(ctx, req)
		if in.Assignee != nil {
			var assignee *string
			if *in.Assignee != "" {
				assignee = in.Assignee
			}
			out, err := backend.AssignTicket(ctx, in.Ref, assignee, in.ExpectedVersion)
			if err != nil {
				return nil, TicketWriteResult{}, toolError(err)
			}
			return nil, out, nil
		}
		if in.Feature != nil {
			if *in.Feature == "" {
				return nil, TicketWriteResult{}, toolError(&service.Error{Code: domain.ErrValidationFailed, Field: "feature", Message: "feature must be a feature reference"})
			}
			ticket, err := backend.MoveTicketFeature(ctx, in.Ref, *in.Feature, in.ExpectedVersion)
			if err != nil {
				return nil, TicketWriteResult{}, toolError(err)
			}
			return nil, toTicketWriteResult(ticket), nil
		}
		out, err := backend.UpdateTicket(ctx, UpdateTicketInput{
			Ref: in.Ref, Status: in.Status, Type: in.Type, Title: in.Title,
			Description: in.Description, Priority: in.Priority, Severity: in.Severity,
			ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			return nil, TicketWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "ticket_reorder",
		Description:  "Reposition a ticket within its current priority group. Omit after_ref to move it to the front.",
		OutputSchema: outputSchemaFor[TicketWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketReorderInput) (*mcp.CallToolResult, TicketWriteResult, error) {
		var afterRef *string
		if in.AfterRef != "" {
			afterRef = &in.AfterRef
		}
		out, err := backend.ReorderTicket(withCallerActor(ctx, req), in.Ref, afterRef, in.ExpectedVersion)
		if err != nil {
			return nil, TicketWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "ticket_delete",
		Description:  "Soft-delete a ticket. Restore it with ticket_restore using the version returned here.",
		OutputSchema: outputSchemaFor[DeleteWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketDeleteInput) (*mcp.CallToolResult, DeleteWriteResult, error) {
		out, err := backend.DeleteTicket(withCallerActor(ctx, req), in.Ref, in.ExpectedVersion)
		if err != nil {
			return nil, DeleteWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "ticket_restore",
		Description:  "Restore a soft-deleted ticket. Its feature must already be active.",
		OutputSchema: outputSchemaFor[TicketWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketRestoreInput) (*mcp.CallToolResult, TicketWriteResult, error) {
		out, err := backend.RestoreTicket(withCallerActor(ctx, req), in.Ref, in.ExpectedVersion)
		if err != nil {
			return nil, TicketWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "comment_create",
		Description:  "Add a Markdown comment to a project, ticket, feature, decision, plan, or document. Markdown references create backlinks, not typed relationships.",
		OutputSchema: outputSchemaFor[CommentWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketCommentInput) (*mcp.CallToolResult, CommentWriteResult, error) {
		out, err := backend.AddComment(withCallerActor(ctx, req), in.Ref, in.Body, in.IdempotencyKey)
		if err != nil {
			return nil, CommentWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "comments_list",
		Description:  "List comments on an entity, oldest first, as compact rows without bodies. Tombstones are included. Use comment_get for a body or deletion state, and next_cursor to continue.",
		OutputSchema: outputSchemaFor[CommentsListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in commentsListInput) (*mcp.CallToolResult, CommentsListOutput, error) {
		out, err := backend.ListComments(ctx, in.Ref)
		if err != nil {
			return nil, CommentsListOutput{}, toolError(err)
		}
		out.Comments, out.NextCursor, err = paginate(out.Comments, in.Limit, in.Cursor)
		if err != nil {
			return nil, CommentsListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "comment_get",
		Description:  "Get a comment's full details by ID, including its body and deletion state.",
		OutputSchema: outputSchemaFor[domain.Comment](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in commentIDInput) (*mcp.CallToolResult, domain.Comment, error) {
		out, err := backend.GetComment(ctx, in.ID)
		if err != nil {
			return nil, domain.Comment{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "comment_update",
		Description:  "Replace a comment's Markdown body. The previous body is archived in comment_history.",
		OutputSchema: outputSchemaFor[CommentWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in commentUpdateInput) (*mcp.CallToolResult, CommentWriteResult, error) {
		out, err := backend.UpdateComment(withCallerActor(ctx, req), in.ID, in.ExpectedVersion, in.Body)
		if err != nil {
			return nil, CommentWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "comment_delete",
		Description:  "Delete a comment, leaving a visible tombstone. Comments cannot be restored.",
		OutputSchema: outputSchemaFor[CommentDeleteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in commentDeleteInput) (*mcp.CallToolResult, CommentDeleteResult, error) {
		out, err := backend.DeleteComment(withCallerActor(ctx, req), in.ID, in.ExpectedVersion)
		if err != nil {
			return nil, CommentDeleteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "comment_history",
		Description:  "List a comment's archived prior bodies, oldest first. The current body is available from comment_get. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[CommentHistoryOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in commentHistoryInput) (*mcp.CallToolResult, CommentHistoryOutput, error) {
		out, err := backend.GetCommentHistory(ctx, in.ID)
		if err != nil {
			return nil, CommentHistoryOutput{}, toolError(err)
		}
		out.Versions, out.NextCursor, err = paginate(out.Versions, in.Limit, in.Cursor)
		if err != nil {
			return nil, CommentHistoryOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "relationship_add",
		Description:  "Add a directional or symmetric ticket-to-ticket relationship.",
		OutputSchema: outputSchemaFor[LinkWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketLinkInput) (*mcp.CallToolResult, LinkWriteResult, error) {
		err := backend.AddRelationship(withCallerActor(ctx, req), in.Ref, in.Type, in.Target)
		if err != nil {
			return nil, LinkWriteResult{}, toolError(err)
		}
		return nil, LinkWriteResult(in), nil
	})

	addTool(s, &mcp.Tool{
		Name:         "relationship_remove",
		Description:  "Remove the exact ticket relationship identified by ref, type, and target.",
		OutputSchema: outputSchemaFor[LinkWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketLinkInput) (*mcp.CallToolResult, LinkWriteResult, error) {
		err := backend.RemoveRelationship(withCallerActor(ctx, req), in.Ref, in.Type, in.Target)
		if err != nil {
			return nil, LinkWriteResult{}, toolError(err)
		}
		return nil, LinkWriteResult(in), nil
	})

	addTool(s, &mcp.Tool{
		Name:         "association_add",
		Description:  "Add a symmetric associated_with connection between two supported entities.",
		OutputSchema: outputSchemaFor[LinkWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in associationInput) (*mcp.CallToolResult, LinkWriteResult, error) {
		if err := backend.AddAssociation(withCallerActor(ctx, req), in.Ref, in.Target); err != nil {
			return nil, LinkWriteResult{}, toolError(err)
		}
		return nil, LinkWriteResult{Ref: in.Ref, Type: "associated_with", Target: in.Target}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "association_remove",
		Description:  "Remove the associated_with connection between two entities.",
		OutputSchema: outputSchemaFor[LinkWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in associationInput) (*mcp.CallToolResult, LinkWriteResult, error) {
		if err := backend.RemoveAssociation(withCallerActor(ctx, req), in.Ref, in.Target); err != nil {
			return nil, LinkWriteResult{}, toolError(err)
		}
		return nil, LinkWriteResult{Ref: in.Ref, Type: "associated_with", Target: in.Target}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "relationships_list",
		Description:  "List a ticket's relationships from its perspective. duplicate_of is visible only from the duplicate ticket because it has no inverse. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[RelationshipsOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketRelationshipsInput) (*mcp.CallToolResult, RelationshipsOutput, error) {
		out, err := backend.GetTicketRelationships(ctx, in.Ref)
		if err != nil {
			return nil, RelationshipsOutput{}, toolError(err)
		}
		out.Relationships, out.NextCursor, err = paginate(out.Relationships, in.Limit, in.Cursor)
		if err != nil {
			return nil, RelationshipsOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "associations_list",
		Description:  "List entities connected through the symmetric associated_with association. Ticket relationships are excluded. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[AssociationsOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ticketAssociationsInput) (*mcp.CallToolResult, AssociationsOutput, error) {
		out, err := backend.GetAssociations(ctx, in.Ref)
		if err != nil {
			return nil, AssociationsOutput{}, toolError(err)
		}
		out.Associated, out.NextCursor, err = paginate(out.Associated, in.Limit, in.Cursor)
		if err != nil {
			return nil, AssociationsOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "external_link_add",
		Description:  "Add a named HTTP, HTTPS, or mailto bookmark to an entity.",
		OutputSchema: outputSchemaFor[LinkView](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in linkAddInput) (*mcp.CallToolResult, LinkView, error) {
		out, err := backend.AddLink(withCallerActor(ctx, req), in.Ref, in.Title, in.URL)
		if err != nil {
			return nil, LinkView{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "external_links_list",
		Description:  "List external bookmarks attached to an entity. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[linksListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in linksListInput) (*mcp.CallToolResult, linksListOutput, error) {
		links, err := backend.ListLinks(ctx, in.Ref)
		if err != nil {
			return nil, linksListOutput{}, toolError(err)
		}
		links, next, err := paginate(links, in.Limit, in.Cursor)
		if err != nil {
			return nil, linksListOutput{}, toolError(err)
		}
		return nil, linksListOutput{Links: links, NextCursor: next}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "external_link_remove",
		Description:  "Remove an external bookmark by owner reference and link ID.",
		OutputSchema: outputSchemaFor[LinkView](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in linkRemoveInput) (*mcp.CallToolResult, LinkView, error) {
		if err := backend.RemoveLink(withCallerActor(ctx, req), in.Ref, in.ID); err != nil {
			return nil, LinkView{}, toolError(err)
		}
		return nil, LinkView{ID: in.ID}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "backlinks_list",
		Description:  "List live entities and comments whose Markdown mentions this entity. Backlinks are derived automatically and are not typed relationships. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[backlinksListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in backlinksListInput) (*mcp.CallToolResult, backlinksListOutput, error) {
		out, err := backend.GetBacklinks(ctx, in.Ref)
		if err != nil {
			return nil, backlinksListOutput{}, toolError(err)
		}
		out, next, err := paginate(out, in.Limit, in.Cursor)
		if err != nil {
			return nil, backlinksListOutput{}, toolError(err)
		}
		return nil, backlinksListOutput{Backlinks: out, NextCursor: next}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "attachment_get",
		Description:  "Get attachment metadata by ID. Binary content is not returned.",
		OutputSchema: outputSchemaFor[AttachmentView](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in attachmentGetInput) (*mcp.CallToolResult, AttachmentView, error) {
		out, err := backend.GetAttachment(ctx, in.ID)
		if err != nil {
			return nil, AttachmentView{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "attachments_list",
		Description:  "List attachment metadata for an entity or comment. Set exactly one of ref or comment_id. Binary content is not returned. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[AttachmentsListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in attachmentsListInput) (*mcp.CallToolResult, AttachmentsListOutput, error) {
		if (in.Ref == "") == (in.CommentID == 0) {
			return nil, AttachmentsListOutput{}, toolError(&service.Error{Code: domain.ErrValidationFailed, Message: "set exactly one of ref or comment_id"})
		}
		items, err := backend.ListAttachments(ctx, in.Ref, in.CommentID)
		if err != nil {
			return nil, AttachmentsListOutput{}, toolError(err)
		}
		items, next, err := paginate(items, in.Limit, in.Cursor)
		if err != nil {
			return nil, AttachmentsListOutput{}, toolError(err)
		}
		return nil, AttachmentsListOutput{Attachments: items, NextCursor: next}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "attachment_versions",
		Description:  "List archived attachment metadata versions, oldest first. Binary content is not returned. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[AttachmentVersionsOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in attachmentVersionsInput) (*mcp.CallToolResult, AttachmentVersionsOutput, error) {
		items, err := backend.ListAttachmentVersions(ctx, in.ID)
		if err != nil {
			return nil, AttachmentVersionsOutput{}, toolError(err)
		}
		items, next, err := paginate(items, in.Limit, in.Cursor)
		if err != nil {
			return nil, AttachmentVersionsOutput{}, toolError(err)
		}
		return nil, AttachmentVersionsOutput{Versions: items, NextCursor: next}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "feature_get",
		Description:  "Get a feature's full details by reference. Set include_deleted:true to retrieve a soft-deleted feature and discover the version required by feature_restore.",
		OutputSchema: outputSchemaFor[domain.Feature](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in featureGetInput) (*mcp.CallToolResult, domain.Feature, error) {
		f, err := backend.GetFeature(ctx, in.Ref, in.IncludeDeleted)
		if err != nil {
			return nil, domain.Feature{}, toolError(err)
		}
		return nil, f, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "features_list",
		Description:  "List a project's non-deleted features as compact rows in priority order. Filters are AND-composed. Reuse the same filters with next_cursor.",
		OutputSchema: outputSchemaFor[FeaturesListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in featuresListInput) (*mcp.CallToolResult, FeaturesListOutput, error) {
		out, err := backend.ListFeatures(ctx, in.ProjectKey, FeatureListFilters{
			Status: in.Status, Priority: in.Priority, Creator: in.Creator, UpdatedSince: in.UpdatedSince,
		}, in.Limit, in.Cursor)
		if err != nil {
			return nil, FeaturesListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "feature_create",
		Description:  "Create a backlog feature. Priority defaults to medium.",
		OutputSchema: outputSchemaFor[FeatureWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureCreateInput) (*mcp.CallToolResult, FeatureWriteResult, error) {
		out, err := backend.CreateFeature(withCallerActor(ctx, req), CreateFeatureInput(in))
		if err != nil {
			return nil, FeatureWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "feature_update",
		Description:  "Partially update a feature's title, description, priority, or workflow status. Omitted fields are unchanged. Send status separately from content fields. Changing priority moves the feature to the end of its new priority group.",
		OutputSchema: outputSchemaFor[FeatureWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureUpdateInput) (*mcp.CallToolResult, FeatureWriteResult, error) {
		fields := in.Title != nil || in.Description != nil || in.Priority != nil
		if (in.Status != nil) == fields {
			return nil, FeatureWriteResult{}, toolError(&service.Error{Code: domain.ErrValidationFailed, Message: "set exactly one operation group: status or content fields"})
		}
		ctx = withCallerActor(ctx, req)
		if in.Status != nil {
			out, err := backend.SetFeatureStatus(ctx, in.Ref, *in.Status, in.ExpectedVersion)
			if err != nil {
				return nil, FeatureWriteResult{}, toolError(err)
			}
			return nil, out, nil
		}
		current, err := backend.GetFeature(ctx, in.Ref)
		if err != nil {
			return nil, FeatureWriteResult{}, toolError(err)
		}
		title, description, priority := current.Title, current.Description, string(current.Priority)
		if in.Title != nil {
			title = *in.Title
		}
		if in.Description != nil {
			description = *in.Description
		}
		if in.Priority != nil {
			priority = *in.Priority
		}
		out, err := backend.UpdateFeature(ctx, UpdateFeatureInput{Ref: in.Ref, Title: title, Description: description, Priority: priority, ExpectedVersion: in.ExpectedVersion})
		if err != nil {
			return nil, FeatureWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "feature_reorder",
		Description:  "Reposition a feature within its current priority group. Omit after_ref to move it to the front.",
		OutputSchema: outputSchemaFor[FeatureWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureReorderInput) (*mcp.CallToolResult, FeatureWriteResult, error) {
		var afterRef *string
		if in.AfterRef != "" {
			afterRef = &in.AfterRef
		}
		out, err := backend.ReorderFeature(withCallerActor(ctx, req), in.Ref, afterRef, in.ExpectedVersion)
		if err != nil {
			return nil, FeatureWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "feature_delete",
		Description:  "Soft-delete a feature. General cannot be deleted. A feature containing tickets requires cascade:true, which also deletes those tickets.",
		OutputSchema: outputSchemaFor[DeleteWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureDeleteInput) (*mcp.CallToolResult, DeleteWriteResult, error) {
		out, err := backend.DeleteFeature(withCallerActor(ctx, req), in.Ref, in.Cascade, in.ExpectedVersion)
		if err != nil {
			return nil, DeleteWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "feature_restore",
		Description:  "Restore a soft-deleted feature. Tickets deleted by a cascade are not restored automatically.",
		OutputSchema: outputSchemaFor[FeatureWriteResult](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureRestoreInput) (*mcp.CallToolResult, FeatureWriteResult, error) {
		out, err := backend.RestoreFeature(withCallerActor(ctx, req), in.Ref, in.ExpectedVersion)
		if err != nil {
			return nil, FeatureWriteResult{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "record_get",
		Description:  "Get a decision, plan, or document by reference.",
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

	addTool(s, &mcp.Tool{
		Name:         "record_create",
		Description:  "Create a decision, plan, or document. kind defaults to decision; new decisions start as proposed. Plans and documents use an immutable markdown, path, or URL representation.",
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

	addTool(s, &mcp.Tool{
		Name:         "record_update",
		Description:  "Replace every applicable field of a decision, plan, or document. Read it first and resend unchanged values. A plan/document's status is the one exception — omit it to leave archive state unchanged. File-backed records cannot be updated through MCP.",
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
				Ref: in.Ref, Title: in.Title, Body: in.Body, Path: in.Path, URL: in.URL, Status: in.Status, ExpectedVersion: in.ExpectedVersion,
			})
			if err != nil {
				return nil, RecordWriteResult{}, toolError(err)
			}
			return nil, recordWriteResultFromContentItem(out, string(kind)), nil
		default:
			return nil, RecordWriteResult{}, toolError(errRecordRefKind())
		}
	})

	addTool(s, &mcp.Tool{
		Name:         "records_list",
		Description:  "List one kind of project record as compact rows. kind defaults to decision. Use next_cursor to continue. include_archived (plan/document only) also returns archived items; default false.",
		OutputSchema: outputSchemaFor[RecordsListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recordsListInput) (*mcp.CallToolResult, RecordsListOutput, error) {
		kind := in.Kind
		if kind == "" {
			kind = "decision"
		}
		switch kind {
		case "decision":
			out, err := backend.ListDecisions(ctx, in.ProjectKey, in.Limit, in.Cursor)
			if err != nil {
				return nil, RecordsListOutput{}, toolError(err)
			}
			return nil, out, nil
		case "plan", "document":
			out, err := backend.ListContentItems(ctx, in.ProjectKey, kind, in.Limit, in.Cursor, in.IncludeArchived)
			if err != nil {
				return nil, RecordsListOutput{}, toolError(err)
			}
			return nil, out, nil
		default:
			return nil, RecordsListOutput{}, toolError(&service.Error{Code: domain.ErrValidationFailed, Field: "kind", Message: "kind must be \"decision\", \"plan\", or \"document\""})
		}
	})

	addTool(s, &mcp.Tool{
		Name:         "record_versions",
		Description:  "List archived prior versions, oldest first, including representation-specific Markdown, path, URL, or file metadata. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[RecordVersionsOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recordVersionsInput) (*mcp.CallToolResult, RecordVersionsOutput, error) {
		kind, kerr := recordRefKind(in.Ref)
		if kerr != nil {
			return nil, RecordVersionsOutput{}, toolError(kerr)
		}
		switch kind {
		case domain.KindDecision:
			out, err := backend.GetDecisionVersions(ctx, in.Ref)
			if err != nil {
				return nil, RecordVersionsOutput{}, toolError(err)
			}
			out.Versions, out.NextCursor, err = paginate(out.Versions, in.Limit, in.Cursor)
			if err != nil {
				return nil, RecordVersionsOutput{}, toolError(err)
			}
			return nil, out, nil
		case domain.KindPlan, domain.KindDocument:
			out, err := backend.GetContentItemVersions(ctx, in.Ref)
			if err != nil {
				return nil, RecordVersionsOutput{}, toolError(err)
			}
			out.Versions, out.NextCursor, err = paginate(out.Versions, in.Limit, in.Cursor)
			if err != nil {
				return nil, RecordVersionsOutput{}, toolError(err)
			}
			return nil, out, nil
		default:
			return nil, RecordVersionsOutput{}, toolError(errRecordRefKind())
		}
	})

	addTool(s, &mcp.Tool{
		Name:         "record_diff",
		Description:  "Diff two record versions line by line. Decisions include their text fields; plans and documents include title and Markdown body only.",
		OutputSchema: outputSchemaFor[RecordDiff](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recordDiffInput) (*mcp.CallToolResult, RecordDiff, error) {
		kind, kerr := recordRefKind(in.Ref)
		if kerr != nil {
			return nil, RecordDiff{}, toolError(kerr)
		}
		switch kind {
		case domain.KindDecision:
			out, err := backend.GetDecisionDiff(ctx, in.Ref, in.From, in.To)
			if err != nil {
				return nil, RecordDiff{}, toolError(err)
			}
			return nil, out, nil
		case domain.KindPlan, domain.KindDocument:
			out, err := backend.GetContentItemDiff(ctx, in.Ref, in.From, in.To)
			if err != nil {
				return nil, RecordDiff{}, toolError(err)
			}
			return nil, out, nil
		default:
			return nil, RecordDiff{}, toolError(errRecordRefKind())
		}
	})

	addTool(s, &mcp.Tool{
		Name:         "ticket_create",
		Description:  "Create a backlog ticket. Exactly one of feature or general:true is required. Priority defaults to medium.",
		OutputSchema: outputSchemaFor[domain.Ticket](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketCreateInput) (*mcp.CallToolResult, domain.Ticket, error) {
		ticket, err := backend.CreateTicket(withCallerActor(ctx, req), CreateTicketInput(in))
		if err != nil {
			return nil, domain.Ticket{}, toolError(err)
		}
		return nil, ticket, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "search",
		Description:  "Search projects, tickets, features, decisions, plans, documents, comments, attachment names, and external links by relevance. A comment, attachment, or link hit's ref identifies its owner. Reuse filters with next_cursor.",
		OutputSchema: outputSchemaFor[SearchOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, SearchOutput, error) {
		out, err := backend.Search(ctx, SearchInput(in))
		if err != nil {
			return nil, SearchOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "project_activity",
		Description:  "List a project's audit events newest first, optionally filtered by actor, entity kind, or event type. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[ActivityListOutput](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in projectActivityInput) (*mcp.CallToolResult, ActivityListOutput, error) {
		out, err := backend.ListActivity(ctx, in.ProjectKey, in.Actor, in.EntityKind, in.EventType, in.Limit, in.Cursor)
		if err != nil {
			return nil, ActivityListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "notifications_list",
		Description:  "List the caller's notifications newest first. Set unread:true to return only unread notifications. Use next_cursor to continue.",
		OutputSchema: outputSchemaFor[NotificationsListOutput](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in notificationsListInput) (*mcp.CallToolResult, NotificationsListOutput, error) {
		out, err := backend.ListNotifications(withCallerActor(ctx, req), in.Unread, in.Limit, in.Cursor)
		if err != nil {
			return nil, NotificationsListOutput{}, toolError(err)
		}
		return nil, out, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "notifications_mark_read",
		Description:  "Mark specified notification IDs read, or set all:true to mark every unread notification. When all is true, ids is ignored.",
		OutputSchema: outputSchemaFor[notificationsMarkReadOutput](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in notificationsMarkReadInput) (*mcp.CallToolResult, notificationsMarkReadOutput, error) {
		n, err := backend.MarkNotificationsRead(withCallerActor(ctx, req), in.IDs, in.All)
		if err != nil {
			return nil, notificationsMarkReadOutput{}, toolError(err)
		}
		return nil, notificationsMarkReadOutput{Marked: n}, nil
	})

	addTool(s, &mcp.Tool{
		Name:         "subscription_update",
		Description:  "Subscribe or unsubscribe the caller from notifications for a ticket, feature, decision, plan, or document.",
		OutputSchema: outputSchemaFor[subscriptionUpdateOutput](),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in subscriptionUpdateInput) (*mcp.CallToolResult, subscriptionUpdateOutput, error) {
		if err := backend.SetSubscription(withCallerActor(ctx, req), in.Ref, in.Subscribed); err != nil {
			return nil, subscriptionUpdateOutput{}, toolError(err)
		}
		return nil, subscriptionUpdateOutput(in), nil
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
	Key string `json:"project_key,omitempty" jsonschema:"Project key, for example ABC. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
}

type projectsListInput struct {
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"Include archived projects; false or omitted returns active projects only."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor          string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page. Do not construct or parse it."`
}

type projectCreateInput struct {
	Key            string `json:"project_key" jsonschema:"Project key: 2-10 uppercase letters or digits, starting with a letter."`
	Title          string `json:"title" jsonschema:"Project title."`
	Description    string `json:"description,omitempty" jsonschema:"Markdown description."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Retry key. Reuse only for identical input; different input returns idempotency_key_reused."`
}

type projectUpdateInput struct {
	Key             string  `json:"project_key" jsonschema:"Project key, for example ABC."`
	Title           *string `json:"title,omitempty" jsonschema:"Project title; omit to leave unchanged."`
	Description     *string `json:"description,omitempty" jsonschema:"Markdown description; omit to leave unchanged."`
	Status          *string `json:"status,omitempty" jsonschema:"active or archived; omit to leave unchanged. Send separately from title or description."`
	ExpectedVersion int64   `json:"expected_version" jsonschema:"Latest project version returned by a read or write."`
}

type ticketGetInput struct {
	Ref            string `json:"ref" jsonschema:"Public ticket reference, for example ABC-123."`
	IncludeDeleted bool   `json:"include_deleted,omitempty" jsonschema:"Include a soft-deleted ticket; false or omitted reads live tickets only."`
}

type projectActivityInput struct {
	ProjectKey string `json:"project_key,omitempty" jsonschema:"Project key. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
	Actor      string `json:"actor,omitempty" jsonschema:"Filter by actor reference, for example agent:codex or human:alice."`
	EntityKind string `json:"entity_kind,omitempty" jsonschema:"Filter by project, ticket, feature, decision, plan, or document."`
	EventType  string `json:"event_type,omitempty" jsonschema:"Filter by an exact event_type returned by this tool, for example ticket_created or comment_added."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor     string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page. Reuse the same filters."`
}

type searchInput struct {
	Query   string   `json:"query" jsonschema:"Search text."`
	Project string   `json:"project_key,omitempty" jsonschema:"Filter by project key; omitted searches every project."`
	Kind    []string `json:"kind,omitempty" jsonschema:"Filter by project, ticket, feature, decision, plan, document, comment, attachment, or link; omitted searches every kind."`
	Status  string   `json:"status,omitempty" jsonschema:"Filter by workflow or decision status. Kinds without status do not match."`
	Limit   int      `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page. Reuse the same filters."`
}

type notificationsListInput struct {
	Unread bool   `json:"unread,omitempty" jsonschema:"Return only unread notifications; false or omitted returns all."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type notificationsMarkReadInput struct {
	IDs []int64 `json:"ids,omitempty" jsonschema:"Notification IDs to mark read."`
	All bool    `json:"all,omitempty" jsonschema:"Mark every unread notification read; ignores ids when true."`
}

type notificationsMarkReadOutput struct {
	Marked int64 `json:"marked"`
}

type subscriptionUpdateInput struct {
	Ref        string `json:"ref" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	Subscribed bool   `json:"subscribed" jsonschema:"true to subscribe; false to unsubscribe."`
}

type subscriptionUpdateOutput struct {
	Ref        string `json:"ref"`
	Subscribed bool   `json:"subscribed"`
}

type ticketsListInput struct {
	ProjectKey   string `json:"project_key,omitempty" jsonschema:"Project key. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
	View         string `json:"view,omitempty" jsonschema:"priority_queue (default) or issue_register."`
	Status       string `json:"status,omitempty" jsonschema:"Filter by backlog, ready, in_progress, blocked, review, done, or cancelled."`
	Type         string `json:"type,omitempty" jsonschema:"Filter by task, bug, security, or chore."`
	Severity     string `json:"severity,omitempty" jsonschema:"Filter by critical, high, medium, or low; bug and security tickets only."`
	Priority     string `json:"priority,omitempty" jsonschema:"Filter by critical, high, medium, or low."`
	FeatureRef   string `json:"feature_ref,omitempty" jsonschema:"Filter by a same-project feature reference."`
	Assignee     string `json:"assignee,omitempty" jsonschema:"Filter by actor reference, for example agent:codex or human:alice."`
	Creator      string `json:"creator,omitempty" jsonschema:"Filter by creator actor reference."`
	UpdatedSince string `json:"updated_since,omitempty" jsonschema:"Filter to tickets updated at or after this RFC3339 timestamp."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor       string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page. Reuse the same view and filters."`
}

type ticketUpdateInput struct {
	Ref             string  `json:"ref" jsonschema:"Public ticket reference, for example ABC-123."`
	Status          *string `json:"status,omitempty" jsonschema:"backlog, ready, in_progress, blocked, review, done, or cancelled. Send alone."`
	Type            *string `json:"type,omitempty" jsonschema:"task, bug, security, or chore."`
	Title           *string `json:"title,omitempty" jsonschema:"Ticket title."`
	Description     *string `json:"description,omitempty" jsonschema:"Markdown description."`
	Priority        *string `json:"priority,omitempty" jsonschema:"critical, high, medium, or low."`
	Severity        *string `json:"severity,omitempty" jsonschema:"critical, high, medium, or low; bug and security tickets only. Send an empty string to clear."`
	Assignee        *string `json:"assignee,omitempty" jsonschema:"Registered actor reference. Send an empty string to clear. Send alone."`
	Feature         *string `json:"feature,omitempty" jsonschema:"Destination feature reference in the same project. Send alone."`
	ExpectedVersion int64   `json:"expected_version" jsonschema:"Latest ticket version returned by a read or write."`
}

type ticketReorderInput struct {
	Ref             string `json:"ref" jsonschema:"Public ticket reference, for example ABC-123."`
	AfterRef        string `json:"after_ref,omitempty" jsonschema:"Same-priority ticket to follow; omit to move to the front of the group."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"Latest ticket version returned by a read or write."`
}

type ticketDeleteInput struct {
	Ref             string `json:"ref" jsonschema:"Public ticket reference, for example ABC-123."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"Latest ticket version returned by a read or write."`
}

type ticketRestoreInput struct {
	Ref             string `json:"ref" jsonschema:"Public ticket reference, for example ABC-123."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"Deleted ticket version returned by ticket_delete or ticket_get with include_deleted:true."`
}

type ticketCommentInput struct {
	Ref            string `json:"ref" jsonschema:"Project key or ticket, feature, decision, plan, or document reference."`
	Body           string `json:"body" jsonschema:"Markdown comment body."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Retry key. Reuse only for identical input; different input returns idempotency_key_reused."`
}

type commentsListInput struct {
	Ref    string `json:"ref" jsonschema:"Project key or ticket, feature, decision, plan, or document reference."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type commentUpdateInput struct {
	ID              int64  `json:"id" jsonschema:"Comment ID."`
	Body            string `json:"body" jsonschema:"Replacement Markdown body."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"Latest comment version returned by a read or write."`
}

type commentDeleteInput struct {
	ID              int64 `json:"id" jsonschema:"Comment ID."`
	ExpectedVersion int64 `json:"expected_version" jsonschema:"Latest comment version returned by a read or write."`
}

type commentIDInput struct {
	ID int64 `json:"id" jsonschema:"Comment ID."`
}

type commentHistoryInput struct {
	ID     int64  `json:"id" jsonschema:"Comment ID."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type ticketLinkInput struct {
	Ref    string `json:"ref" jsonschema:"Source ticket reference."`
	Type   string `json:"type" jsonschema:"parent_of, child_of, blocks, blocked_by, related_to, duplicate_of, supersedes, or superseded_by."`
	Target string `json:"target" jsonschema:"Target ticket reference."`
}

type associationInput struct {
	Ref    string `json:"ref" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	Target string `json:"target" jsonschema:"Other ticket, feature, decision, plan, or document reference."`
}

type ticketRelationshipsInput struct {
	Ref    string `json:"ref" jsonschema:"Public ticket reference, for example ABC-123."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type backlinksListInput struct {
	Ref    string `json:"ref" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type backlinksListOutput struct {
	Backlinks  []BacklinkView `json:"backlinks"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type attachmentGetInput struct {
	ID int64 `json:"id" jsonschema:"Attachment ID."`
}

type attachmentsListInput struct {
	Ref       string `json:"ref,omitempty" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	CommentID int64  `json:"comment_id,omitempty" jsonschema:"Comment ID."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor    string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type attachmentVersionsInput struct {
	ID     int64  `json:"id" jsonschema:"Attachment ID."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type linkAddInput struct {
	Ref   string `json:"ref" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	Title string `json:"title" jsonschema:"Short bookmark label."`
	URL   string `json:"url" jsonschema:"HTTP, HTTPS, or mailto URL."`
}

type linksListInput struct {
	Ref    string `json:"ref" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type linksListOutput struct {
	Links      []LinkView `json:"links"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type linkRemoveInput struct {
	Ref string `json:"ref" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	ID  int64  `json:"id" jsonschema:"External link ID."`
}

type ticketAssociationsInput struct {
	Ref    string `json:"ref" jsonschema:"Ticket, feature, decision, plan, or document reference."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type featureGetInput struct {
	Ref            string `json:"ref" jsonschema:"Public feature reference, for example ABC-F1."`
	IncludeDeleted bool   `json:"include_deleted,omitempty" jsonschema:"Include a soft-deleted feature; false or omitted reads live features only."`
}

type featuresListInput struct {
	ProjectKey   string `json:"project_key,omitempty" jsonschema:"Project key. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
	Status       string `json:"status,omitempty" jsonschema:"Filter by backlog, ready, in_progress, blocked, review, done, or cancelled."`
	Priority     string `json:"priority,omitempty" jsonschema:"Filter by critical, high, medium, or low."`
	Creator      string `json:"creator,omitempty" jsonschema:"Filter by creator actor reference."`
	UpdatedSince string `json:"updated_since,omitempty" jsonschema:"Filter to features updated at or after this RFC3339 timestamp."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor       string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page. Reuse the same filters."`
}

type featureCreateInput struct {
	ProjectKey     string `json:"project_key,omitempty" jsonschema:"Project key. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
	Title          string `json:"title" jsonschema:"Feature title."`
	Description    string `json:"description,omitempty" jsonschema:"Markdown description."`
	Priority       string `json:"priority,omitempty" jsonschema:"critical, high, medium, or low; default medium."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Retry key. Reuse only for identical input; different input returns idempotency_key_reused."`
}

type featureUpdateInput struct {
	Ref             string  `json:"ref" jsonschema:"Public feature reference, for example ABC-F1."`
	Title           *string `json:"title,omitempty" jsonschema:"Feature title."`
	Description     *string `json:"description,omitempty" jsonschema:"Markdown description."`
	Priority        *string `json:"priority,omitempty" jsonschema:"critical, high, medium, or low."`
	Status          *string `json:"status,omitempty" jsonschema:"backlog, ready, in_progress, blocked, review, done, or cancelled. Send alone."`
	ExpectedVersion int64   `json:"expected_version" jsonschema:"Latest feature version returned by a read or write."`
}

type featureReorderInput struct {
	Ref             string `json:"ref" jsonschema:"Public feature reference, for example ABC-F1."`
	AfterRef        string `json:"after_ref,omitempty" jsonschema:"Same-priority feature to follow; omit to move to the front of the group."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"Latest feature version returned by a read or write."`
}

type featureDeleteInput struct {
	Ref             string `json:"ref" jsonschema:"Public feature reference, for example ABC-F1."`
	Cascade         bool   `json:"cascade,omitempty" jsonschema:"Also soft-delete the feature's live tickets; false refuses deletion when tickets remain."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"Latest feature version returned by a read or write."`
}

type featureRestoreInput struct {
	Ref             string `json:"ref" jsonschema:"Public feature reference, for example ABC-F1."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"Deleted feature version returned by feature_delete or feature_get with include_deleted:true."`
}

type recordGetInput struct {
	Ref string `json:"ref" jsonschema:"Decision, plan, or document reference."`
}

type recordsListInput struct {
	ProjectKey      string `json:"project_key,omitempty" jsonschema:"Project key. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
	Kind            string `json:"kind,omitempty" jsonschema:"decision (default), plan, or document."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor          string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"Plan or document only: also return archived items. Default false."`
}

type recordVersionsInput struct {
	Ref    string `json:"ref" jsonschema:"Decision, plan, or document reference."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum rows; default 20, maximum 100."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding page."`
}

type recordDiffInput struct {
	Ref  string `json:"ref" jsonschema:"Decision, plan, or document reference."`
	From int64  `json:"from" jsonschema:"Earlier version from record_versions or the entity's current version."`
	To   int64  `json:"to" jsonschema:"Later version from record_versions or the entity's current version."`
}

type recordCreateInput struct {
	ProjectKey     string `json:"project_key,omitempty" jsonschema:"Project key. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
	Kind           string `json:"kind,omitempty" jsonschema:"decision (default), plan, or document."`
	Title          string `json:"title" jsonschema:"Record title."`
	Context        string `json:"context,omitempty" jsonschema:"Decision only: Markdown describing the situation."`
	Decision       string `json:"decision,omitempty" jsonschema:"Decision only: Markdown describing what was decided."`
	Rationale      string `json:"rationale,omitempty" jsonschema:"Decision only: Markdown explaining why."`
	Consequences   string `json:"consequences,omitempty" jsonschema:"Decision only: Markdown describing expected consequences."`
	Representation string `json:"representation,omitempty" jsonschema:"Plan or document only: markdown (default), path, or url. Immutable after creation."`
	Body           string `json:"body,omitempty" jsonschema:"Plan or document with markdown representation: Markdown body."`
	Path           string `json:"path,omitempty" jsonschema:"Plan or document with path representation: opaque path stored but never opened or resolved."`
	URL            string `json:"url,omitempty" jsonschema:"Plan or document with url representation: HTTP, HTTPS, or mailto URL."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Retry key. Reuse only for identical input; different input returns idempotency_key_reused."`
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
	Ref             string  `json:"ref" jsonschema:"Decision, plan, or document reference."`
	Title           string  `json:"title" jsonschema:"Full-replacement title; always required."`
	Context         *string `json:"context,omitempty" jsonschema:"Decision only; required. Resend the current Markdown value if unchanged."`
	Decision        *string `json:"decision,omitempty" jsonschema:"Decision only; required. Resend the current Markdown value if unchanged."`
	Rationale       *string `json:"rationale,omitempty" jsonschema:"Decision only; required. Resend the current Markdown value if unchanged."`
	Consequences    *string `json:"consequences,omitempty" jsonschema:"Decision only; required. Resend the current Markdown value if unchanged."`
	Status          *string `json:"status,omitempty" jsonschema:"Decision: required — proposed, accepted, rejected, or superseded. Plan or document: optional — active or archived; omit to leave the current status unchanged."`
	SupersededBy    string  `json:"superseded_by,omitempty" jsonschema:"Decision only: replacement decision reference. Omit or send empty to clear."`
	Body            string  `json:"body,omitempty" jsonschema:"Markdown plan/document only. Resend the current body if unchanged."`
	Path            string  `json:"path,omitempty" jsonschema:"Path plan/document only. Resend the current opaque path if unchanged."`
	URL             string  `json:"url,omitempty" jsonschema:"URL plan/document only. Resend the current URL if unchanged."`
	ExpectedVersion int64   `json:"expected_version" jsonschema:"Latest record version returned by a read or write."`
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
	ProjectKey     string `json:"project_key,omitempty" jsonschema:"Project key. Omission uses the stdio bridge's configured default; direct /mcp connections have no default."`
	Type           string `json:"type" jsonschema:"task, bug, security, or chore."`
	Title          string `json:"title" jsonschema:"Ticket title."`
	Description    string `json:"description,omitempty" jsonschema:"Markdown description."`
	Priority       string `json:"priority,omitempty" jsonschema:"critical, high, medium, or low; default medium."`
	Severity       string `json:"severity,omitempty" jsonschema:"critical, high, medium, or low; bug and security tickets only."`
	Feature        string `json:"feature,omitempty" jsonschema:"Destination feature reference; required unless general is true."`
	General        bool   `json:"general,omitempty" jsonschema:"Set true to use the project's General feature; required unless feature is set."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Retry key. Reuse only for identical input; different input returns idempotency_key_reused."`
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
		detail := ""
		if svcErr.Field != "" {
			detail += "; field=" + svcErr.Field
		}
		if svcErr.CurrentVersion != nil {
			detail += fmt.Sprintf("; current_version=%d", *svcErr.CurrentVersion)
		}
		return fmt.Errorf("%s: %s%s", svcErr.Code, svcErr.Message, detail)
	}
	return fmt.Errorf("%s: an unexpected error occurred", domain.ErrInternal)
}
