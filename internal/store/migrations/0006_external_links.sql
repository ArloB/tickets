-- Phase 4: named external links (product spec §5.11's "named external
-- links" half — uploaded/path attachments with content-addressed
-- storage stay Phase 5). Attachable to tickets, features, and
-- decisions. Add/delete only, no in-place edit and no version column:
-- docs/contracts/concurrency.md's exceptions list covers why —
-- matching entity_associations/ticket_relationships rather than
-- comments' own-versioned pattern, since a link is a lightweight
-- annotation, not a first-class versioned record.
CREATE TABLE external_links (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id  INTEGER NOT NULL REFERENCES entities(id),
    title      TEXT NOT NULL,
    url        TEXT NOT NULL,
    created_at TEXT NOT NULL,
    created_by INTEGER NOT NULL REFERENCES actors(id)
);
CREATE INDEX idx_external_links_entity ON external_links(entity_id);
