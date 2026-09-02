package backup

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/store"
	"github.com/google/uuid"
)

// seedActorID/seedActorKind/seedActorName describe the two actors
// migration 0002_core_domain.sql seeds into every database at id 1
// and id 2 — deterministic across every fresh install (fixed INSERT
// statement, AUTOINCREMENT starting at 1), except for uuid, which is
// randomblob(16) and therefore different in every database. Import
// relies on this determinism: since the target database Import writes
// into always already has these two rows (store.Open runs the
// migration before Import ever sees the connection), inserting the
// envelope's own copies of them would collide on primary key. Import
// skips them instead — every other row's actor_id/created_by/etc.
// referencing id 1 or id 2 resolves correctly regardless, since
// nothing in the FK graph is keyed by uuid.
var seedActors = map[int64]struct{ Kind, Name string }{
	1: {"system", "system"},
	2: {"human", "local"},
}

// ImportReport is `tickets import`'s output, in both --dry-run (the
// default) and --commit mode: what would be inserted (or was), and
// any reference problems found. Import refuses to commit if Problems
// is non-empty, regardless of the commit flag — a partial import of
// data that fails its own reference checks is worse than no import.
type ImportReport struct {
	Committed bool           `json:"committed"`
	Counts    map[string]int `json:"counts"`
	Problems  []string       `json:"problems"`
}

const maxReportedProblems = 50

