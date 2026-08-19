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
// else. In particular: no assignee, no deleted_at, even though
// domain.Ticket carries both as of Phase 1 (see this file's doc).
// There is no ticket list endpoint in Phase 0/1, so no ticketCompact
// exists yet — build it alongside whichever phase adds one.
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
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toTicketDetail(t domain.Ticket) ticketDetail {
	var severity *string
	if t.Severity != nil {
		v := string(*t.Severity)
		severity = &v
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
		Version:     t.Version,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}
