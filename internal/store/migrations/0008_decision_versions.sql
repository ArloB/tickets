-- Phase 5 Step 2: decision versioning, diff, and supersession (product
-- spec §5.8). Extends the same `decisions` table Phase 3's minimal
-- slice created (0005_decisions.sql), the way that migration's own
-- comment always said Phase 5 would.

-- `consequences` was named in §5.8's field list ("Title, Markdown
-- context, decision, rationale, and consequences") but Phase 3's slice
-- never added the column — closed here, not carried forward as a gap.
ALTER TABLE decisions ADD COLUMN consequences TEXT NOT NULL DEFAULT '';

-- "An optional link to the decision that supersedes it" (§5.8) — set on
-- the *old* decision, pointing at the *new* one that replaces it.
-- References entities(id) like every other cross-entity pointer in
-- this schema (ADR 0002); resolved to a public reference by
-- internal/service, not joined here (mirrors how derived_mentions'
-- target_entity_id is resolved).
ALTER TABLE decisions ADD COLUMN superseded_by INTEGER REFERENCES entities(id);

-- Every edit archives the pre-update row (§5.8: "accepted decisions
-- remain editable only by creating a new version, and every version
-- remains visible") — the same archive-then-overwrite pattern
-- comment_versions already established (0002_core_domain.sql): version
-- stores the row's version *before* the edit that superseded it, and
-- the live `decisions` row itself represents the current version, not
-- archived until its own next edit.
CREATE TABLE decision_versions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    decision_id  INTEGER NOT NULL REFERENCES entities(id),
    version      INTEGER NOT NULL,
    title        TEXT NOT NULL,
    context      TEXT NOT NULL,
    decision     TEXT NOT NULL,
    rationale    TEXT NOT NULL,
    consequences TEXT NOT NULL,
    status       TEXT NOT NULL,
    edited_by    INTEGER NOT NULL REFERENCES actors(id),
    created_at   TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_decision_versions_decision_version ON decision_versions(decision_id, version);
