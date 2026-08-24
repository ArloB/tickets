-- Phase 5 Step 6: unified FTS5 search (product spec §5.12), following
-- the external-content + trigger pattern the sqlite spike proved
-- (docs/spikes/sqlite/REPORT.md assertion 5) and ADR 0003 anticipated.
--
-- search_documents is a synthetic index over two different id spaces:
-- entities.id (tickets/features/decisions/plans/documents) and
-- comments.id. (source_kind, source_id) is the natural key back to
-- the origin row; search_documents.id is its own AUTOINCREMENT surrogate
-- so FTS5's content_rowid has a single, dense id space to join against
-- regardless of which source table a hit came from — see ADR 0018,
-- which amends ADR 0003's "joined via entities.id" consequence line.
--
-- entity_id is the owning entity's internal id for every row: for an
-- 'entity' source it equals source_id; for a 'comment' source it's the
-- comment's parent ticket entity id. This is what lets a ticket
-- delete/restore cascade to its comments' search rows with one
-- indexed lookup (WHERE entity_id = ?) instead of a join back to
-- comments on every delete.
--
-- ref/comment_id are denormalized (not resolved via a join at query
-- time) so a search hit is directly renderable without a second
-- lookup per row — the same reasoning Backlink's schema already
-- applies to mention hits.
CREATE TABLE search_documents (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_kind TEXT NOT NULL,       -- 'entity' | 'comment'
    source_id   INTEGER NOT NULL,    -- entities.id or comments.id
    entity_id   INTEGER NOT NULL,    -- owning entity id (see above)
    comment_id  INTEGER,             -- set only for source_kind='comment'
    kind        TEXT NOT NULL,       -- ticket|feature|decision|plan|document|comment
    project_id  INTEGER NOT NULL,
    ref         TEXT NOT NULL,       -- the owning entity's formatted reference
    status      TEXT,                -- workflow/decision status; NULL where not applicable
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    UNIQUE(source_kind, source_id)
);
CREATE INDEX idx_search_documents_entity ON search_documents(entity_id);
CREATE INDEX idx_search_documents_project ON search_documents(project_id);

-- External-content FTS5 table: search_documents holds the columns that
-- matter for filtering (kind/project_id/status) and for rendering a
-- hit; search_fts indexes only the free-text columns actually
-- searched. porter unicode61 stems ("running" matches "run") the same
-- tokenizer choice the spike validated.
CREATE VIRTUAL TABLE search_fts USING fts5(
    title, body,
    content='search_documents', content_rowid='id',
    tokenize='porter unicode61'
);

-- Sync triggers keep search_fts current with search_documents in the
-- same transaction as every write — this is the whole point of the
-- content_rowid pattern: search_fts stores no data of its own, and an
-- external-content table drifts silently the moment a write to the
-- source table isn't mirrored, so every path that touches
-- search_documents needs all three of these, not just INSERT.
CREATE TRIGGER search_documents_ai AFTER INSERT ON search_documents BEGIN
    INSERT INTO search_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
CREATE TRIGGER search_documents_ad AFTER DELETE ON search_documents BEGIN
    INSERT INTO search_fts(search_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
END;
CREATE TRIGGER search_documents_au AFTER UPDATE ON search_documents BEGIN
    INSERT INTO search_fts(search_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
    INSERT INTO search_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
