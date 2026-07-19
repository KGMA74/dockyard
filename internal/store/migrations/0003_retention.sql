-- Retention policies: automatic tag cleanup rules, evaluated daily alongside
-- the GC and on demand via the admin API.

CREATE TABLE retention_policies (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    -- Which repositories the policy covers ('*' spans slashes).
    repo_pattern   TEXT    NOT NULL DEFAULT '*',
    -- Keep only the N most recently pushed tags (0 = rule disabled).
    keep_n         INTEGER NOT NULL DEFAULT 0,
    -- Delete tags not pulled for this many days (0 = rule disabled).
    unpulled_days  INTEGER NOT NULL DEFAULT 0,
    -- JSON array of tag glob patterns that are always kept (e.g. ["v*", "latest"]).
    keep_patterns  TEXT    NOT NULL DEFAULT '[]',
    -- JSON array of exact tag names that are always kept.
    protected_tags TEXT    NOT NULL DEFAULT '[]',
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
