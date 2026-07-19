-- Per-repository overrides of REQUIRE_SIGNED_PUSH. The first pattern
-- matching a repository wins (evaluated in id order); no match falls back
-- to the global default.

CREATE TABLE signing_policies (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_pattern TEXT    NOT NULL,
    required     INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
