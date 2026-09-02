package mcpsrv

import (
	"time"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// ProjectCompact/TicketCompact are the compact list-row shapes
// projects_list/tickets_list return — deliberately not domain.Project/
// domain.Ticket, which carry Description and would violate product
// spec §7.2/§11's "list and search omit full bodies" rule the moment a
// list tool's OutputSchema included one. This mirrors
// internal/httpapi/wire.go's projectCompact/ticketCompact split and
// internal/apiclient's ProjectCompact/TicketCompact field-for-field —
// three independent DTOs at three boundaries is the existing
// convention (see apiclient/dto.go's own doc comment on why it doesn't
// reuse domain structs), not something introduced here.
type ProjectCompact struct {
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SearchHit is one ranked search result (product spec §5.12) — mirrors
// internal/httpapi/search.go's searchHit and internal/apiclient's
// SearchHit field-for-field, the same three-DTOs-at-three-boundaries
// convention ProjectCompact's doc comment explains.
type SearchHit struct {
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	CommentID *int64 `json:"comment_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Snippet   string `json:"snippet"`
}

type SearchOutput struct {
	Hits       []SearchHit `json:"hits"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// NotificationCompact mirrors internal/httpapi/notifications.go's
// notificationView and internal/apiclient's Notification field-for-
// field, the same three-DTOs-at-three-boundaries convention
// ProjectCompact's doc comment explains.
type NotificationCompact struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Entity      string     `json:"entity"`
	EntityKind  string     `json:"entity_kind"`
	CommentID   *int64     `json:"comment_id,omitempty"`
	TriggeredBy string     `json:"triggered_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
}

type NotificationsListOutput struct {
	Notifications []NotificationCompact `json:"notifications"`
	NextCursor    string                `json:"next_cursor,omitempty"`
}

type ProjectsListOutput struct {
	Projects   []ProjectCompact `json:"projects"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type TicketCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Severity  *string   `json:"severity,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version"`
}

type TicketsListOutput struct {
	Tickets    []TicketCompact `json:"tickets"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// TicketWriteResult is every ticket-mutating tool's output (product
// spec §7.2: "writes return the changed entity's stable reference,
// version, essential fields, and warnings rather than echoing an
// entire expanded record") — deliberately not domain.Ticket, which
// ticket_create still returns in full as a documented exception (a
// freshly created ticket is exactly the one time an agent needs the
// whole record back, having supplied none of it itself).
type TicketWriteResult struct {
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Assignee  string    `json:"assignee,omitempty"`
	Feature   string    `json:"feature,omitempty"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toTicketWriteResult(t domain.Ticket) TicketWriteResult {
	out := TicketWriteResult{Ref: t.Ref, Status: string(t.Status), Priority: string(t.Priority), Feature: t.FeatureRef, Version: t.Version, UpdatedAt: t.UpdatedAt}
	if t.Assignee != nil {
		out.Assignee = string(t.Assignee.Kind) + ":" + t.Assignee.Name
	}
	return out
}

// DeleteWriteResult is ticket_delete/feature_delete's output — a soft
// delete leaves nothing new to report beyond the ref and the version
// the delete itself produced, same "essential fields only" rule as
// TicketWriteResult/FeatureWriteResult.
type DeleteWriteResult struct {
	Ref     string `json:"ref"`
	Version int64  `json:"version"`
}

// CommentCompact is comments_list's row shape — no Body, same
// compact/detail split every other *_list tool follows (product spec
// §7.2, enforced by TestListToolsOmitFullBodies). comment_get follows
// up with the full body for any one row.
type CommentCompact struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toCommentCompact(c domain.Comment) CommentCompact {
	return CommentCompact{
		ID: c.ID, Author: string(c.Author.Kind) + ":" + c.Author.Name,
		Version: c.Version, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// CommentsListOutput is comments_list's output — unpaginated on the
// wire, matching GET .../comments' own contract (no next_cursor).
type CommentsListOutput struct {
	Comments   []CommentCompact `json:"comments"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// CommentHistoryOutput is comment_history's output — comment_update
// archives the prior body into a version row on every edit
// (service.EditComment), so this is the only way to see a comment's
// earlier text.
type CommentHistoryOutput struct {
	Versions   []domain.CommentVersion `json:"versions"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

// ActivityEventView mirrors internal/httpapi/activity.go's
// activityEvent field-for-field (product spec §5.10's project
// activity feed).
type ActivityEventView struct {
	ID             int64     `json:"id"`
	Entity         string    `json:"entity,omitempty"`
	EntityKind     string    `json:"entity_kind"`
	Actor          string    `json:"actor"`
	EventType      string    `json:"event_type"`
	CommentID      int64     `json:"comment_id,omitempty"`
	CommentExcerpt string    `json:"comment_excerpt,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ActivityListOutput is project_activity's output.
type ActivityListOutput struct {
	Events     []ActivityEventView `json:"events"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// BacklinkView is one entity/comment currently mentioning a ref via a
// #ref backlink (product spec §6.1) — mirrors service.Backlink with
// string fields, the same reasoning every other Backend-facing type
// gives for uniform string shapes across both implementations.
type BacklinkView struct {
	Ref       string `json:"ref"`
	CommentID int64  `json:"comment_id,omitempty"`
}

// LinkView is one named external link (product spec §5.11) — the
// shape link_add and links_list both return. No version column and no
// in-place edit (service.ExternalLink's doc): change a link by
// removing and re-adding it.
type LinkView struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type AttachmentView struct {
	ID             int64      `json:"id"`
	OwnerRef       string     `json:"owner_ref,omitempty"`
	CommentID      int64      `json:"comment_id,omitempty"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title"`
	CurrentVersion int64      `json:"current_version"`
	FileName       string     `json:"file_name,omitempty"`
	FileSize       int64      `json:"file_size,omitempty"`
	MediaType      string     `json:"media_type,omitempty"`
	Checksum       string     `json:"checksum,omitempty"`
	PathValue      string     `json:"path_value,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Creator        string     `json:"creator"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

type AttachmentVersionView struct {
	Version    int64     `json:"version"`
	Kind       string    `json:"kind"`
	FileName   string    `json:"file_name,omitempty"`
	FileSize   int64     `json:"file_size,omitempty"`
	MediaType  string    `json:"media_type,omitempty"`
	Checksum   string    `json:"checksum,omitempty"`
	PathValue  string    `json:"path_value,omitempty"`
	UploadedBy string    `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

type AttachmentsListOutput struct {
	Attachments []AttachmentView `json:"attachments"`
	NextCursor  string           `json:"next_cursor,omitempty"`
}

type AttachmentVersionsOutput struct {
	Versions   []AttachmentVersionView `json:"versions"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

// CommentDeleteResult is comment_delete's output. The server's own
// response to DELETE /comments/{id} carries nothing but a status
// string (apiclient.DeleteComment's doc) — id is enough to confirm
// which comment was affected.
type CommentDeleteResult struct {
	ID int64 `json:"id"`
}

// CommentWriteResult is ticket_comment's output — product spec §7.2's
// "writes return the changed entity's stable reference, version, and
// essential fields" rule, applied to a comment (which has no public
// reference of its own, only an integer id — see apiclient.Comment's
// doc comment).
type CommentWriteResult struct {
	ID        int64     `json:"id"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// RelationshipView is one edge in ticket_relationships' output —
// mirrors service.RelationshipView/apiclient's relationship wire shape
// with string fields, the same reasoning CreateTicketInput's doc
// comment gives for using strings uniformly across both Backend
// implementations.
type RelationshipView struct {
	Type  string `json:"type"`
	Other string `json:"other"`
}

// RelationshipsOutput is ticket_relationships' output.
type RelationshipsOutput struct {
	Relationships []RelationshipView `json:"relationships"`
	NextCursor    string             `json:"next_cursor,omitempty"`
}

// AssociationsOutput is ticket_associations' output — a bare list of
// the other entity's references, mirroring apiclient.AssociationsPage/
// cmd/tickets' `ticket associations` table exactly (product spec §5.7:
// an association carries no type or version of its own to echo back).
type AssociationsOutput struct {
	Associated []string `json:"associated"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// LinkWriteResult is ticket_link's output — an edge has no version or
// id of its own to echo back (product spec §5.7: a duplicate add is
// already 409 already_exists, not a versioned resource), so this
// simply confirms what was created.
type LinkWriteResult struct {
	Ref    string `json:"ref"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

// FeatureCompact is one row of features_list — mirrors
// internal/httpapi/wire.go's featureCompact/apiclient.FeatureCompact
// field-for-field, same three-DTO convention ProjectCompact/
// TicketCompact's doc comment explains.
type FeatureCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FeaturesListOutput struct {
	Features   []FeatureCompact `json:"features"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func toFeatureCompact(f domain.Feature) FeatureCompact {
	return FeatureCompact{Ref: f.Ref, Title: f.Title, Status: string(f.Status), Priority: string(f.Priority), Version: f.Version, UpdatedAt: f.UpdatedAt}
}

func fromAPIFeatureCompact(f apiclient.FeatureCompact) FeatureCompact {
	return FeatureCompact{Ref: f.Ref, Title: f.Title, Status: f.Status, Priority: f.Priority, Version: f.Version, UpdatedAt: f.UpdatedAt}
}

// FeatureBriefRow is one row of project_brief's features section —
// FeatureCompact plus a ticket-progress summary, mirroring
// internal/httpapi/project_brief.go's featureBriefRow/
// apiclient.FeatureBriefRow field-for-field.
type FeatureBriefRow struct {
	FeatureCompact
	TicketsTotal int `json:"tickets_total"`
	TicketsDone  int `json:"tickets_done"`
}

func toFeatureBriefRow(f service.FeatureBriefRow) FeatureBriefRow {
	return FeatureBriefRow{FeatureCompact: toFeatureCompact(f.Feature), TicketsTotal: f.TicketsTotal, TicketsDone: f.TicketsDone}
}

func fromAPIFeatureBriefRow(f apiclient.FeatureBriefRow) FeatureBriefRow {
	return FeatureBriefRow{
		FeatureCompact: FeatureCompact{Ref: f.Ref, Title: f.Title, Status: f.Status, Priority: f.Priority, Version: f.Version, UpdatedAt: f.UpdatedAt},
		TicketsTotal:   f.TicketsTotal, TicketsDone: f.TicketsDone,
	}
}

// DecisionCompact is one row of project_brief's recent_decisions
// section — mirrors internal/httpapi/wire.go's decisionCompact/
// apiclient.DecisionCompact field-for-field.
type DecisionCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toDecisionCompact(d domain.Decision) DecisionCompact {
	return DecisionCompact{Ref: d.Ref, Title: d.Title, Status: string(d.Status), Version: d.Version, UpdatedAt: d.UpdatedAt}
}

func fromAPIDecisionCompact(d apiclient.DecisionCompact) DecisionCompact {
	return DecisionCompact{Ref: d.Ref, Title: d.Title, Status: d.Status, Version: d.Version, UpdatedAt: d.UpdatedAt}
}

// ContentItemCompact is one row of project_brief's recent_plans
// section — mirrors internal/httpapi/wire.go's contentItemCompact/
// apiclient.ContentItemCompact field-for-field.
type ContentItemCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toContentItemCompact(c domain.ContentItem) ContentItemCompact {
	return ContentItemCompact{Ref: c.Ref, Title: c.Title, Kind: string(c.Kind), Version: c.Version, UpdatedAt: c.UpdatedAt}
}

func fromAPIContentItemCompact(c apiclient.ContentItemCompact) ContentItemCompact {
	return ContentItemCompact{Ref: c.Ref, Title: c.Title, Kind: c.Kind, Version: c.Version, UpdatedAt: c.UpdatedAt}
}

// ActivityEvent is one row of project_brief's recent_activity section
// — mirrors internal/httpapi/activity.go's activityEvent/
// apiclient.ActivityEvent field-for-field.
type ActivityEvent struct {
	ID             int64     `json:"id"`
	Entity         string    `json:"entity,omitempty"`
	EntityKind     string    `json:"entity_kind"`
	Actor          string    `json:"actor"`
	EventType      string    `json:"event_type"`
	CommentID      *int64    `json:"comment_id,omitempty"`
	CommentExcerpt *string   `json:"comment_excerpt,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

func toActivityEvent(e service.ActivityEvent) ActivityEvent {
	return ActivityEvent{
		ID: e.ID, Entity: e.EntityRef, EntityKind: string(e.EntityKind), Actor: e.Actor.String(),
		EventType: e.EventType, CommentID: e.CommentID, CommentExcerpt: e.CommentBody, CreatedAt: e.CreatedAt,
	}
}

func fromAPIActivityEvent(e apiclient.ActivityEvent) ActivityEvent {
	return ActivityEvent{
		ID: e.ID, Entity: e.Entity, EntityKind: e.EntityKind, Actor: e.Actor, EventType: e.EventType,
		CommentID: e.CommentID, CommentExcerpt: e.CommentExcerpt, CreatedAt: e.CreatedAt,
	}
}

// ProjectBrief is project_brief's output — mirrors
// internal/httpapi/project_brief.go's projectBriefView field-for-field.
type ProjectBrief struct {
	Project         domain.Project       `json:"project"`
	InProgress      []TicketCompact      `json:"in_progress"`
	IssueRegister   []TicketCompact      `json:"issue_register"`
	Features        []FeatureBriefRow    `json:"features"`
	RecentActivity  []ActivityEvent      `json:"recent_activity"`
	RecentDecisions []DecisionCompact    `json:"recent_decisions"`
	RecentPlans     []ContentItemCompact `json:"recent_plans"`
}

func toProjectBrief(b service.ProjectBrief) ProjectBrief {
	inProgress := make([]TicketCompact, len(b.InProgress))
	for i, t := range b.InProgress {
		inProgress[i] = toTicketCompact(t)
	}
	issues := make([]TicketCompact, len(b.IssueRegister))
	for i, t := range b.IssueRegister {
		issues[i] = toTicketCompact(t)
	}
	features := make([]FeatureBriefRow, len(b.Features))
	for i, f := range b.Features {
		features[i] = toFeatureBriefRow(f)
	}
	activity := make([]ActivityEvent, len(b.RecentActivity))
	for i, e := range b.RecentActivity {
		activity[i] = toActivityEvent(e)
	}
	decisions := make([]DecisionCompact, len(b.RecentDecisions))
	for i, d := range b.RecentDecisions {
		decisions[i] = toDecisionCompact(d)
	}
	plans := make([]ContentItemCompact, len(b.RecentPlans))
	for i, p := range b.RecentPlans {
		plans[i] = toContentItemCompact(p)
	}
	return ProjectBrief{
		Project: b.Project, InProgress: inProgress, IssueRegister: issues, Features: features,
		RecentActivity: activity, RecentDecisions: decisions, RecentPlans: plans,
	}
}

func fromAPIProjectBrief(b apiclient.ProjectBrief) ProjectBrief {
	inProgress := make([]TicketCompact, len(b.InProgress))
	for i, t := range b.InProgress {
		inProgress[i] = fromAPITicketCompact(t)
	}
	issues := make([]TicketCompact, len(b.IssueRegister))
	for i, t := range b.IssueRegister {
		issues[i] = fromAPITicketCompact(t)
	}
	features := make([]FeatureBriefRow, len(b.Features))
	for i, f := range b.Features {
		features[i] = fromAPIFeatureBriefRow(f)
	}
	activity := make([]ActivityEvent, len(b.RecentActivity))
	for i, e := range b.RecentActivity {
		activity[i] = fromAPIActivityEvent(e)
	}
	decisions := make([]DecisionCompact, len(b.RecentDecisions))
	for i, d := range b.RecentDecisions {
		decisions[i] = fromAPIDecisionCompact(d)
	}
	plans := make([]ContentItemCompact, len(b.RecentPlans))
	for i, p := range b.RecentPlans {
		plans[i] = fromAPIContentItemCompact(p)
	}
	var creator *string
	if b.Project.Creator != nil {
		creator = b.Project.Creator
	}
	proj := domain.Project{
		Key: b.Project.Key, Title: b.Project.Title, Description: b.Project.Description,
		Status: domain.ProjectStatus(b.Project.Status), Version: b.Project.Version,
		CreatedAt: b.Project.CreatedAt, UpdatedAt: b.Project.UpdatedAt,
	}
	if creator != nil {
		ref, err := domain.ParseActorRef(*creator)
		if err == nil {
			proj.Creator = &ref
		}
	}
	return ProjectBrief{
		Project: proj, InProgress: inProgress, IssueRegister: issues, Features: features,
		RecentActivity: activity, RecentDecisions: decisions, RecentPlans: plans,
	}
}

// FeatureWriteResult is feature_create/feature_update's output —
// product spec §7.2's "writes return the changed entity's stable
// reference, version, and essential fields" rule. feature_get, unlike
// these, still returns the full domain.Feature: a single feature is
// small enough that there's no compactness reason to trim it, and its
// only text field (Description) is exactly what a caller fetching by
// reference is usually after.
type FeatureWriteResult struct {
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toFeatureWriteResult(f domain.Feature) FeatureWriteResult {
	return FeatureWriteResult{Ref: f.Ref, Status: string(f.Status), Priority: string(f.Priority), Version: f.Version, UpdatedAt: f.UpdatedAt}
}

// DecisionWriteResult is CreateDecision/UpdateDecision's Backend-level
// output — product spec §7.2's "writes return the changed entity's
// stable reference, version, and essential fields" rule. record_get
// still returns the full record: same reasoning as feature_get (small
// enough, and its text fields are exactly what a caller fetching by
// reference wants). This is a Backend-internal shape, not what the
// record_create/record_update *tools* return on the wire — see
// RecordWriteResult, which the tool handlers (tools.go) convert this
// into, since one tool now answers decisions, plans, and documents.
type DecisionWriteResult struct {
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toDecisionWriteResult(d domain.Decision) DecisionWriteResult {
	return DecisionWriteResult{Ref: d.Ref, Status: string(d.Status), Version: d.Version, UpdatedAt: d.UpdatedAt}
}

// ContentItemWriteResult is CreateContentItem/UpdateContentItem's
// Backend-level output — mirrors DecisionWriteResult's shape (ref,
// status, version, updated_at).
type ContentItemWriteResult struct {
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toContentItemWriteResult(c domain.ContentItem) ContentItemWriteResult {
	return ContentItemWriteResult{Ref: c.Ref, Status: string(c.Status), Version: c.Version, UpdatedAt: c.UpdatedAt}
}

// RecordDetail is what the record_get tool actually returns on the
// wire — a superset shape covering decisions, plans, and documents,
// since mcp.AddTool fixes one Out type per tool registration and
// record_* now answers all three kinds (docs/adr/0017-content-items.md).
// Fields that don't apply to the fetched record's Kind are omitted
// (omitempty) rather than sent as null/""/0 — a plan or document's
// response has no context/decision/rationale/consequences/status/
// superseded_by, and a decision's response has no body.
type RecordDetail struct {
	Ref            string    `json:"ref"`
	Project        string    `json:"project"`
	Kind           string    `json:"kind"`
	Title          string    `json:"title"`
	Context        string    `json:"context,omitempty"`
	Decision       string    `json:"decision,omitempty"`
	Rationale      string    `json:"rationale,omitempty"`
	Consequences   string    `json:"consequences,omitempty"`
	Status         string    `json:"status,omitempty"`
	SupersededBy   *string   `json:"superseded_by,omitempty"`
	Representation string    `json:"representation,omitempty"`
	Body           string    `json:"body,omitempty"`
	FileName       string    `json:"file_name,omitempty"`
	MediaType      string    `json:"media_type,omitempty"`
	PathValue      string    `json:"path_value,omitempty"`
	URLValue       string    `json:"url_value,omitempty"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func toRecordDetailFromDecision(d domain.Decision) RecordDetail {
	return RecordDetail{
		Ref: d.Ref, Project: d.ProjectKey, Kind: "decision", Title: d.Title,
		Context: d.Context, Decision: d.Decision, Rationale: d.Rationale, Consequences: d.Consequences,
		Status: string(d.Status), SupersededBy: d.SupersededBy,
		Version: d.Version, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func toRecordDetailFromContentItem(c domain.ContentItem) RecordDetail {
	return RecordDetail{
		Ref: c.Ref, Project: c.ProjectKey, Kind: string(c.Kind), Title: c.Title,
		Representation: c.Representation, Body: c.Body,
		FileName: c.FileName, MediaType: c.MediaType, PathValue: c.PathValue, URLValue: c.URLValue,
		Status: string(c.Status), Version: c.Version, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// RecordCompact is records_list's row shape — no Context/Decision/
// Rationale/Consequences/Body, the same compact/detail split every
// other *_list tool follows (enforced by TestListToolsOmitFullBodies).
// Status is a decision's workflow status or a plan/document's
// active/archived lifecycle status (ADR 0028) — either way, whichever
// status the fetched record's Kind actually has.
type RecordCompact struct {
	Ref       string    `json:"ref"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Status    string    `json:"status,omitempty"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RecordsListOutput is records_list's output.
type RecordsListOutput struct {
	Records    []RecordCompact `json:"records"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// RecordVersion mirrors RecordDetail's union-of-fields approach — one
// archived state of a decision, plan, or document.
type RecordVersion struct {
	Version        int64     `json:"version"`
	Title          string    `json:"title"`
	Context        string    `json:"context,omitempty"`
	Decision       string    `json:"decision,omitempty"`
	Rationale      string    `json:"rationale,omitempty"`
	Consequences   string    `json:"consequences,omitempty"`
	Status         string    `json:"status,omitempty"`
	Representation string    `json:"representation,omitempty"`
	Body           string    `json:"body,omitempty"`
	FileName       string    `json:"file_name,omitempty"`
	FileSize       int64     `json:"file_size,omitempty"`
	MediaType      string    `json:"media_type,omitempty"`
	Checksum       string    `json:"checksum,omitempty"`
	PathValue      string    `json:"path_value,omitempty"`
	URLValue       string    `json:"url_value,omitempty"`
	EditedBy       string    `json:"edited_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// RecordVersionsOutput is record_versions' output.
type RecordVersionsOutput struct {
	Versions   []RecordVersion `json:"versions"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// DiffLineView mirrors domain.DiffLine with a string Op, the same
// string-fields convention every Backend-facing type uses.
type DiffLineView struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

func toDiffLineViews(lines []domain.DiffLine) []DiffLineView {
	out := make([]DiffLineView, len(lines))
	for i, l := range lines {
		out[i] = DiffLineView{Op: string(l.Op), Text: l.Text}
	}
	return out
}

// RecordDiff mirrors RecordDetail's union-of-fields approach for a
// line-level diff between two versions of a decision, plan, or
// document. Title is always present; the rest apply to one kind or
// the other.
type RecordDiff struct {
	FromVersion  int64          `json:"from_version"`
	ToVersion    int64          `json:"to_version"`
	Title        []DiffLineView `json:"title"`
	Context      []DiffLineView `json:"context,omitempty"`
	Decision     []DiffLineView `json:"decision,omitempty"`
	Rationale    []DiffLineView `json:"rationale,omitempty"`
	Consequences []DiffLineView `json:"consequences,omitempty"`
	Body         []DiffLineView `json:"body,omitempty"`
	StatusFrom   string         `json:"status_from,omitempty"`
	StatusTo     string         `json:"status_to,omitempty"`
}

// RecordWriteResult is what record_create/record_update actually
// return on the wire — generalizes DecisionWriteResult/
// ContentItemWriteResult to cover whichever kind was written. Kind
// lets a caller confirm what got created without a separate
// record_get, the same way Ref/Version already let it confirm the
// rest.
type RecordWriteResult struct {
	Ref       string    `json:"ref"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status,omitempty"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func recordWriteResultFromDecision(d DecisionWriteResult) RecordWriteResult {
	return RecordWriteResult{Ref: d.Ref, Kind: "decision", Status: d.Status, Version: d.Version, UpdatedAt: d.UpdatedAt}
}

func recordWriteResultFromContentItem(c ContentItemWriteResult, kind string) RecordWriteResult {
	return RecordWriteResult{Ref: c.Ref, Kind: kind, Status: c.Status, Version: c.Version, UpdatedAt: c.UpdatedAt}
}

// toProjectCompact/toTicketCompact convert from internal/domain's full
// entities — InProcessBackend's source of truth (*service.Service
// returns domain types directly).
func toProjectCompact(p domain.Project) ProjectCompact {
	return ProjectCompact{Key: p.Key, Title: p.Title, Status: string(p.Status), Version: p.Version, UpdatedAt: p.UpdatedAt}
}

func toTicketCompact(t domain.Ticket) TicketCompact {
	var severity *string
	if t.Severity != nil {
		v := string(*t.Severity)
		severity = &v
	}
	return TicketCompact{
		Ref: t.Ref, Title: t.Title, Type: string(t.Type), Status: string(t.Status),
		Priority: string(t.Priority), Severity: severity, UpdatedAt: t.UpdatedAt, Version: t.Version,
	}
}

// fromAPIProjectCompact/fromAPITicketCompact convert from apiclient's
// wire DTOs — HTTPBackend's source of truth. Kept separate from the
// domain-sourced converters above rather than routing HTTPBackend
// through a domain.Project/domain.Ticket round trip it doesn't
// otherwise need, matching httpbackend.go's existing
// toDomainProject/toDomainTicket pattern of converting directly at the
// boundary that needs it.
func fromAPIProjectCompact(p apiclient.ProjectCompact) ProjectCompact {
	return ProjectCompact{Key: p.Key, Title: p.Title, Status: p.Status, Version: p.Version, UpdatedAt: p.UpdatedAt}
}

func fromAPITicketCompact(t apiclient.TicketCompact) TicketCompact {
	return TicketCompact{
		Ref: t.Ref, Title: t.Title, Type: t.Type, Status: t.Status,
		Priority: t.Priority, Severity: t.Severity, UpdatedAt: t.UpdatedAt, Version: t.Version,
	}
}
