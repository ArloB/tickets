-- Phase 5 Step 7: @actor mentions, subscriptions, and notifications
-- (product spec §6.4, ADR 0015's deferred @actor-mention table, ADR
-- 0019).
--
-- actor_mentions mirrors derived_mentions' shape (0002_core_domain.sql)
-- but targets actors, not entities — an actor has no entities.id
-- (ADR 0002's registry only covers principal records), so it cannot
-- reuse derived_mentions' target_entity_id FK. Same delete-and-
-- reinsert-per-write discipline, same source_comment_id=0 sentinel for
-- "the source entity's own body" (never a real comments.id, which
-- AUTOINCREMENTs from 1).
CREATE TABLE actor_mentions (
    source_entity_id  INTEGER NOT NULL REFERENCES entities(id),
    source_comment_id INTEGER NOT NULL DEFAULT 0,
    actor_id          INTEGER NOT NULL REFERENCES actors(id),
    created_at        TEXT NOT NULL,
    PRIMARY KEY (source_entity_id, source_comment_id, actor_id)
);
CREATE INDEX idx_actor_mentions_actor ON actor_mentions(actor_id);

-- One row per (entity, subscriber) — creating or commenting on an
-- entity subscribes the actor by default (§6.4); unsubscribing simply
-- deletes the row. No soft-delete: an unsubscribe is not an event
-- worth an audit trail of its own, just a live on/off flag.
CREATE TABLE subscriptions (
    entity_id  INTEGER NOT NULL REFERENCES entities(id),
    actor_id   INTEGER NOT NULL REFERENCES actors(id),
    created_at TEXT NOT NULL,
    PRIMARY KEY (entity_id, actor_id)
);
CREATE INDEX idx_subscriptions_actor ON subscriptions(actor_id);

-- notifications is an append-only delivered-event log, not a live
-- index: read_at is the only field an ordinary operation ever updates
-- after insert (ADR 0019 — ordinary application operations don't
-- delete or rewrite a notification once created, the same immutability
-- §5.12 already gives audit_events, deliberately not the mention-
-- cleanup precedent derived_mentions/actor_mentions follow). entity_id
-- is always the subject record the notification is about (the owning
-- ticket for a comment/mention notification, not the comment itself);
-- comment_id is set only for a comment-sourced notification
-- (commented/mentioned-in-a-comment). triggered_by is nullable only
-- for the same reason entities.created_by is (a future non-actor-
-- attributed system event) — every notification this step actually
-- emits sets it.
CREATE TABLE notifications (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id     INTEGER NOT NULL REFERENCES actors(id),
    kind         TEXT NOT NULL,  -- 'assigned' | 'mentioned' | 'commented' | 'changed'
    entity_id    INTEGER NOT NULL REFERENCES entities(id),
    comment_id   INTEGER,
    triggered_by INTEGER REFERENCES actors(id),
    created_at   TEXT NOT NULL,
    read_at      TEXT
);
CREATE INDEX idx_notifications_actor_created ON notifications(actor_id, created_at, id);
