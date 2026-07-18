-- database: :memory:
-- Initial schema: users, sessions, revoked_tokens, audit_log.
-- Timestamps are stored as UTC RFC3339 text.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'pusher', 'reader')),
    -- JSON array of repository glob patterns the user is restricted to;
    -- empty array means no restriction.
    repo_patterns TEXT    NOT NULL DEFAULT '[]',
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE sessions (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash TEXT    NOT NULL UNIQUE,
    user_agent         TEXT    NOT NULL DEFAULT '',
    ip                 TEXT    NOT NULL DEFAULT '',
    created_at         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at         TEXT    NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- Access tokens revoked before their natural expiry (logout); pruned on TTL.
CREATE TABLE revoked_tokens (
    token_hash TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX idx_revoked_tokens_expires_at ON revoked_tokens(expires_at);

CREATE TABLE audit_log (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    actor     TEXT NOT NULL DEFAULT '',
    action    TEXT NOT NULL,
    repo      TEXT NOT NULL DEFAULT '',
    tag       TEXT NOT NULL DEFAULT '',
    source_ip TEXT NOT NULL DEFAULT '',
    result    TEXT NOT NULL DEFAULT '',
    details   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_log_at ON audit_log(at);
CREATE INDEX idx_audit_log_repo ON audit_log(repo);
