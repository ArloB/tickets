-- Phase 2: identity and auth (ADR 0004). Human accounts, sessions,
-- agent bearer tokens, and login-attempt records for throttling — the
-- schema requestActor(r)/mcpActor()'s real implementations resolve
-- against, replacing the two hardcoded actors migration
-- 0002_core_domain.sql seeded as placeholders ('system' stays a fixed
-- actor per §4.1; 'local' becomes an ordinary human_accounts row via
-- `tickets setup`, not a special case).

-- actors (migration 0002_core_domain.sql) has no description column —
-- an oversight caught while building the admin agent-management
-- endpoints, not a deliberate Phase 1 omission: product spec §4.1
-- explicitly says an agent "has a name, optional description, owning
-- human, and one or more revocable API tokens." Only agents use it in
-- practice (humans/system leave it '' ), but it lives on the shared
-- actors table rather than a kind-specific one, matching how owner_id
-- is likewise nullable-and-agent-only on the same shared table.
ALTER TABLE actors ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- One human_accounts row per human actor (1:1 extension of actors,
-- the same pattern ADR 0002 uses for entities/projects/tickets/etc.,
-- even though actors itself sits outside the entities registry — see
-- 0002_core_domain.sql's actors table comment). password_hash is a
-- self-describing Argon2id-encoded string (algorithm, version,
-- params, salt, and hash all in one value), so future parameter
-- tuning doesn't invalidate hashes already stored
-- (internal/auth.HashPassword). is_admin is the operational flag
-- product spec §4.2 keeps orthogonal to the flat viewer/editor
-- content-permission model — it gates account/agent/token/server-
-- status administration, never ordinary project content.
CREATE TABLE human_accounts (
    actor_id      INTEGER PRIMARY KEY REFERENCES actors(id),
    username      TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_human_accounts_username ON human_accounts(username);

-- Sessions back the browser-facing cookie (ADR 0004: "secure, HTTP-
-- only, same-site session cookie"). id is the cookie's own value,
-- stored raw here — unlike agent_tokens.token_hash below. That
-- asymmetry is deliberate, not an oversight: a session is short-lived,
-- scoped to one cookie, and never logged or exported the way a
-- long-lived agent token might be, so hashing it would add a lookup
-- step to every authenticated request for little real benefit.
-- csrf_token is a second, independent random value handed to the
-- client at login and required on every cookie-authenticated mutating
-- request (ADR 0004: "session security (CSRF, throttling, expiry)").
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    actor_id     INTEGER NOT NULL REFERENCES actors(id),
    csrf_token   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);
CREATE INDEX idx_sessions_actor ON sessions(actor_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- Agent bearer tokens (ADR 0004: "only a token's hash is ever stored;
-- the raw value is shown once at creation"). One agent actor can hold
-- several tokens (product spec §4.1), each independently revocable —
-- id, not actor_id, is what a revoke call names. expires_at is
-- nullable: an optional expiry (product spec §10); revoked_at is set
-- once, permanently, by an explicit revoke — there is no "un-revoke".
CREATE TABLE agent_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id    INTEGER NOT NULL REFERENCES actors(id),
    token_hash  TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    expires_at  TEXT,
    revoked_at  TEXT
);
CREATE UNIQUE INDEX idx_agent_tokens_hash ON agent_tokens(token_hash);
CREATE INDEX idx_agent_tokens_actor ON agent_tokens(actor_id);

-- Login-attempt log backing internal/auth's DB-persisted throttle
-- (chosen over an in-memory counter so a restart doesn't hand an
-- attacker a fresh window). Every attempt is recorded, success or
-- failure — a successful login isn't deleted from the log, since the
-- throttle only ever counts failures in a trailing window, and an
-- unbounded intermixed log is simpler than pruning successes out.
CREATE TABLE login_attempts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT NOT NULL,
    ip         TEXT NOT NULL,
    succeeded  INTEGER NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_login_attempts_username_time ON login_attempts(username, created_at);
CREATE INDEX idx_login_attempts_ip_time ON login_attempts(ip, created_at);

-- Widen idempotency_keys' primary key from (key) to (key, actor_id)
-- (ADR 0008): two different actors reusing the same client-chosen key
-- must not collide, or silently hand one actor's created record back
-- to the other. Nothing references idempotency_keys via a foreign
-- key, so a straight drop-and-recreate is safe without a
-- foreign_keys=OFF/rename dance. Existing rows all predate real actor
-- attribution (every Phase 0/1 write used the seeded 'local' actor)
-- and are dropped outright rather than backfilled — idempotency_keys
-- is a bounded-retention cache by ADR 0008's own design; nothing in it
-- is worth preserving across this schema change.
DROP TABLE idempotency_keys;
CREATE TABLE idempotency_keys (
    key         TEXT NOT NULL,
    actor_id    INTEGER NOT NULL REFERENCES actors(id),
    fingerprint TEXT NOT NULL,
    ref_key     TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (key, actor_id)
);
