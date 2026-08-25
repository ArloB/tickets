-- Phase 6 Step 1: resolve the open question internal/service/agent.go
-- left as a placeholder (ADR 0012 amendment) — product spec §5.12
-- requires an audit event for every "token operation", but
-- audit_events.entity_id was NOT NULL and actors sit outside the
-- entities registry (ADR 0002), so CreateAgent/CreateAgentToken/
-- RevokeAgentToken had nothing to attach an event to.
--
-- entity_id becomes nullable and a new target_actor_id column is
-- added for actor-scoped events (agent_created, agent_token_issued,
-- agent_token_revoked); exactly one of the two is set per row. SQLite
-- has no ALTER COLUMN, so this rebuilds the table: create the new
-- shape, copy every existing row (all of which have entity_id set,
-- target_actor_id NULL), drop the old table, rename, and recreate both
-- indexes untouched. idx_audit_events_created_at
-- (0007_activity_feed_index.sql) is unaffected — it doesn't reference
-- entity_id — so it isn't recreated here.
--
-- ListActivityPage (internal/store/activity.go) joins audit_events to
-- entities via `JOIN entities e ON e.id = ae.entity_id` — an INNER
-- JOIN, so a target_actor_id row (entity_id NULL) is excluded from
-- every project activity feed automatically, with no extra filter
-- needed. Token events are project-less by nature (an agent isn't
-- owned by one project); they surface only through
-- ListAgentAuditEvents (Phase 6 Step 1's admin-view addition, not this
-- migration).

CREATE TABLE audit_events_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id       INTEGER REFERENCES entities(id),
    target_actor_id INTEGER REFERENCES actors(id),
    actor_id        INTEGER NOT NULL REFERENCES actors(id),
    event_type      TEXT NOT NULL,
    comment_id      INTEGER REFERENCES comments(id),
    correlation_id  TEXT NOT NULL,
    changes         TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL,
    CHECK ((entity_id IS NULL) != (target_actor_id IS NULL))
);

INSERT INTO audit_events_new (id, entity_id, target_actor_id, actor_id, event_type, comment_id, correlation_id, changes, created_at)
SELECT id, entity_id, NULL, actor_id, event_type, comment_id, correlation_id, changes, created_at
FROM audit_events;

DROP TABLE audit_events;
ALTER TABLE audit_events_new RENAME TO audit_events;

CREATE INDEX idx_audit_events_entity ON audit_events(entity_id, created_at);
CREATE INDEX idx_audit_events_actor ON audit_events(actor_id, created_at);
CREATE INDEX idx_audit_events_created_at ON audit_events(created_at, id);
CREATE INDEX idx_audit_events_target_actor ON audit_events(target_actor_id, created_at);
