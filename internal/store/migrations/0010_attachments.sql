-- Phase 5 Step 4: attachments (product spec §5.11), within ADR 0007's
-- already-fixed boundary — content-addressed uploads, path references
-- never read. Only `upload`/`path` kinds exist here: `url`-shaped links
-- stay entirely on `external_links` (0006_external_links.sql), removing
-- the overlap between the two mechanisms rather than documenting it as
-- an accepted ambiguity (see Phase 5 plan's confirmed decisions).
--
-- Follows the current-state-in-main-row + `_versions`-holds-only-history
-- pattern decisions (0008) and content_items (0009) already established.
-- attachments is not a 1:1 extension of `entities` — an attachment has
-- no public reference of its own (§6.3's stable-reference requirement
-- only applies to principal record kinds); it is scoped to exactly one
-- of entity_id (any principal entity: ticket/feature/decision/plan/
-- document) or comment_id, enforced in Go since SQLite has no portable
-- "exactly one of" CHECK across two nullable FKs worth relying on here.
CREATE TABLE attachments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id       INTEGER REFERENCES entities(id),
    comment_id      INTEGER REFERENCES comments(id),
    kind            TEXT NOT NULL,       -- 'upload' | 'path'
    title           TEXT NOT NULL,
    current_version INTEGER NOT NULL DEFAULT 1,
    file_hash       TEXT,
    file_name       TEXT,
    file_size       INTEGER,
    media_type      TEXT,
    checksum        TEXT,
    path_value      TEXT,
    created_at      TEXT NOT NULL,
    created_by      INTEGER NOT NULL REFERENCES actors(id),
    deleted_at      TEXT
);
CREATE INDEX idx_attachments_entity ON attachments(entity_id);
CREATE INDEX idx_attachments_comment ON attachments(comment_id);

CREATE TABLE attachment_versions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    attachment_id  INTEGER NOT NULL REFERENCES attachments(id),
    version        INTEGER NOT NULL,
    kind           TEXT NOT NULL,
    file_hash      TEXT,
    file_name      TEXT,
    file_size      INTEGER,
    media_type     TEXT,
    checksum       TEXT,
    path_value     TEXT,
    uploaded_by    INTEGER NOT NULL REFERENCES actors(id),
    created_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_attachment_versions_attachment_version ON attachment_versions(attachment_id, version);
