-- Push-based replication targets: on each tag push, the pushed repo/tag is
-- queued for copy to every enabled target whose repo_pattern matches, via
-- the same at-least-once outbox pattern as webhook_deliveries.
-- Credentials are stored in plaintext, same tradeoff as webhooks.secret.

CREATE TABLE replication_targets (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL UNIQUE,
    base_url     TEXT    NOT NULL,
    username     TEXT    NOT NULL DEFAULT '',
    password     TEXT    NOT NULL DEFAULT '',
    repo_pattern TEXT    NOT NULL DEFAULT '*',
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE replication_deliveries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    target_id       INTEGER NOT NULL REFERENCES replication_targets(id) ON DELETE CASCADE,
    repo            TEXT    NOT NULL,
    tag             TEXT    NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    delivered_at    TEXT,
    last_error      TEXT,
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
