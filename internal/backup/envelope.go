package backup

// Envelope is `tickets export`'s versioned JSON document (product spec
// §12, plan.md's Phase 6 Step 4): every non-secret domain table, in
// insertion order (each slice only ever references ids in tables
// listed before it). Row structs mirror their table's columns exactly,
// preserving internal surrogate ids (entities.id, actors.id, ...) so
// import can insert them verbatim rather than building an old-to-new
// id remapper — Import refuses a non-empty target database rather
// than attempt id remapping (see import.go's doc comment).
//
// Deliberately excluded, and never selected by Export even internally:
// human_accounts.password_hash, sessions, agent_tokens.token_hash, and
// login_attempts — product spec §10's "never export secrets or
// credentials." idempotency_keys is also excluded: it is a bounded-
// retention cache, not domain data, and its stored ref_key values
// carry no independent meaning once the rows they point at are
// re-inserted with the same ids anyway. FTS5 search tables are
// excluded too — Import rebuilds the search index from the imported
// rows (store.RebuildSearchIndex) rather than trying to serialize a
// virtual table.
type Envelope struct {
	FormatVersion int    `json:"format_version"`
	SchemaVersion int    `json:"schema_version"`
	ServerVersion string `json:"server_version"`
	ExportedAt    string `json:"exported_at"`

	Entities            []EntityRow            `json:"entities"`
	Actors              []ActorRow             `json:"actors"`
	Projects            []ProjectRow           `json:"projects"`
	Features            []FeatureRow           `json:"features"`
	Tickets             []TicketRow            `json:"tickets"`
	ReferenceCounters   []ReferenceCounterRow  `json:"reference_counters"`
	Decisions           []DecisionRow          `json:"decisions"`
	DecisionVersions    []DecisionVersionRow   `json:"decision_versions"`
	ContentItems        []ContentItemRow       `json:"content_items"`
	ContentVersions     []ContentVersionRow    `json:"content_versions"`
	Comments            []CommentRow           `json:"comments"`
	CommentVersions     []CommentVersionRow    `json:"comment_versions"`
	Attachments         []AttachmentRow        `json:"attachments"`
	AttachmentVersions  []AttachmentVersionRow `json:"attachment_versions"`
	TicketRelationships []RelationshipRow      `json:"ticket_relationships"`
	EntityAssociations  []AssociationRow       `json:"entity_associations"`
	DerivedMentions     []DerivedMentionRow    `json:"derived_mentions"`
	ActorMentions       []ActorMentionRow      `json:"actor_mentions"`
	ExternalLinks       []ExternalLinkRow      `json:"external_links"`
	AuditEvents         []AuditEventRow        `json:"audit_events"`
	Subscriptions       []SubscriptionRow      `json:"subscriptions"`
	Notifications       []NotificationRow      `json:"notifications"`
}

type EntityRow struct {
	ID        int64   `json:"id"`
	UUID      string  `json:"uuid"`
	ProjectID *int64  `json:"project_id"`
	Kind      string  `json:"kind"`
	Version   int64   `json:"version"`
	CreatedBy *int64  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at"`
}

// ActorRow carries only actors' own columns — never human_accounts or
// agent_tokens (this package's doc comment). An imported agent actor
// therefore has an identity (for attribution: comment authors,
// created_by, audit trail) but no working token; an imported human
// actor has an identity but no password, and cannot log in until an
// admin sets one up some other way. Neither limitation is worked
// around here — see docs/backup-recovery.md.
type ActorRow struct {
	ID          int64   `json:"id"`
	UUID        string  `json:"uuid"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	OwnerID     *int64  `json:"owner_id"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	DeletedAt   *string `json:"deleted_at"`
}

type ProjectRow struct {
	ID               int64  `json:"id"`
	Key              string `json:"key"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Status           string `json:"status"`
	GeneralFeatureID *int64 `json:"general_feature_id"`
}

type FeatureRow struct {
	ID           int64  `json:"id"`
	ProjectID    int64  `json:"project_id"`
	Seq          int64  `json:"seq"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Description  string `json:"description"`
	Priority     string `json:"priority"`
	Position     int64  `json:"position"`
	PriorityRank int64  `json:"priority_rank"`
}

