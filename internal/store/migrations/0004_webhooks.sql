-- Webhooks: subscriptions + an at-least-once delivery outbox with
-- exponential-backoff retries.

CREATE TABLE webhooks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    url        TEXT    NOT NULL,
    -- HMAC-SHA256 signing secret for the X-Dockyard-Signature header ('' = unsigned).
    secret     TEXT    NOT NULL DEFAULT '',
    -- JSON array of event types the hook wants: ["push","delete","retention","gc"].
    events     TEXT    NOT NULL DEFAULT '["push"]',
    -- generic | slack | discord — payload shape.
    format     TEXT    NOT NULL DEFAULT 'generic' CHECK (format IN ('generic', 'slack', 'discord')),
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE webhook_deliveries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id      INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    -- The serialized event payload to POST.
    payload         TEXT    NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    delivered_at    TEXT,
    last_error      TEXT    NOT NULL DEFAULT '',
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_webhook_deliveries_due ON webhook_deliveries(next_attempt_at) WHERE delivered_at IS NULL;
