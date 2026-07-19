-- Last-pull tracking per (repo, reference) — feeds age-based retention
-- policies. Written by an async batcher, so the pull hot path never waits on
-- SQLite.

CREATE TABLE last_pulls (
    repo           TEXT    NOT NULL,
    reference      TEXT    NOT NULL,
    last_pulled_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    pull_count     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (repo, reference)
) WITHOUT ROWID;
CREATE INDEX idx_last_pulls_at ON last_pulls(last_pulled_at);
