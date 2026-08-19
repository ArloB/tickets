-- Phase 0 vertical slice schema. Deliberately minimal: only what Step 5's
-- three endpoints (project create/get, ticket create/get/update) need.
-- No actors, no audit_events, no comments, no attachments — those wait
-- for the Phase 1+ work that can validate their real shape (see the
-- Phase 0 implementation plan's deferral list, and ADR 0002/0009).

-- Shared entity registry (ADR 0002). `id` is an internal-only surrogate
-- key (AUTOINCREMENT so a purged row's id is never reused, which would
-- otherwise silently re-point old references once admin purge exists —
-- product spec §5.12); `uuid` is the canonical public identity (§5.2).
-- `project_id` is NULL only for a project's own entities row.
CREATE TABLE entities (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid       BLOB(16) NOT NULL,
    project_id INTEGER REFERENCES entities(id),
    kind       TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX idx_entities_uuid ON entities(uuid);
CREATE INDEX idx_entities_project_kind ON entities(project_id, kind);

CREATE TABLE projects (
    id                 INTEGER PRIMARY KEY REFERENCES entities(id),
    key                TEXT NOT NULL,
    title              TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'active',
    general_feature_id INTEGER REFERENCES entities(id)
);
CREATE UNIQUE INDEX idx_projects_key ON projects(key);

CREATE TABLE features (
    id         INTEGER PRIMARY KEY REFERENCES entities(id),
    project_id INTEGER NOT NULL REFERENCES entities(id),
    seq        INTEGER NOT NULL,
    title      TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'backlog'
);
CREATE UNIQUE INDEX idx_features_project_seq ON features(project_id, seq);

CREATE TABLE tickets (
    id          INTEGER PRIMARY KEY REFERENCES entities(id),
    project_id  INTEGER NOT NULL REFERENCES entities(id),
    feature_id  INTEGER NOT NULL REFERENCES entities(id),
    seq         INTEGER NOT NULL,
    type        TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'backlog',
    priority    TEXT NOT NULL DEFAULT 'medium',
    severity    TEXT
);
CREATE UNIQUE INDEX idx_tickets_project_seq ON tickets(project_id, seq);

-- Public reference allocation (ADR 0009): one row per (project, kind),
-- incremented in the same transaction as the entity it names.
CREATE TABLE reference_counters (
    project_id INTEGER NOT NULL REFERENCES entities(id),
    kind       TEXT NOT NULL,
    next_seq   INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (project_id, kind)
);

-- Idempotency-key records (ADR 0008 / docs/contracts/concurrency.md).
-- ref_key is the created record's stable reference (a project key or a
-- ticket ref) - deliberately NOT a serialized snapshot of the response.
-- A cache hit re-fetches the live record via that reference, so fields
-- tagged json:"-" (e.g. domain.Ticket.UUID) and any field a later phase
-- adds both survive a replay instead of silently reverting to whatever
-- a stale snapshot happened to contain (see Phase 0 Step 5 review).
-- Bounded retention is enforced by application-level cleanup, not a
-- schema constraint, in this phase.
CREATE TABLE idempotency_keys (
    key           TEXT NOT NULL PRIMARY KEY,
    fingerprint   TEXT NOT NULL,
    ref_key       TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
