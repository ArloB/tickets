package apiclient

import "time"

// Project, Ticket, and CreateTicketRequest below are apiclient's own
// response/request shapes, matching api/openapi.yaml's Project/Ticket
// schemas and internal/httpapi/wire.go's projectDetail/ticketDetail
// DTOs field-for-field — not internal/domain's Project/Ticket structs.
// Reusing the domain structs here was the bug this package fixes: they
// carry fields (Project.UUID, Ticket.DeletedAt) the wire never sends
// and can drift from the actual wire shape for reasons that have
// nothing to do with the HTTP contract (e.g. a domain field added for
// a store/service-only reason). A client package should assume only
// what the server actually promises to send, per api/openapi.yaml —
// not the server's internal representation.

// Project is GET /projects/{key} and POST /projects' response shape.
type Project struct {
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Creator     *string   `json:"creator,omitempty"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Ticket is every ticket-returning endpoint's response shape that this
// client uses (GET /tickets/{ref}, POST /projects/{key}/tickets).
// Assignee/Creator are the wire's "kind:name" strings, not a
// domain.ActorRef — decoding that string into an ActorRef, if a caller
// wants one, is the caller's job (see internal/mcpsrv/httpbackend.go).
// No comments/relationships fields: this client never sends
// ?include=, since no MCP tool today (RegisterTools, tools.go) takes
// an include parameter — add them here when a tool does, not before.
type Ticket struct {
	Ref         string     `json:"ref"`
	Project     string     `json:"project"`
	Feature     string     `json:"feature"`
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	Severity    *string    `json:"severity,omitempty"`
	Assignee    *string    `json:"assignee,omitempty"`
	Creator     *string    `json:"creator,omitempty"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// ProjectCompact is one row of GET /projects — deliberately not
// Project: a list response must never carry Description (product spec
// §7.2/§11's "list and search omit full bodies"), so this is a
// distinct, narrower wire shape, matching internal/httpapi/wire.go's
// projectCompact field-for-field.
type ProjectCompact struct {
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectsPage is GET /projects' response envelope.
type ProjectsPage struct {
	Projects   []ProjectCompact `json:"projects"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// TicketCompact is one row of GET /projects/{key}/tickets — see
// ProjectCompact's doc comment for why this isn't just Ticket.
// Matches internal/httpapi/wire.go's ticketCompact field-for-field.
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

// TicketsPage is GET /projects/{key}/tickets' response envelope.
type TicketsPage struct {
	Tickets    []TicketCompact `json:"tickets"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// CreateTicketRequest is POST /projects/{key}/tickets' request body.
// Description/Priority have no omitempty: CreateTicket's caller always
// sends both, even empty, matching the server's CreateTicketRequest
// contract (priority defaults server-side when empty, description is
// simply allowed to be blank). Severity is the one optional field.
// Feature/General are mutually exclusive and exactly one is required —
// the server rejects a request with neither or both set.
type CreateTicketRequest struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Severity    string `json:"severity,omitempty"`
	Feature     string `json:"feature,omitempty"`
	General     bool   `json:"general,omitempty"`
}
