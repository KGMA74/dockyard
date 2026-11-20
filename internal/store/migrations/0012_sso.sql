-- Single sign-on (OIDC). Accounts created by a first SSO login are
-- 'oidc' accounts: no password, matched on the identity provider's subject
-- (issuer + sub) rather than the username, which an IdP may let users change.
--
-- cli_secret_hash: a generated secret any account can use as its docker
-- login password — the only way for an SSO account, which has no password,
-- and a better one than a person's real password in a CLI config.

ALTER TABLE users ADD COLUMN auth_source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
ALTER TABLE users ADD COLUMN cli_secret_hash TEXT;
CREATE UNIQUE INDEX idx_users_oidc_subject ON users(oidc_subject) WHERE oidc_subject IS NOT NULL;
