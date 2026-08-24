package mcpsrv

import (
	"time"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/domain"
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
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toTicketWriteResult(t domain.Ticket) TicketWriteResult {
	return TicketWriteResult{Ref: t.Ref, Status: string(t.Status), Priority: string(t.Priority), Version: t.Version, UpdatedAt: t.UpdatedAt}
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
}

// AssociationsOutput is ticket_associations' output — a bare list of
// the other entity's references, mirroring apiclient.AssociationsPage/
// cmd/tickets' `ticket associations` table exactly (product spec §5.7:
// an association carries no type or version of its own to echo back).
type AssociationsOutput struct {
	Associated []string `json:"associated"`
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
// version, updated_at); a content item has no status field to echo.
type ContentItemWriteResult struct {
	Ref       string    `json:"ref"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toContentItemWriteResult(c domain.ContentItem) ContentItemWriteResult {
	return ContentItemWriteResult{Ref: c.Ref, Version: c.Version, UpdatedAt: c.UpdatedAt}
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
		Version: c.Version, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
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
	return RecordWriteResult{Ref: c.Ref, Kind: kind, Version: c.Version, UpdatedAt: c.UpdatedAt}
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
