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
// 0001); Feature has no dedicated HTTP endpoint in Phase 1 either
// (the plan keeps Phase 1 below the API line) — it surfaces only as a
// reference on Ticket over HTTP, but the full type is shared
// internally between store and service, and by service-level tests.
type Feature struct {
	UUID        string         `json:"-"`
	Ref         string         `json:"ref"`
	ProjectKey  string         `json:"project"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      WorkflowStatus `json:"status"`
	Priority    Priority       `json:"priority"`
	Version     int64          `json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	// DeletedAt is nil on every ordinary read (Get/List filter
	// deleted_at IS NULL, ADR 0013) — it is only ever non-nil on the
	// result of a lookup made specifically to check deletion state
	// (internal/store's *ByRefAnyDeletion functions), which Restore
	// uses since a soft-deleted record is otherwise invisible to the
	// normal Get path.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// Ticket is the base unit of actionable work (§5.5). Assignee is nil
// until Phase 1's AssignTicket is called — every existing Phase 0
// response keeps its exact shape (omitempty), so this addition does
// not touch the OpenAPI contract for the endpoints that already exist.
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
	Assignee    *ActorRef      `json:"assignee,omitempty"`
	Version     int64          `json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	// DeletedAt: see Feature.DeletedAt's doc — same contract.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// Comment is a Markdown note attached to a principal entity (§5.10).
// Unlike Project/Feature/Ticket it has no public reference — migration
// 0002_core_domain.sql's comment explains why comments keep a
// dedicated INTEGER PRIMARY KEY instead of joining the entities
// registry (Phase 5's FTS5 content_rowid). ID is that primary key,
// exposed directly: ADR 0002's "no bare integer id" rule guards
// entities.id, a surrogate that always has a real ref/uuid hiding
// behind it — a comment has neither, so its id is the actual public
// identity here, not a leaked internal detail.
//
// Deletion is a soft-delete with a visible tombstone (§5.10): Body
// stays intact in storage (the audit trail wants it), and DeletedAt
// being set is what a caller checks to render the tombstone instead
// of the content.
type Comment struct {
	ID        int64      `json:"id"`
	Author    ActorRef   `json:"author"`
	Body      string     `json:"body"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// CommentVersion is one archived prior body of a Comment (§5.10:
// "comment edits create versions"). EditedBy is the actor who made
// the edit that superseded this body — not necessarily who originally
// wrote it beyond version 1, since nothing but audit_events tracks
// who authored the *current* live body between edits (see
// internal/service/comment.go's doc on this).
type CommentVersion struct {
	Version   int64     `json:"version"`
	Body      string    `json:"body"`
	EditedBy  ActorRef  `json:"edited_by"`
	CreatedAt time.Time `json:"created_at"`
}
