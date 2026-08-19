package domain

import "time"

// These are pure value types shared by internal/store, internal/service,
// internal/httpapi, and internal/mcpsrv. They hold no behavior beyond
// what's on this package already (enums, references) and do no I/O.
//
// Phase 0's vertical slice deliberately omits creator/assignee fields:
// there are no authenticated actors until ADR 0004 lands in Phase 2
// (see docs/contracts/representations.md's Phase-0-reduced shape).
//
// UUID fields are tagged json:"-": ADR 0002 makes the UUID the
// canonical identity references resolve to, but the wire shape only
// ever exposes the formatted reference (or, for a project, its Key) —
// matching docs/contracts/representations.md's ticket example, which
// has no raw uuid field. The Go field stays for internal/store and
// future use; nothing serializes it today.

// Project is the top-level container (product spec §5.3). JSON tags
// double as the HTTP wire shape (internal/httpapi) and the idempotency
// result-cache encoding (internal/service) — one shape, not two.
type Project struct {
	UUID        string        `json:"-"`
	Key         string        `json:"key"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Status      ProjectStatus `json:"status"`
	Version     int64         `json:"version"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// Feature is a short/medium-term outcome containing tickets (§5.4).
// Every project has exactly one system-created General feature (ADR
// 0001); Feature has no dedicated HTTP endpoint in Phase 0's slice —
// it surfaces only as a reference on Ticket — but the type is shared
// internally between store and service.
type Feature struct {
	UUID      string         `json:"-"`
	Ref       string         `json:"ref"`
	Title     string         `json:"title"`
	Status    WorkflowStatus `json:"status"`
	Version   int64          `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Ticket is the base unit of actionable work (§5.5).
type Ticket struct {
	UUID        string         `json:"-"`
	Ref         string         `json:"ref"`
	ProjectKey  string         `json:"project"`
	FeatureRef  string         `json:"feature"`
	Type        TicketType     `json:"type"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      WorkflowStatus `json:"status"`
	Priority    Priority       `json:"priority"`
	Severity    *Severity      `json:"severity,omitempty"`
	Version     int64          `json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}
