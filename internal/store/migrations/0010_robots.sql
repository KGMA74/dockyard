-- Robot accounts: non-human credentials for CI pipelines and clusters. They
-- only speak the registry protocol (never the admin API), cannot delete, and
-- authenticate with a generated secret stored as a SHA-256 hash (the secret
-- is 256 random bits, so a slow hash adds nothing).

CREATE TABLE robots (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL UNIQUE,
    description   TEXT    NOT NULL DEFAULT '',
    role          TEXT    NOT NULL CHECK (role IN ('pusher', 'reader')),
    repo_patterns TEXT    NOT NULL DEFAULT '[]',
    secret_hash   TEXT    NOT NULL,
    created_by    TEXT    NOT NULL DEFAULT '',
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    expires_at    TEXT,
    last_used_at  TEXT
);
