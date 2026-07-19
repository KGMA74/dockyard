-- Periodic storage snapshots feeding the "storage insights" view (growth over
-- time). Sampled every 6 hours by the server; pruned after 90 days.

CREATE TABLE stats_history (
    at         TEXT    PRIMARY KEY,
    total_size INTEGER NOT NULL,
    blob_count INTEGER NOT NULL,
    repo_count INTEGER NOT NULL
) WITHOUT ROWID;
