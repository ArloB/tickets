package backup

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/buildinfo"
	"github.com/ArloB/tickets/internal/store"
	"github.com/google/uuid"
)

const envelopeFormatVersion = 1

// Export reads every non-secret domain table from db into a versioned
// Envelope (`tickets export`, product spec §12's portable-JSON
// mechanism). See Envelope's doc comment for exactly what is and
// is not included, and why.
//
// attachmentsDir is optional (product spec §7.3's `[--attachments
// DIR]`): when non-empty, every blob referenced by an exported
// attachment or an uploaded content item's file representation is
// copied there via srcBlobs, in the same sharded layout Backup uses,
// so the directory can be pointed at directly by `tickets import
// --attachments`. When empty, Export still succeeds — the envelope
// carries attachment/content-item metadata (file_hash, file_name,
// size, ...) either way — but the bytes themselves are left behind;
// Import refuses to commit an envelope with referenced blobs unless
// it's given a matching --attachments directory, rather than silently
// producing an installation where attachment metadata exists and the
// content doesn't.
func Export(ctx context.Context, db *sql.DB, srcBlobs *blobstore.Store, attachmentsDir string) (Envelope, error) {
	schemaVersion, err := store.HighestEmbeddedMigrationVersion()
	if err != nil {
		return Envelope{}, fmt.Errorf("export: schema version: %w", err)
	}
	env := Envelope{
		FormatVersion: envelopeFormatVersion,
		SchemaVersion: schemaVersion,
		ServerVersion: buildinfo.Version,
		ExportedAt:    time.Now().UTC().Format(store.TimeLayout),
	}

	for _, step := range []struct {
		name string
		fn   func(context.Context, *sql.DB, *Envelope) error
	}{
		{"entities", exportEntities},
		{"actors", exportActors},
		{"projects", exportProjects},
		{"features", exportFeatures},
		{"tickets", exportTickets},
		{"reference_counters", exportReferenceCounters},
		{"decisions", exportDecisions},
		{"decision_versions", exportDecisionVersions},
		{"content_items", exportContentItems},
		{"content_versions", exportContentVersions},
		{"comments", exportComments},
		{"comment_versions", exportCommentVersions},
		{"attachments", exportAttachments},
		{"attachment_versions", exportAttachmentVersions},
		{"ticket_relationships", exportRelationships},
		{"entity_associations", exportAssociations},
		{"derived_mentions", exportDerivedMentions},
		{"actor_mentions", exportActorMentions},
		{"external_links", exportExternalLinks},
		{"audit_events", exportAuditEvents},
		{"subscriptions", exportSubscriptions},
		{"notifications", exportNotifications},
	} {
		if err := step.fn(ctx, db, &env); err != nil {
			return Envelope{}, fmt.Errorf("export: %s: %w", step.name, err)
		}
	}

	if attachmentsDir != "" {
		destBlobs, err := blobstore.Open(attachmentsDir)
		if err != nil {
			return Envelope{}, fmt.Errorf("export: open attachments directory: %w", err)
		}
		for hash := range referencedBlobHashes(env) {
			if _, err := copyBlob(srcBlobs, destBlobs, hash); err != nil {
				return Envelope{}, fmt.Errorf("export: %w", err)
			}
		}
	}
	return env, nil
}

func uuidString(b []byte) (string, error) {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return "", fmt.Errorf("parse uuid: %w", err)
	}
	return u.String(), nil
}

func exportEntities(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, uuid, project_id, kind, version, created_by, created_at, updated_at, deleted_at FROM entities ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r EntityRow
		var u []byte
		if err := rows.Scan(&r.ID, &u, &r.ProjectID, &r.Kind, &r.Version, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt); err != nil {
			return err
		}
		if r.UUID, err = uuidString(u); err != nil {
			return err
		}
		env.Entities = append(env.Entities, r)
	}
	return rows.Err()
}

func exportActors(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, uuid, kind, name, owner_id, description, created_at, updated_at, deleted_at FROM actors ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r ActorRow
		var u []byte
		if err := rows.Scan(&r.ID, &u, &r.Kind, &r.Name, &r.OwnerID, &r.Description, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt); err != nil {
			return err
		}
		if r.UUID, err = uuidString(u); err != nil {
			return err
		}
		env.Actors = append(env.Actors, r)
	}
	return rows.Err()
}

func exportProjects(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, key, title, description, status, general_feature_id FROM projects ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r ProjectRow
		if err := rows.Scan(&r.ID, &r.Key, &r.Title, &r.Description, &r.Status, &r.GeneralFeatureID); err != nil {
			return err
		}
		env.Projects = append(env.Projects, r)
	}
	return rows.Err()
}

