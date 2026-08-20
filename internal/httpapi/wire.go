package httpapi

import (
	"time"

	"github.com/ArloB/tickets/internal/domain"
)

// This file is Step 5's decoupling point: internal/service's domain
// structs (domain.Project, domain.Ticket) are no longer written
// directly to the wire. Phase 1 added fields to those structs
// (Ticket.Assignee, both types' DeletedAt) that stay nil on every
// response reachable today only because no endpoint sets them yet —
// that's an accident of what's wired up, not a contract. Writing
// domain.Ticket to the wire directly means the contract is protected
// by the *absence* of an assign/soft-delete endpoint, not by the type
// system; the moment Phase 2 adds one, the same handler would start
// leaking a field api/openapi.yaml's additionalProperties: false
// schemas don't know about. These DTOs and their to*() mappers name
// the Phase 0/1 wire field set explicitly, so a new domain.Ticket
// field requires a deliberate edit here before it can reach a client.

// projectDetail is GET /projects/{key} and POST /projects' response
// shape — every field api/openapi.yaml's Project schema declares,
// nothing else.
type projectDetail struct {
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toProjectDetail(p domain.Project) projectDetail {
	return projectDetail{
		Key:         p.Key,
		Title:       p.Title,
		Description: p.Description,
		Status:      string(p.Status),
		Version:     p.Version,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// projectCompact is GET /projects' list-item shape — the plan's Step
// 5 change to api/openapi.yaml: ProjectsPage.projects now references
// a ProjectCompact schema instead of the full Project detail schema,
// finally giving representations.md's compact/detail split a real
// implementation for projects (docs/contracts/representations.md).
// No description, no created_at — the same "small enough for a
// context-budget-conscious list" reasoning that doc's ticket compact
// example applies.
type projectCompact struct {
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toProjectCompact(p domain.Project) projectCompact {
	return projectCompact{
		Key:       p.Key,
		Title:     p.Title,
		Status:    string(p.Status),
		Version:   p.Version,
		UpdatedAt: p.UpdatedAt,
	}
}

// ticketDetail is every ticket-returning endpoint's response shape —
// every field api/openapi.yaml's Ticket schema declares, nothing
// else. Creator and Assignee join as of Step 13: Creator was added to
// domain.Ticket in Step 9 for the store/service layers only, deferred
// to here per this file's original doc; Assignee joins now because
// Step 13 is what adds the assign mutation route — a response DTO
// that could never show the assignee an assign call just set would be
// a real usability gap, not a deliberate omission like the others. No
// deleted_at: unlike Comment's visible tombstone (§5.10), a soft-
// deleted ticket is invisible to every normal read path (ADR 0013),
// so no route that returns a ticketDetail can ever populate it.
type ticketDetail struct {
	Ref         string    `json:"ref"`
	Project     string    `json:"project"`
	Feature     string    `json:"feature"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Severity    *string   `json:"severity,omitempty"`
	Assignee    *string   `json:"assignee,omitempty"`
	Creator     *string   `json:"creator,omitempty"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Comments/Relationships are nil (omitted) unless the caller asked
	// for them via ?include=comments,relationships (representation.go)
	// — docs/contracts/representations.md's "expands a detail response
	// with sub-resources that are otherwise summarized... rather than
	// embedded." A pointer-to-slice, not a bare slice, so "explicitly
	// asked for zero results" (empty slice) is distinguishable from
	// "didn't ask" (nil) in the marshaled JSON.
	Comments      *[]commentDetail    `json:"comments,omitempty"`
	Relationships *[]relationshipView `json:"relationships,omitempty"`
}

func toTicketDetail(t domain.Ticket) ticketDetail {
	var severity *string
	if t.Severity != nil {
		v := string(*t.Severity)
		severity = &v
	}
	var assignee *string
	if t.Assignee != nil {
		v := t.Assignee.String()
		assignee = &v
	}
	var creator *string
	if t.Creator != nil {
		v := t.Creator.String()
		creator = &v
	}
	return ticketDetail{
		Ref:         t.Ref,
		Project:     t.ProjectKey,
		Feature:     t.FeatureRef,
		Type:        string(t.Type),
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Priority:    string(t.Priority),
		Severity:    severity,
		Assignee:    assignee,
		Creator:     creator,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// ticketCompact is GET /projects/{key}/tickets' list-item shape,
// matching docs/contracts/representations.md's documented example
// exactly: no project/feature/description/creator/assignee — just
// enough to render a list row (product spec §7.2's context-budget
// reasoning, the same one projectCompact/featureCompact apply).
type ticketCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Severity  *string   `json:"severity,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version"`
}

func toTicketCompact(t domain.Ticket) ticketCompact {
	var severity *string
	if t.Severity != nil {
		v := string(*t.Severity)
		severity = &v
	}
	return ticketCompact{
		Ref: t.Ref, Title: t.Title, Type: string(t.Type), Status: string(t.Status),
		Priority: string(t.Priority), Severity: severity, UpdatedAt: t.UpdatedAt, Version: t.Version,
	}
}

type ticketsPage struct {
	Tickets    []ticketCompact `json:"tickets"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// featureDetail is every feature-returning endpoint's response shape —
// same "explicit field list, no deleted_at" contract as ticketDetail
// (see this file's top doc). Unlike ticketDetail, no creator field
// yet: domain.Feature gained Creator in Step 9 for the store/service
// layers, but no caller has needed it on the wire — add it here,
// deliberately, when one does. No assignee at all: features are never
// assigned (product spec §5.4), so domain.Feature has no such field to
// omit in the first place.
type featureDetail struct {
	Ref         string    `json:"ref"`
	Project     string    `json:"project"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toFeatureDetail(f domain.Feature) featureDetail {
	return featureDetail{
		Ref:         f.Ref,
		Project:     f.ProjectKey,
		Title:       f.Title,
		Description: f.Description,
		Status:      string(f.Status),
		Priority:    string(f.Priority),
		Version:     f.Version,
		CreatedAt:   f.CreatedAt,
		UpdatedAt:   f.UpdatedAt,
	}
}

// featureCompact is GET /projects/{key}/features' list-item shape —
// the same compact/detail split projectCompact/ticketCompact use. No
// description: a project's feature list is meant to be skimmed
// (product spec §5.4), not read in full, the same "small enough for a
// context-budget-conscious list" reasoning as projectCompact.
type featureCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toFeatureCompact(f domain.Feature) featureCompact {
	return featureCompact{
		Ref:       f.Ref,
		Title:     f.Title,
		Status:    string(f.Status),
		Priority:  string(f.Priority),
		Version:   f.Version,
		UpdatedAt: f.UpdatedAt,
	}
}

type featuresPage struct {
	Features []featureCompact `json:"features"`
}

// commentDetail is every comment-returning endpoint's response shape.
// Unlike ticketDetail/featureDetail, this mirrors domain.Comment's
// fields directly rather than hiding some of them — DeletedAt is
// meant to reach the wire (§5.10's "visible tombstone": Body stays
// intact in storage, and DeletedAt being set is what a caller checks
// to render a tombstone instead of the content, per domain.Comment's
// own doc comment). Comments have no Creator field at all (they use
// Author instead, already always populated — there was never a
// placeholder actor for comments to retrofit).
type commentDetail struct {
	ID        int64      `json:"id"`
	Author    string     `json:"author"`
	Body      string     `json:"body"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

func toCommentDetail(c domain.Comment) commentDetail {
	return commentDetail{
		ID: c.ID, Author: c.Author.String(), Body: c.Body,
		Version: c.Version, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, DeletedAt: c.DeletedAt,
	}
}

type commentsPage struct {
	Comments []commentDetail `json:"comments"`
}

// commentVersionEntry is one entry in a comment's edit history — an
// EditedBy version author, not a Creator (domain.CommentVersion has no
// such field; the comment as a whole has no separate creation event to
// carry one).
type commentVersionEntry struct {
	Version   int64     `json:"version"`
	Body      string    `json:"body"`
	EditedBy  string    `json:"edited_by"`
	CreatedAt time.Time `json:"created_at"`
}

func toCommentVersionEntry(v domain.CommentVersion) commentVersionEntry {
	return commentVersionEntry{Version: v.Version, Body: v.Body, EditedBy: v.EditedBy.String(), CreatedAt: v.CreatedAt}
}

type commentHistoryPage struct {
	Versions []commentVersionEntry `json:"versions"`
}

// relationshipView is one relationship edge as seen from the ticket
// the caller asked about — Type is already resolved to that ticket's
// perspective (service.RelationshipView's own doc).
type relationshipView struct {
	Type  string `json:"type"`
	Other string `json:"other"`
}

type relationshipsPage struct {
	Relationships []relationshipView `json:"relationships"`
}

// associationsPage is every entity associated with the one the caller
// asked about — bare formatted references (ticket or feature), no
// detail: a caller that wants more than the ref fetches it separately
// via GET /tickets/{ref} or GET /features/{ref}.
type associationsPage struct {
	Associated []string `json:"associated"`
}

// deleteResponse is every soft-delete endpoint's response shape
// (tickets and features both): the entity's new version, so a caller
// can construct a subsequent restore call's If-Match without a second
// read. This closes the gap ADR 0013's Consequences section flagged —
// "restore is undiscoverable through the current API surface" — now
// that Step 9 made DeleteTicket/DeleteFeature return the version
// store.SoftDeleteEntity already computed. 200 OK with a small body,
// not 204 No Content, following the precedent
// DELETE /agents/{name}/tokens/{id} already set (admin.go) for a
// delete response carrying anything the caller needs next.
type deleteResponse struct {
	Version int64 `json:"version"`
}
