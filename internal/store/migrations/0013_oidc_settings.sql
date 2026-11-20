-- Single sign-on configured from the console. Ignored when the deployment
-- sets OIDC_ISSUER_URL: the environment then owns the whole configuration.

CREATE TABLE oidc_settings (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    enabled           INTEGER NOT NULL DEFAULT 0,
    issuer_url        TEXT    NOT NULL DEFAULT '',
    client_id         TEXT    NOT NULL DEFAULT '',
    -- AES-256-GCM, keyed from JWT_SECRET; never returned by the API.
    client_secret_enc TEXT    NOT NULL DEFAULT '',
    redirect_url      TEXT    NOT NULL DEFAULT '',
    display_name      TEXT    NOT NULL DEFAULT 'SSO',
    scopes            TEXT    NOT NULL DEFAULT '["openid","profile","email","groups"]',
    username_claim    TEXT    NOT NULL DEFAULT 'preferred_username',
    groups_claim      TEXT    NOT NULL DEFAULT 'groups',
    admin_groups      TEXT    NOT NULL DEFAULT '[]',
    pusher_groups     TEXT    NOT NULL DEFAULT '[]',
    reader_groups     TEXT    NOT NULL DEFAULT '[]',
    default_role      TEXT    NOT NULL DEFAULT 'reader' CHECK (default_role IN ('reader', 'pusher', 'admin', 'none')),
    updated_by        TEXT    NOT NULL DEFAULT '',
    updated_at        TEXT    NOT NULL DEFAULT ''
);
INSERT INTO oidc_settings (id) VALUES (1);
