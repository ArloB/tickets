-- Phase 5 Step 3: content_items (product spec §5.9) — plans and
-- documents as a shared table, entities.kind (KindPlan/KindDocument)
-- is the sole discriminator (docs/adr/0017-content-items.md). This
-- step only ever writes representation='markdown'; the file/path/url
-- columns are reserved for Steps 4-5 and stay NULL until then.
-- kind ('plan' | 'document') duplicates entities.kind onto this row —
-- the one exception to this table's "no redundant kind column" design
-- (docs/adr/0017-content-items.md): plans and documents share one
-- physical table but number their references independently (ADR 0009),
-- so a plan and a document in the same project can legitimately both
-- land on seq=1. SQLite can't express a uniqueness constraint across a
-- join, so the (project_id, seq) uniqueness that actually matters —
-- "this reference names exactly one row" — has to be
-- (project_id, kind, seq) instead, which requires kind to live on this
-- table. Always set equal to the owning entities row's kind, in the
-- same transaction, by InsertContentItem.
CREATE TABLE content_items (
    id             INTEGER PRIMARY KEY REFERENCES entities(id),
    project_id     INTEGER NOT NULL REFERENCES entities(id),
    kind           TEXT NOT NULL,
    seq            INTEGER NOT NULL,
    title          TEXT NOT NULL,
    representation TEXT NOT NULL,
    body           TEXT NOT NULL DEFAULT '',
    file_hash      TEXT,
    file_name      TEXT,
    file_size      INTEGER,
    media_type     TEXT,
    checksum       TEXT,
    path_value     TEXT,
    url_value      TEXT
);
CREATE UNIQUE INDEX idx_content_items_project_kind_seq ON content_items(project_id, kind, seq);

-- Archive-then-overwrite version history, the same pattern
-- decision_versions/comment_versions use: InsertContentItemVersion
-- archives the pre-update row before UpdateContentItemFields
-- overwrites it, both inside one transaction.
CREATE TABLE content_versions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    content_item_id INTEGER NOT NULL REFERENCES entities(id),
    version         INTEGER NOT NULL,
    representation  TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT,
    file_hash       TEXT,
    file_name       TEXT,
    file_size       INTEGER,
    media_type      TEXT,
    checksum        TEXT,
    path_value      TEXT,
    url_value       TEXT,
    edited_by       INTEGER NOT NULL REFERENCES actors(id),
    created_at      TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_content_versions_item_version ON content_versions(content_item_id, version);
