-- Vulnerability scans: one row per scan attempt for an image digest, run via
-- an external `trivy server` the operator points Dockyard at.

CREATE TABLE scans (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,               -- repository name
    reference      TEXT    NOT NULL,                -- tag or digest requested
    digest         TEXT    NOT NULL,                -- resolved manifest digest actually scanned
    status         TEXT    NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed')),
    requested_by   TEXT    NOT NULL DEFAULT '',
    trivy_version  TEXT    NOT NULL DEFAULT '',
    -- Summary counts, denormalized for fast list/badge rendering without
    -- parsing report_json.
    critical_count INTEGER NOT NULL DEFAULT 0,
    high_count     INTEGER NOT NULL DEFAULT 0,
    medium_count   INTEGER NOT NULL DEFAULT 0,
    low_count      INTEGER NOT NULL DEFAULT 0,
    unknown_count  INTEGER NOT NULL DEFAULT 0,
    -- Full `trivy image --format json` output, gzip-compressed.
    report_json    BLOB,
    error          TEXT    NOT NULL DEFAULT '',
    started_at     TEXT,
    finished_at    TEXT,
    created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_scans_digest ON scans(digest);
CREATE INDEX idx_scans_name_ref ON scans(name, reference);
CREATE INDEX idx_scans_queue ON scans(status) WHERE status IN ('queued', 'running');