// Import validates env against db (`tickets import`), and — only when
// commit is true and validation finds no problems — inserts every row
// verbatim inside one transaction, then rebuilds the search index
// (store.RebuildSearchIndex) since Export never serializes the FTS5
// virtual tables.
//
// Import requires db to be an empty target: entities has zero rows,
// and actors has exactly the two rows migration 0002_core_domain.sql
// always seeds. Ids throughout env are preserved verbatim rather than
// remapped — this is what makes "empty target" a hard requirement
// rather than a recommendation: inserting a row at an id already
// occupied by unrelated data would either collide (primary key) or
// silently attach imported content to the wrong existing entity.
// Building a full old-to-new id remapper across every table's foreign
// keys was considered and rejected as disproportionate to this
// command's actual use (moving or archiving one server's content into
// a fresh installation, per plan.md's Phase 6 Step 4) — the
// recommended path for merging into a live, non-empty database is a
// fresh `tickets server` pointed at a new data directory, not this
// command.
//
// Imported agent actors arrive tokenless (agent_tokens is never
// exported) and need a newly issued token before they can
// authenticate again; imported human actors likewise arrive without a
// password (human_accounts is never exported). Both are stated in the
// report, not silently left for the operator to discover.
//
// attachmentsDir mirrors Export's own flag: when the envelope
// references any blob (an uploaded attachment or content item), a
// commit is refused unless attachmentsDir is non-empty and actually
// contains every referenced hash — otherwise a committed import would
// leave attachment metadata pointing at bytes that were never brought
// over. dstBlobs is only used when attachmentsDir is non-empty and
// commit succeeds, to copy the verified blobs into the target data
// directory's own blob store.
func Import(ctx context.Context, db *sql.DB, env Envelope, attachmentsDir string, dstBlobs *blobstore.Store, commit bool) (ImportReport, error) {
	report := ImportReport{Counts: map[string]int{}}

	if env.FormatVersion != envelopeFormatVersion {
		report.Problems = append(report.Problems, fmt.Sprintf(
			"export format version %d is not supported by this build (want %d) — re-export with a compatible tickets version",
			env.FormatVersion, envelopeFormatVersion))
		return report, nil
	}

	highest, err := store.HighestEmbeddedMigrationVersion()
	if err != nil {
		return ImportReport{}, fmt.Errorf("import: schema version: %w", err)
	}
	if env.SchemaVersion > highest {
		report.Problems = append(report.Problems, fmt.Sprintf(
			"export schema version %d is newer than this build supports (max %d) — upgrade tickets before importing",
			env.SchemaVersion, highest))
	}

	emptyProblems, err := checkTargetEmpty(ctx, db)
	if err != nil {
		return ImportReport{}, fmt.Errorf("import: check target database: %w", err)
	}
	report.Problems = append(report.Problems, emptyProblems...)
	report.Problems = append(report.Problems, validateReferences(env)...)

	referencedHashes := referencedBlobHashes(env)
	var srcBlobs *blobstore.Store
	if len(referencedHashes) > 0 {
		if attachmentsDir == "" {
			report.Problems = append(report.Problems, fmt.Sprintf(
				"export references %d attachment blob(s) but no --attachments directory was given — "+
					"the imported rows would point at content that was never brought over", len(referencedHashes)))
		} else {
			srcBlobs, err = blobstore.Open(attachmentsDir)
			if err != nil {
				return ImportReport{}, fmt.Errorf("import: open attachments directory: %w", err)
			}
			for hash := range referencedHashes {
				if _, err := srcBlobs.ModTime(hash); err != nil {
					report.Problems = append(report.Problems, fmt.Sprintf("attachments directory is missing blob %s", hash))
				}
			}
		}
	}

	report.Counts["entities"] = len(env.Entities)
	report.Counts["actors"] = len(env.Actors) - len(seedActors)
	if report.Counts["actors"] < 0 {
		report.Counts["actors"] = 0
	}
	report.Counts["projects"] = len(env.Projects)
	report.Counts["features"] = len(env.Features)
	report.Counts["tickets"] = len(env.Tickets)
	report.Counts["decisions"] = len(env.Decisions)
	report.Counts["content_items"] = len(env.ContentItems)
	report.Counts["comments"] = len(env.Comments)
	report.Counts["attachments"] = len(env.Attachments)
	report.Counts["external_links"] = len(env.ExternalLinks)
	report.Counts["notifications"] = len(env.Notifications)

	if len(report.Problems) > maxReportedProblems {
		report.Problems = append(report.Problems[:maxReportedProblems],
			fmt.Sprintf("...and %d more", len(report.Problems)-maxReportedProblems))
	}

	if !commit || len(report.Problems) > 0 {
		return report, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ImportReport{}, fmt.Errorf("import: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := insertAll(ctx, tx, env); err != nil {
		return ImportReport{}, fmt.Errorf("import: %w", err)
	}
	if _, err := store.RebuildSearchIndex(ctx, tx); err != nil {
		return ImportReport{}, fmt.Errorf("import: rebuild search index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ImportReport{}, fmt.Errorf("import: commit: %w", err)
	}
	committed = true

	// Blobs are copied only after the database transaction commits:
	// the rows referencing them are the source of truth for what
	// "imported successfully" means, and a blob copy failure after
	// that point is reported as an error rather than silently
	// unwound (blobstore.Store.Put's content-addressing makes a
	// re-run safe — an already-present blob is simply skipped).
	if srcBlobs != nil {
		for hash := range referencedHashes {
			if _, err := copyBlob(srcBlobs, dstBlobs, hash); err != nil {
				return ImportReport{}, fmt.Errorf("import: copy attachment blobs: %w", err)
			}
		}
	}

	report.Committed = true
	return report, nil
}

func checkTargetEmpty(ctx context.Context, db *sql.DB) ([]string, error) {
	var problems []string

	var entityCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities`).Scan(&entityCount); err != nil {
		return nil, err
	}
	if entityCount > 0 {
		problems = append(problems, fmt.Sprintf(
			"target database is not empty: %d existing entities row(s) — import only supports a fresh data directory", entityCount))
	}

	rows, err := db.QueryContext(ctx, `SELECT id, kind, name FROM actors ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var extraActors int
	for rows.Next() {
		var id int64
		var kind, name string
		if err := rows.Scan(&id, &kind, &name); err != nil {
			return nil, err
		}
		seed, isSeed := seedActors[id]
		if !isSeed || seed.Kind != kind || seed.Name != name {
			extraActors++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if extraActors > 0 {
		problems = append(problems, fmt.Sprintf(
			"target database is not empty: %d actor(s) beyond the standard seeded system/local pair — import only supports a fresh data directory", extraActors))
	}
	return problems, nil
}

// validateReferences checks every foreign-key-shaped field in env
// resolves to an id present elsewhere in env — the "reference
// collisions and invalid relationships" plan.md's Step 4 asks a dry
// run to name. It is not exhaustive (uniqueness constraints, enum
// validity, and relationship-cycle detection are left to the database
// and internal/service's own rules, exercised for real only once a
// commit is attempted): the goal is to catch a hand-edited or
// corrupted envelope before it reaches the database, not to
// re-implement every validation internal/service already owns.
func validateReferences(env Envelope) []string {
	var problems []string

	// insertAll skips any envelope actor at id 1 or id 2 unconditionally
	// (seedActors' own doc comment) — checked here, not there, so a
	// hand-edited or corrupted envelope that put a real actor at one of
	// those ids is refused with a named problem instead of silently
	// dropped, which would misattribute every row that actor's id
	// touches (author_id, created_by, actor_id, ...) onto the target's
	// unrelated system/local actor.
	for _, a := range env.Actors {
		seed, isSeed := seedActors[a.ID]
		if isSeed && (seed.Kind != a.Kind || seed.Name != a.Name) {
			problems = append(problems, fmt.Sprintf(
				"actors id=%d is expected to be the seeded %s/%s actor but the export has %s/%s — refusing to silently drop it",
				a.ID, seed.Kind, seed.Name, a.Kind, a.Name))
		}
	}

	entityIDs := make(map[int64]bool, len(env.Entities))
	for _, e := range env.Entities {
		if entityIDs[e.ID] {
			problems = append(problems, fmt.Sprintf("duplicate entities.id=%d in export", e.ID))
		}
		entityIDs[e.ID] = true
	}
	actorIDs := make(map[int64]bool, len(env.Actors))
	for _, a := range env.Actors {
		if actorIDs[a.ID] {
			problems = append(problems, fmt.Sprintf("duplicate actors.id=%d in export", a.ID))
		}
		actorIDs[a.ID] = true
	}
	commentIDs := make(map[int64]bool, len(env.Comments))
	for _, c := range env.Comments {
		commentIDs[c.ID] = true
	}
	attachmentIDs := make(map[int64]bool, len(env.Attachments))
	for _, a := range env.Attachments {
		attachmentIDs[a.ID] = true
	}

	entity := func(table string, id int64) {
		if !entityIDs[id] {
			problems = append(problems, fmt.Sprintf("%s references entity id=%d, not present in export", table, id))
		}
	}
	entityPtr := func(table string, id *int64) {
		if id != nil {
			entity(table, *id)
		}
	}
	actor := func(table string, id int64) {
		if !actorIDs[id] {
			problems = append(problems, fmt.Sprintf("%s references actor id=%d, not present in export", table, id))
		}
	}
	actorPtr := func(table string, id *int64) {
		if id != nil {
			actor(table, *id)
		}
	}
	commentPtr := func(table string, id *int64) {
		if id != nil && !commentIDs[*id] {
			problems = append(problems, fmt.Sprintf("%s references comment id=%d, not present in export", table, *id))
		}
	}

	for _, r := range env.Entities {
		entityPtr("entities", r.ProjectID)
		actorPtr("entities", r.CreatedBy)
	}
	for _, r := range env.Projects {
		entityPtr("projects", r.GeneralFeatureID)
	}
	for _, r := range env.Features {
		entity("features", r.ProjectID)
	}
	for _, r := range env.Tickets {
		entity("tickets", r.ProjectID)
		entity("tickets", r.FeatureID)
		actorPtr("tickets", r.AssigneeID)
	}
	for _, r := range env.Decisions {
		entity("decisions", r.ProjectID)
		entityPtr("decisions", r.SupersededBy)
	}
	for _, r := range env.DecisionVersions {
		entity("decision_versions", r.DecisionID)
		actor("decision_versions", r.EditedBy)
	}
	for _, r := range env.ContentItems {
		entity("content_items", r.ProjectID)
	}
	for _, r := range env.ContentVersions {
		entity("content_versions", r.ContentItemID)
		actor("content_versions", r.EditedBy)
	}
	for _, r := range env.Comments {
		entity("comments", r.EntityID)
		actor("comments", r.AuthorID)
	}
	for _, r := range env.CommentVersions {
		if !commentIDs[r.CommentID] {
			problems = append(problems, fmt.Sprintf("comment_versions references comment id=%d, not present in export", r.CommentID))
		}
		actor("comment_versions", r.EditedBy)
	}
	for _, r := range env.Attachments {
		entityPtr("attachments", r.EntityID)
		commentPtr("attachments", r.CommentID)
		actor("attachments", r.CreatedBy)
	}
	for _, r := range env.AttachmentVersions {
		if !attachmentIDs[r.AttachmentID] {
			problems = append(problems, fmt.Sprintf("attachment_versions references attachment id=%d, not present in export", r.AttachmentID))
		}
		actor("attachment_versions", r.UploadedBy)
	}
	for _, r := range env.TicketRelationships {
		entity("ticket_relationships", r.SourceID)
		entity("ticket_relationships", r.TargetID)
		actor("ticket_relationships", r.CreatedBy)
	}
	for _, r := range env.EntityAssociations {
		entity("entity_associations", r.SourceID)
		entity("entity_associations", r.TargetID)
		actor("entity_associations", r.CreatedBy)
	}
	for _, r := range env.DerivedMentions {
		entity("derived_mentions", r.SourceEntityID)
		entity("derived_mentions", r.TargetEntityID)
	}
	for _, r := range env.ActorMentions {
		entity("actor_mentions", r.SourceEntityID)
		actor("actor_mentions", r.ActorID)
	}
	for _, r := range env.ExternalLinks {
		entity("external_links", r.EntityID)
		actor("external_links", r.CreatedBy)
	}
	for _, r := range env.AuditEvents {
		entityPtr("audit_events", r.EntityID)
		actorPtr("audit_events", r.TargetActorID)
		actor("audit_events", r.ActorID)
		commentPtr("audit_events", r.CommentID)
	}
	for _, r := range env.Subscriptions {
		entity("subscriptions", r.EntityID)
		actor("subscriptions", r.ActorID)
	}
	for _, r := range env.Notifications {
		entity("notifications", r.EntityID)
		actor("notifications", r.ActorID)
		actorPtr("notifications", r.TriggeredBy)
		commentPtr("notifications", r.CommentID)
	}
	return problems
}

func insertAll(ctx context.Context, tx *sql.Tx, env Envelope) error {
	for _, r := range env.Actors {
		if _, isSeed := seedActors[r.ID]; isSeed {
			continue // already present — see seedActors' doc comment
		}
		u, err := uuid.Parse(r.UUID)
		if err != nil {
			return fmt.Errorf("actors id=%d: parse uuid: %w", r.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO actors(id, uuid, kind, name, owner_id, description, created_at, updated_at, deleted_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, u[:], r.Kind, r.Name, r.OwnerID, r.Description, r.CreatedAt, r.UpdatedAt, r.DeletedAt,
		); err != nil {
			return fmt.Errorf("insert actors id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.Entities {
		u, err := uuid.Parse(r.UUID)
		if err != nil {
			return fmt.Errorf("entities id=%d: parse uuid: %w", r.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entities(id, uuid, project_id, kind, version, created_by, created_at, updated_at, deleted_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, u[:], r.ProjectID, r.Kind, r.Version, r.CreatedBy, r.CreatedAt, r.UpdatedAt, r.DeletedAt,
		); err != nil {
			return fmt.Errorf("insert entities id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.Projects {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO projects(id, key, title, description, status, general_feature_id) VALUES (?, ?, ?, ?, ?, ?)`,
			r.ID, r.Key, r.Title, r.Description, r.Status, r.GeneralFeatureID,
		); err != nil {
			return fmt.Errorf("insert projects id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.Features {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO features(id, project_id, seq, title, status, description, priority, position, priority_rank)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ProjectID, r.Seq, r.Title, r.Status, r.Description, r.Priority, r.Position, r.PriorityRank,
		); err != nil {
			return fmt.Errorf("insert features id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.Tickets {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tickets(id, project_id, feature_id, seq, type, title, description, status, priority,
			                      severity, assignee_id, position, priority_rank, severity_rank)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ProjectID, r.FeatureID, r.Seq, r.Type, r.Title, r.Description, r.Status, r.Priority,
			r.Severity, r.AssigneeID, r.Position, r.PriorityRank, r.SeverityRank,
		); err != nil {
			return fmt.Errorf("insert tickets id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.ReferenceCounters {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reference_counters(project_id, kind, next_seq) VALUES (?, ?, ?)`,
			r.ProjectID, r.Kind, r.NextSeq,
		); err != nil {
			return fmt.Errorf("insert reference_counters (%d,%s): %w", r.ProjectID, r.Kind, err)
		}
	}

	for _, r := range env.Decisions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO decisions(id, project_id, seq, title, context, decision, rationale, consequences, status, superseded_by)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ProjectID, r.Seq, r.Title, r.Context, r.Decision, r.Rationale, r.Consequences, r.Status, r.SupersededBy,
		); err != nil {
			return fmt.Errorf("insert decisions id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.DecisionVersions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO decision_versions(id, decision_id, version, title, context, decision, rationale, consequences, status, edited_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.DecisionID, r.Version, r.Title, r.Context, r.Decision, r.Rationale, r.Consequences, r.Status, r.EditedBy, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert decision_versions id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.ContentItems {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO content_items(id, project_id, kind, seq, title, representation, body, file_hash, file_name,
			                            file_size, media_type, checksum, path_value, url_value)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ProjectID, r.Kind, r.Seq, r.Title, r.Representation, r.Body, r.FileHash, r.FileName,
			r.FileSize, r.MediaType, r.Checksum, r.PathValue, r.URLValue,
		); err != nil {
			return fmt.Errorf("insert content_items id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.ContentVersions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO content_versions(id, content_item_id, version, representation, title, body, file_hash,
			                               file_name, file_size, media_type, checksum, path_value, url_value, edited_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ContentItemID, r.Version, r.Representation, r.Title, r.Body, r.FileHash,
			r.FileName, r.FileSize, r.MediaType, r.Checksum, r.PathValue, r.URLValue, r.EditedBy, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert content_versions id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.Comments {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO comments(id, entity_id, author_id, body, version, created_at, updated_at, deleted_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.EntityID, r.AuthorID, r.Body, r.Version, r.CreatedAt, r.UpdatedAt, r.DeletedAt,
		); err != nil {
			return fmt.Errorf("insert comments id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.CommentVersions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO comment_versions(id, comment_id, version, body, edited_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			r.ID, r.CommentID, r.Version, r.Body, r.EditedBy, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert comment_versions id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.Attachments {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO attachments(id, entity_id, comment_id, kind, title, current_version, file_hash, file_name,
			                          file_size, media_type, checksum, path_value, created_at, created_by, deleted_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.EntityID, r.CommentID, r.Kind, r.Title, r.CurrentVersion, r.FileHash, r.FileName,
			r.FileSize, r.MediaType, r.Checksum, r.PathValue, r.CreatedAt, r.CreatedBy, r.DeletedAt,
		); err != nil {
			return fmt.Errorf("insert attachments id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.AttachmentVersions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO attachment_versions(id, attachment_id, version, kind, file_hash, file_name, file_size,
			                                  media_type, checksum, path_value, uploaded_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.AttachmentID, r.Version, r.Kind, r.FileHash, r.FileName, r.FileSize,
			r.MediaType, r.Checksum, r.PathValue, r.UploadedBy, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert attachment_versions id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.TicketRelationships {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ticket_relationships(source_id, target_id, type, created_at, created_by) VALUES (?, ?, ?, ?, ?)`,
			r.SourceID, r.TargetID, r.Type, r.CreatedAt, r.CreatedBy,
		); err != nil {
			return fmt.Errorf("insert ticket_relationships (%d,%d,%s): %w", r.SourceID, r.TargetID, r.Type, err)
		}
	}

	for _, r := range env.EntityAssociations {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entity_associations(source_id, target_id, created_at, created_by) VALUES (?, ?, ?, ?)`,
			r.SourceID, r.TargetID, r.CreatedAt, r.CreatedBy,
		); err != nil {
			return fmt.Errorf("insert entity_associations (%d,%d): %w", r.SourceID, r.TargetID, err)
		}
	}

	for _, r := range env.DerivedMentions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO derived_mentions(source_entity_id, source_comment_id, target_entity_id, created_at) VALUES (?, ?, ?, ?)`,
			r.SourceEntityID, r.SourceCommentID, r.TargetEntityID, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert derived_mentions (%d,%d,%d): %w", r.SourceEntityID, r.SourceCommentID, r.TargetEntityID, err)
		}
	}

	for _, r := range env.ActorMentions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO actor_mentions(source_entity_id, source_comment_id, actor_id, created_at) VALUES (?, ?, ?, ?)`,
			r.SourceEntityID, r.SourceCommentID, r.ActorID, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert actor_mentions (%d,%d,%d): %w", r.SourceEntityID, r.SourceCommentID, r.ActorID, err)
		}
	}

	for _, r := range env.ExternalLinks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO external_links(id, entity_id, title, url, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
			r.ID, r.EntityID, r.Title, r.URL, r.CreatedAt, r.CreatedBy,
		); err != nil {
			return fmt.Errorf("insert external_links id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.AuditEvents {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO audit_events(id, entity_id, target_actor_id, actor_id, event_type, comment_id, correlation_id, changes, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.EntityID, r.TargetActorID, r.ActorID, r.EventType, r.CommentID, r.CorrelationID, r.Changes, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert audit_events id=%d: %w", r.ID, err)
		}
	}

	for _, r := range env.Subscriptions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO subscriptions(entity_id, actor_id, created_at) VALUES (?, ?, ?)`,
			r.EntityID, r.ActorID, r.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert subscriptions (%d,%d): %w", r.EntityID, r.ActorID, err)
		}
	}

	for _, r := range env.Notifications {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO notifications(id, actor_id, kind, entity_id, comment_id, triggered_by, created_at, read_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ActorID, r.Kind, r.EntityID, r.CommentID, r.TriggeredBy, r.CreatedAt, r.ReadAt,
		); err != nil {
			return fmt.Errorf("insert notifications id=%d: %w", r.ID, err)
		}
	}

	return nil
}