func exportFeatures(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, seq, title, status, description, priority, position, priority_rank FROM features ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r FeatureRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Seq, &r.Title, &r.Status, &r.Description, &r.Priority, &r.Position, &r.PriorityRank); err != nil {
			return err
		}
		env.Features = append(env.Features, r)
	}
	return rows.Err()
}

func exportTickets(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, feature_id, seq, type, title, description, status, priority, severity,
		        assignee_id, position, priority_rank, severity_rank
		 FROM tickets ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r TicketRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.FeatureID, &r.Seq, &r.Type, &r.Title, &r.Description, &r.Status,
			&r.Priority, &r.Severity, &r.AssigneeID, &r.Position, &r.PriorityRank, &r.SeverityRank); err != nil {
			return err
		}
		env.Tickets = append(env.Tickets, r)
	}
	return rows.Err()
}

func exportReferenceCounters(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx, `SELECT project_id, kind, next_seq FROM reference_counters ORDER BY project_id, kind`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r ReferenceCounterRow
		if err := rows.Scan(&r.ProjectID, &r.Kind, &r.NextSeq); err != nil {
			return err
		}
		env.ReferenceCounters = append(env.ReferenceCounters, r)
	}
	return rows.Err()
}

func exportDecisions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, seq, title, context, decision, rationale, consequences, status, superseded_by
		 FROM decisions ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r DecisionRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Seq, &r.Title, &r.Context, &r.Decision, &r.Rationale,
			&r.Consequences, &r.Status, &r.SupersededBy); err != nil {
			return err
		}
		env.Decisions = append(env.Decisions, r)
	}
	return rows.Err()
}

func exportDecisionVersions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, decision_id, version, title, context, decision, rationale, consequences, status, edited_by, created_at
		 FROM decision_versions ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r DecisionVersionRow
		if err := rows.Scan(&r.ID, &r.DecisionID, &r.Version, &r.Title, &r.Context, &r.Decision, &r.Rationale,
			&r.Consequences, &r.Status, &r.EditedBy, &r.CreatedAt); err != nil {
			return err
		}
		env.DecisionVersions = append(env.DecisionVersions, r)
	}
	return rows.Err()
}

func exportContentItems(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, kind, seq, title, representation, body, file_hash, file_name, file_size,
		        media_type, checksum, path_value, url_value, status
		 FROM content_items ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r ContentItemRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.Seq, &r.Title, &r.Representation, &r.Body,
			&r.FileHash, &r.FileName, &r.FileSize, &r.MediaType, &r.Checksum, &r.PathValue, &r.URLValue, &r.Status); err != nil {
			return err
		}
		env.ContentItems = append(env.ContentItems, r)
	}
	return rows.Err()
}

func exportContentVersions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, content_item_id, version, representation, title, body, file_hash, file_name, file_size,
		        media_type, checksum, path_value, url_value, edited_by, created_at
		 FROM content_versions ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r ContentVersionRow
		if err := rows.Scan(&r.ID, &r.ContentItemID, &r.Version, &r.Representation, &r.Title, &r.Body,
			&r.FileHash, &r.FileName, &r.FileSize, &r.MediaType, &r.Checksum, &r.PathValue, &r.URLValue,
			&r.EditedBy, &r.CreatedAt); err != nil {
			return err
		}
		env.ContentVersions = append(env.ContentVersions, r)
	}
	return rows.Err()
}

func exportComments(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, entity_id, author_id, body, version, created_at, updated_at, deleted_at FROM comments ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r CommentRow
		if err := rows.Scan(&r.ID, &r.EntityID, &r.AuthorID, &r.Body, &r.Version, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt); err != nil {
			return err
		}
		env.Comments = append(env.Comments, r)
	}
	return rows.Err()
}

func exportCommentVersions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, comment_id, version, body, edited_by, created_at FROM comment_versions ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r CommentVersionRow
		if err := rows.Scan(&r.ID, &r.CommentID, &r.Version, &r.Body, &r.EditedBy, &r.CreatedAt); err != nil {
			return err
		}
		env.CommentVersions = append(env.CommentVersions, r)
	}
	return rows.Err()
}

func exportAttachments(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, entity_id, comment_id, kind, title, current_version, file_hash, file_name, file_size,
		        media_type, checksum, path_value, created_at, created_by, deleted_at
		 FROM attachments ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r AttachmentRow
		if err := rows.Scan(&r.ID, &r.EntityID, &r.CommentID, &r.Kind, &r.Title, &r.CurrentVersion,
			&r.FileHash, &r.FileName, &r.FileSize, &r.MediaType, &r.Checksum, &r.PathValue,
			&r.CreatedAt, &r.CreatedBy, &r.DeletedAt); err != nil {
			return err
		}
		env.Attachments = append(env.Attachments, r)
	}
	return rows.Err()
}

