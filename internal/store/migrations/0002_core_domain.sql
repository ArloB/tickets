-- Phase 1: actors, comments, audit events, relationships, associations,
-- derived mentions, and the ordering/assignment columns Phase 0 left
-- out. See the Phase 1 implementation plan and ADR 0011-0015.

-- Actors (product spec §4.1, §8.3). Deliberately outside the entities
-- registry (ADR 0002): actors have no project scope and no public
-- reference — they're a separate identity system, not project content.
-- Wire form is "kind:name" (e.g. "agent:codex-1"), matching
-- docs/contracts/representations.md's creator example.
CREATE TABLE actors (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid       BLOB(16) NOT NULL,
    kind       TEXT NOT NULL,  -- 'human' | 'agent' | 'system'
    name       TEXT NOT NULL,
    owner_id   INTEGER REFERENCES actors(id),  -- an agent's owning human (§4.1)
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX idx_actors_uuid ON actors(uuid);
CREATE UNIQUE INDEX idx_actors_kind_name ON actors(kind, name);

-- Seed actors ahead of the real ADR 0004 auth system (Phase 2): every
-- mutation needs an attributed actor now, and there is no authenticated
-- caller yet to attribute it to. 'system' backfills every pre-Phase-1
-- row below; 'local' is what internal/httpapi passes until Phase 2
-- resolves a real session/token. Timestamps are built with strftime
-- rather than a bound parameter because this is static migration SQL;
-- the expression matches store.TimeLayout's fixed 30-char width
-- exactly (date + 'T' + time + '.' + 9 fractional digits + 'Z') so
-- store.parseTime succeeds on it like any other row.
INSERT INTO actors(uuid, kind, name, created_at, updated_at) VALUES
    (randomblob(16), 'system', 'system',
     strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z',
     strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z'),
    (randomblob(16), 'human', 'local',
     strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z',
     strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z');

-- entities.created_by (product spec §5.12's "actor" on every record).
-- Added nullable: with foreign_keys=ON, ALTER TABLE ADD COLUMN cannot
-- carry a REFERENCES clause together with a non-NULL default, so every
-- pre-Phase-1 row is backfilled to the seeded system actor by the
-- UPDATE below, and NOT-NULL-ness for new rows is enforced in Go
-- (internal/service), not by a schema constraint — consistent with
-- 0001_initial.sql's existing enum-validation-lives-in-Go convention.
ALTER TABLE entities ADD COLUMN created_by INTEGER REFERENCES actors(id);
UPDATE entities SET created_by = (SELECT id FROM actors WHERE kind = 'system' AND name = 'system');

-- Ticket assignment (§5.5) and manual ordering (§5.6). assignee_id
-- stays nullable permanently — an unassigned ticket is a normal state,
-- not a migration artifact.
ALTER TABLE tickets ADD COLUMN assignee_id INTEGER REFERENCES actors(id);
ALTER TABLE tickets ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

-- priority_rank / severity_rank make ORDER BY correct by construction.
-- priority and severity are TEXT ('critical'|'high'|'medium'|'low'),
-- which sorts alphabetically to critical, high, low, medium — wrong
-- for the priority queue (§5.6) and the severity-ordered issue
-- register (§5.5). SQLite disallows adding a non-VIRTUAL generated
-- column via ALTER TABLE ADD COLUMN, so these are plain integers,
-- written by exactly one place (internal/store's priorityRank /
-- severityRank helpers) on every insert or update of the source
-- column — never computed ad hoc in a query. 0 = critical ... 3 = low,
-- matching docs/contracts/enums.md's spec order; 4 is the "no/unknown
-- value" sentinel a NULL severity or (theoretically) an invalid stored
-- value lands on, sorting after every real severity.
ALTER TABLE tickets ADD COLUMN priority_rank INTEGER NOT NULL DEFAULT 4;
ALTER TABLE tickets ADD COLUMN severity_rank INTEGER NOT NULL DEFAULT 4;
UPDATE tickets SET priority_rank = CASE priority
    WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END;
UPDATE tickets SET severity_rank = CASE severity
    WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END;

CREATE INDEX idx_tickets_priority_queue ON tickets(project_id, priority_rank, position);
CREATE INDEX idx_tickets_issue_register ON tickets(project_id, severity_rank, priority_rank, position);

-- Features become first-class (§5.4): description, priority, and
-- position. features.priority defaults to 'medium' — the same default
-- 0001_initial.sql already gives tickets.priority — so priority_rank's
-- default (2, medium's rank) is correct for every existing row with no
-- separate backfill UPDATE needed, unlike tickets.priority_rank above,
-- where existing rows already carry varied values.
ALTER TABLE features ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE features ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium';
ALTER TABLE features ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE features ADD COLUMN priority_rank INTEGER NOT NULL DEFAULT 2;

CREATE INDEX idx_features_priority_queue ON features(project_id, priority_rank, position);

-- Comments and their edit history (§5.10). id is a plain surrogate
-- key, not shared with the entities registry — comments aren't
-- principal entities with their own public reference (ADR 0002's 1:1
-- extension pattern doesn't apply to them) — but it stays a small
-- INTEGER PRIMARY KEY so Phase 5's external-content FTS5 can use it as
-- content_rowid without a later migration (same reasoning as ADR
-- 0002's entities.id).
CREATE TABLE comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id  INTEGER NOT NULL REFERENCES entities(id),
    author_id  INTEGER NOT NULL REFERENCES actors(id),
    body       TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT  -- soft-delete tombstone (§5.10); the comment row
                      -- survives so the tombstone stays visible
);
CREATE INDEX idx_comments_entity ON comments(entity_id, created_at);

-- Every edit snapshots the prior body (§5.10: "comment edits create
-- versions and remain visible in the audit trail").
CREATE TABLE comment_versions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    comment_id INTEGER NOT NULL REFERENCES comments(id),
    version    INTEGER NOT NULL,
    body       TEXT NOT NULL,
    edited_by  INTEGER NOT NULL REFERENCES actors(id),
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_comment_versions_comment_version ON comment_versions(comment_id, version);

-- Append-only audit trail (§5.12). comment_id is set only for events
-- that describe a comment (add/edit/delete) so the activity feed can
-- join comments and other audit events into one stream per entity
-- (§5.10: "the activity feed combines comments with selected audit
-- events"). changes is a JSON before/after or patch fragment — no
-- schema-level shape beyond "valid JSON", matching how the rest of
-- this migration keeps enum/structure validation in Go, not SQL.
CREATE TABLE audit_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id      INTEGER NOT NULL REFERENCES entities(id),
    actor_id       INTEGER NOT NULL REFERENCES actors(id),
    event_type     TEXT NOT NULL,
    comment_id     INTEGER REFERENCES comments(id),
    correlation_id TEXT NOT NULL,
    changes        TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL
);
CREATE INDEX idx_audit_events_entity ON audit_events(entity_id, created_at);
CREATE INDEX idx_audit_events_actor ON audit_events(actor_id, created_at);

-- Typed ticket relationships (§5.7). One row per logical edge, not two
-- kept in sync: parent_of stores parent->child, blocks stores
-- blocker->blocked, supersedes stores superseder->superseded, and
-- related_to/duplicate_of canonicalize to source_id < target_id at
-- write time (internal/service, not this schema). Reading the far end
-- of a directed edge uses domain.RelationshipType.Inverse(), already
-- implemented in internal/domain, against the same row.
CREATE TABLE ticket_relationships (
    source_id  INTEGER NOT NULL REFERENCES entities(id),
    target_id  INTEGER NOT NULL REFERENCES entities(id),
    type       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    created_by INTEGER NOT NULL REFERENCES actors(id),
    PRIMARY KEY (source_id, target_id, type)
);
-- Indexes the far end of the edge so "what points at this ticket" is
-- as cheap as "what does this ticket point at" (the primary key's
-- natural source-first order only covers the latter).
CREATE INDEX idx_ticket_relationships_target ON ticket_relationships(target_id, type);

-- The looser, symmetric associated_with link (§5.7) for
-- decisions/plans/documents/features/tickets. No type column: per
-- docs/contracts/enums.md, association_type has exactly one value in
-- the MVP, so recording it on every row would be redundant with the
-- table's own meaning. Canonicalized source_id < target_id at write
-- time, same as related_to above.
CREATE TABLE entity_associations (
    source_id  INTEGER NOT NULL REFERENCES entities(id),
    target_id  INTEGER NOT NULL REFERENCES entities(id),
    created_at TEXT NOT NULL,
    created_by INTEGER NOT NULL REFERENCES actors(id),
    PRIMARY KEY (source_id, target_id)
);
CREATE INDEX idx_entity_associations_target ON entity_associations(target_id);

-- Derived mentions (§5.2): a backlink edge created by scanning
-- Markdown bodies and comments for references, never implying a typed
-- relationship or scheduling semantics. source_comment_id uses a
-- NOT NULL DEFAULT 0 sentinel (0 = the source entity's own body, never
-- a real comments.id since that column AUTOINCREMENTs from 1) rather
-- than a nullable column, because SQLite's NULL != NULL would silently
-- defeat this primary key's uniqueness for every own-body mention.
CREATE TABLE derived_mentions (
    source_entity_id  INTEGER NOT NULL REFERENCES entities(id),
    source_comment_id INTEGER NOT NULL DEFAULT 0,
    target_entity_id  INTEGER NOT NULL REFERENCES entities(id),
    created_at        TEXT NOT NULL,
    PRIMARY KEY (source_entity_id, source_comment_id, target_entity_id)
);
CREATE INDEX idx_derived_mentions_target ON derived_mentions(target_entity_id);