type TicketRow struct {
	ID           int64   `json:"id"`
	ProjectID    int64   `json:"project_id"`
	FeatureID    int64   `json:"feature_id"`
	Seq          int64   `json:"seq"`
	Type         string  `json:"type"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Status       string  `json:"status"`
	Priority     string  `json:"priority"`
	Severity     *string `json:"severity"`
	AssigneeID   *int64  `json:"assignee_id"`
	Position     int64   `json:"position"`
	PriorityRank int64   `json:"priority_rank"`
	SeverityRank int64   `json:"severity_rank"`
}

// ReferenceCounterRow preserves ADR 0009's per-(project,kind) sequence
// counters — without these, the next ticket/feature/decision/etc.
// created after import would collide with an imported reference.
type ReferenceCounterRow struct {
	ProjectID int64  `json:"project_id"`
	Kind      string `json:"kind"`
	NextSeq   int64  `json:"next_seq"`
}

type DecisionRow struct {
	ID           int64  `json:"id"`
	ProjectID    int64  `json:"project_id"`
	Seq          int64  `json:"seq"`
	Title        string `json:"title"`
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Rationale    string `json:"rationale"`
	Consequences string `json:"consequences"`
	Status       string `json:"status"`
	SupersededBy *int64 `json:"superseded_by"`
}

type DecisionVersionRow struct {
	ID           int64  `json:"id"`
	DecisionID   int64  `json:"decision_id"`
	Version      int64  `json:"version"`
	Title        string `json:"title"`
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Rationale    string `json:"rationale"`
	Consequences string `json:"consequences"`
	Status       string `json:"status"`
	EditedBy     int64  `json:"edited_by"`
	CreatedAt    string `json:"created_at"`
}

type ContentItemRow struct {
	ID             int64   `json:"id"`
	ProjectID      int64   `json:"project_id"`
	Kind           string  `json:"kind"`
	Seq            int64   `json:"seq"`
	Title          string  `json:"title"`
	Representation string  `json:"representation"`
	Body           string  `json:"body"`
	FileHash       *string `json:"file_hash"`
	FileName       *string `json:"file_name"`
	FileSize       *int64  `json:"file_size"`
	MediaType      *string `json:"media_type"`
	Checksum       *string `json:"checksum"`
	PathValue      *string `json:"path_value"`
	URLValue       *string `json:"url_value"`
	Status         string  `json:"status"`
}

type ContentVersionRow struct {
	ID             int64   `json:"id"`
	ContentItemID  int64   `json:"content_item_id"`
	Version        int64   `json:"version"`
	Representation string  `json:"representation"`
	Title          string  `json:"title"`
	Body           *string `json:"body"`
	FileHash       *string `json:"file_hash"`
	FileName       *string `json:"file_name"`
	FileSize       *int64  `json:"file_size"`
	MediaType      *string `json:"media_type"`
	Checksum       *string `json:"checksum"`
	PathValue      *string `json:"path_value"`
	URLValue       *string `json:"url_value"`
	EditedBy       int64   `json:"edited_by"`
	CreatedAt      string  `json:"created_at"`
}

type CommentRow struct {
	ID        int64   `json:"id"`
	EntityID  int64   `json:"entity_id"`
	AuthorID  int64   `json:"author_id"`
	Body      string  `json:"body"`
	Version   int64   `json:"version"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at"`
}

type CommentVersionRow struct {
	ID        int64  `json:"id"`
	CommentID int64  `json:"comment_id"`
	Version   int64  `json:"version"`
	Body      string `json:"body"`
	EditedBy  int64  `json:"edited_by"`
	CreatedAt string `json:"created_at"`
}

type AttachmentRow struct {
	ID             int64   `json:"id"`
	EntityID       *int64  `json:"entity_id"`
	CommentID      *int64  `json:"comment_id"`
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	CurrentVersion int64   `json:"current_version"`
	FileHash       *string `json:"file_hash"`
	FileName       *string `json:"file_name"`
	FileSize       *int64  `json:"file_size"`
	MediaType      *string `json:"media_type"`
	Checksum       *string `json:"checksum"`
	PathValue      *string `json:"path_value"`
	CreatedAt      string  `json:"created_at"`
	CreatedBy      int64   `json:"created_by"`
	DeletedAt      *string `json:"deleted_at"`
}

