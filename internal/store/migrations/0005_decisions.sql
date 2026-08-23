-- Phase 3 Step 6: decisions (product spec §5.8), a minimal slice —
-- title, context, decision, rationale, status only. No versioning, no
-- supersession-linking, no diff machinery: this is deliberately just
-- enough to exercise the representative agent workflow's "record a
-- decision" step. Phase 5 extends this same table with those, the way
-- it also fully owns plans/documents (§5.9) as sibling record kinds.
--
-- 1:1 extension of entities, the same pattern ADR 0002 uses for
-- projects/features/tickets. No priority/position columns: decisions
-- aren't ordered or prioritized (§5.8 has no such concept for them).
CREATE TABLE decisions (
    id         INTEGER PRIMARY KEY REFERENCES entities(id),
    project_id INTEGER NOT NULL REFERENCES entities(id),
    seq        INTEGER NOT NULL,
    title      TEXT NOT NULL,
    context    TEXT NOT NULL DEFAULT '',
    decision   TEXT NOT NULL DEFAULT '',
    rationale  TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'proposed'
);
CREATE UNIQUE INDEX idx_decisions_project_seq ON decisions(project_id, seq);