func exportAttachmentVersions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, attachment_id, version, kind, file_hash, file_name, file_size, media_type, checksum,
		        path_value, uploaded_by, created_at
		 FROM attachment_versions ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r AttachmentVersionRow
		if err := rows.Scan(&r.ID, &r.AttachmentID, &r.Version, &r.Kind, &r.FileHash, &r.FileName, &r.FileSize,
			&r.MediaType, &r.Checksum, &r.PathValue, &r.UploadedBy, &r.CreatedAt); err != nil {
			return err
		}
		env.AttachmentVersions = append(env.AttachmentVersions, r)
	}
	return rows.Err()
}

func exportRelationships(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT source_id, target_id, type, created_at, created_by FROM ticket_relationships ORDER BY source_id, target_id, type`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r RelationshipRow
		if err := rows.Scan(&r.SourceID, &r.TargetID, &r.Type, &r.CreatedAt, &r.CreatedBy); err != nil {
			return err
		}
		env.TicketRelationships = append(env.TicketRelationships, r)
	}
	return rows.Err()
}

func exportAssociations(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT source_id, target_id, created_at, created_by FROM entity_associations ORDER BY source_id, target_id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r AssociationRow
		if err := rows.Scan(&r.SourceID, &r.TargetID, &r.CreatedAt, &r.CreatedBy); err != nil {
			return err
		}
		env.EntityAssociations = append(env.EntityAssociations, r)
	}
	return rows.Err()
}

func exportDerivedMentions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT source_entity_id, source_comment_id, target_entity_id, created_at
		 FROM derived_mentions ORDER BY source_entity_id, source_comment_id, target_entity_id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r DerivedMentionRow
		if err := rows.Scan(&r.SourceEntityID, &r.SourceCommentID, &r.TargetEntityID, &r.CreatedAt); err != nil {
			return err
		}
		env.DerivedMentions = append(env.DerivedMentions, r)
	}
	return rows.Err()
}

func exportActorMentions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT source_entity_id, source_comment_id, actor_id, created_at
		 FROM actor_mentions ORDER BY source_entity_id, source_comment_id, actor_id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r ActorMentionRow
		if err := rows.Scan(&r.SourceEntityID, &r.SourceCommentID, &r.ActorID, &r.CreatedAt); err != nil {
			return err
		}
		env.ActorMentions = append(env.ActorMentions, r)
	}
	return rows.Err()
}

func exportExternalLinks(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, entity_id, title, url, created_at, created_by FROM external_links ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r ExternalLinkRow
		if err := rows.Scan(&r.ID, &r.EntityID, &r.Title, &r.URL, &r.CreatedAt, &r.CreatedBy); err != nil {
			return err
		}
		env.ExternalLinks = append(env.ExternalLinks, r)
	}
	return rows.Err()
}

// exportAuditEvents never selects a column other than these nine —
// audit_events has no secret columns, but this is still an explicit
// list rather than SELECT *, matching this package's rule for every
// other table.
func exportAuditEvents(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, entity_id, target_actor_id, actor_id, event_type, comment_id, correlation_id, changes, created_at
		 FROM audit_events ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r AuditEventRow
		if err := rows.Scan(&r.ID, &r.EntityID, &r.TargetActorID, &r.ActorID, &r.EventType, &r.CommentID,
			&r.CorrelationID, &r.Changes, &r.CreatedAt); err != nil {
			return err
		}
		env.AuditEvents = append(env.AuditEvents, r)
	}
	return rows.Err()
}

func exportSubscriptions(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx, `SELECT entity_id, actor_id, created_at FROM subscriptions ORDER BY entity_id, actor_id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r SubscriptionRow
		if err := rows.Scan(&r.EntityID, &r.ActorID, &r.CreatedAt); err != nil {
			return err
		}
		env.Subscriptions = append(env.Subscriptions, r)
	}
	return rows.Err()
}

func exportNotifications(ctx context.Context, db *sql.DB, env *Envelope) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, actor_id, kind, entity_id, comment_id, triggered_by, created_at, read_at FROM notifications ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r NotificationRow
		if err := rows.Scan(&r.ID, &r.ActorID, &r.Kind, &r.EntityID, &r.CommentID, &r.TriggeredBy, &r.CreatedAt, &r.ReadAt); err != nil {
			return err
		}
		env.Notifications = append(env.Notifications, r)
	}
	return rows.Err()
}