type AttachmentVersionRow struct {
	ID           int64   `json:"id"`
	AttachmentID int64   `json:"attachment_id"`
	Version      int64   `json:"version"`
	Kind         string  `json:"kind"`
	FileHash     *string `json:"file_hash"`
	FileName     *string `json:"file_name"`
	FileSize     *int64  `json:"file_size"`
	MediaType    *string `json:"media_type"`
	Checksum     *string `json:"checksum"`
	PathValue    *string `json:"path_value"`
	UploadedBy   int64   `json:"uploaded_by"`
	CreatedAt    string  `json:"created_at"`
}

type RelationshipRow struct {
	SourceID  int64  `json:"source_id"`
	TargetID  int64  `json:"target_id"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	CreatedBy int64  `json:"created_by"`
}

type AssociationRow struct {
	SourceID  int64  `json:"source_id"`
	TargetID  int64  `json:"target_id"`
	CreatedAt string `json:"created_at"`
	CreatedBy int64  `json:"created_by"`
}

type DerivedMentionRow struct {
	SourceEntityID  int64  `json:"source_entity_id"`
	SourceCommentID int64  `json:"source_comment_id"`
	TargetEntityID  int64  `json:"target_entity_id"`
	CreatedAt       string `json:"created_at"`
}

type ActorMentionRow struct {
	SourceEntityID  int64  `json:"source_entity_id"`
	SourceCommentID int64  `json:"source_comment_id"`
	ActorID         int64  `json:"actor_id"`
	CreatedAt       string `json:"created_at"`
}

type ExternalLinkRow struct {
	ID        int64  `json:"id"`
	EntityID  int64  `json:"entity_id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	CreatedBy int64  `json:"created_by"`
}

// AuditEventRow's Changes is always JSON already (audit_events.changes
// is TEXT storing serialized JSON) but travels as an opaque string
// here rather than json.RawMessage — this envelope's own encoding
// doesn't need to parse it, only round-trip it byte-for-byte.
type AuditEventRow struct {
	ID            int64  `json:"id"`
	EntityID      *int64 `json:"entity_id"`
	TargetActorID *int64 `json:"target_actor_id"`
	ActorID       int64  `json:"actor_id"`
	EventType     string `json:"event_type"`
	CommentID     *int64 `json:"comment_id"`
	CorrelationID string `json:"correlation_id"`
	Changes       string `json:"changes"`
	CreatedAt     string `json:"created_at"`
}

type SubscriptionRow struct {
	EntityID  int64  `json:"entity_id"`
	ActorID   int64  `json:"actor_id"`
	CreatedAt string `json:"created_at"`
}

// referencedBlobHashes collects every distinct file_hash the envelope's
// own rows reference — attachments/attachment_versions and
// content_items/content_versions (a plan/document stored as an
// uploaded file, not just Markdown, per docs/adr/0017-content-items.md).
// Export uses this to know what to copy into --attachments DIR; Import
// uses it to know what must already be there before a commit that
// carries upload-kind content can be trusted.
func referencedBlobHashes(env Envelope) map[string]bool {
	hashes := make(map[string]bool)
	add := func(h *string) {
		if h != nil && *h != "" {
			hashes[*h] = true
		}
	}
	for _, r := range env.Attachments {
		add(r.FileHash)
	}
	for _, r := range env.AttachmentVersions {
		add(r.FileHash)
	}
	for _, r := range env.ContentItems {
		add(r.FileHash)
	}
	for _, r := range env.ContentVersions {
		add(r.FileHash)
	}
	return hashes
}

type NotificationRow struct {
	ID          int64   `json:"id"`
	ActorID     int64   `json:"actor_id"`
	Kind        string  `json:"kind"`
	EntityID    int64   `json:"entity_id"`
	CommentID   *int64  `json:"comment_id"`
	TriggeredBy *int64  `json:"triggered_by"`
	CreatedAt   string  `json:"created_at"`
	ReadAt      *string `json:"read_at"`
}
