-- Byte quotas per repository or per user, enforced at blob-upload commit
-- (embedded mode only — proxy/mirror mode doesn't own the storage writes).
-- quota_usage tracks cumulative pushed bytes per scope; it is not
-- decremented by GC or manual deletion (it measures push volume against
-- the quota, not live on-disk size), and can be reset by an admin.

CREATE TABLE quotas (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_type   TEXT    NOT NULL CHECK (scope_type IN ('repo', 'user')),
    scope_value  TEXT    NOT NULL,
    max_bytes    INTEGER NOT NULL,
    warn_percent INTEGER NOT NULL DEFAULT 90,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (scope_type, scope_value)
);

CREATE TABLE quota_usage (
    scope_type  TEXT    NOT NULL CHECK (scope_type IN ('repo', 'user')),
    scope_value TEXT    NOT NULL,
    bytes_used  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (scope_type, scope_value)
);
