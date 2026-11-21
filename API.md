# API reference

Endpoint reference for the admin API, the Docker Registry V2 API, `dockyard-cli`, and the Makefile targets. See the [README](./README.md) for setup and configuration.

The machine-readable contract is [`api/openapi.yaml`](./api/openapi.yaml) (OpenAPI 3): request and response schemas for every admin endpoint. The console's TypeScript types are generated from it, and CI fails when they drift apart.

## Admin API

Every endpoint needs `Authorization: Bearer <token>` except login, refresh, logout and the public SSO endpoints. **(admin)** marks endpoints reserved to the admin role; the others are open to any signed-in user, within their repository patterns.

### Health and metrics

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Health + mode + version, storage probe (`ok`, `latency_ms`, cached counts, `free_bytes` on local), mirror cache counters; `status: degraded` when the backend fails |
| `GET` | `/metrics` | Prometheus metrics (when `METRICS_ENABLED`) |

### Authentication and your account

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/admin/auth/login` | Get access + refresh tokens |
| `POST` | `/api/admin/auth/refresh` | Renew the access token (rotates the refresh token) |
| `POST` | `/api/admin/auth/logout` | Revoke token + kill session (persisted) |
| `POST` | `/api/admin/auth/password` | Change own password |
| `GET` | `/api/admin/auth/me` | The signed-in account as the server sees it |
| `POST` / `DELETE` | `/api/admin/auth/cli-secret` | Issue (or replace) / revoke your CLI secret — a `dkc_…` token for `docker login`, returned once; the only registry credential of an SSO account |
| `GET` | `/api/admin/sessions/mine` | Your active sessions |
| `DELETE` | `/api/admin/sessions/mine/:id` | Revoke one of your sessions |
| `GET` | `/api/admin/events` | Server-sent events: pushes, deletions, scans, GC… (`?token=` accepted, as `EventSource` cannot set headers) |

### Single sign-on (OpenID Connect)

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/admin/auth/oidc` | Public: whether SSO is on, to show the sign-in button |
| `GET` | `/api/admin/auth/oidc/login?return=/path` | Public: redirects to the identity provider |
| `GET` | `/api/admin/auth/oidc/callback?code=&state=` | Public: the provider's redirect target |
| `POST` | `/api/admin/auth/oidc/exchange` | Public: trades the one-time code from the callback for a session `{code}` |
| `GET` / `PUT` | `/api/admin/sso` | Read / save and apply the SSO configuration (admin) |
| `POST` | `/api/admin/sso/test` | Test an issuer's discovery document `{issuer_url}` (admin) |

### Users, robots and sessions

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/admin/users` | List users (admin) |
| `POST` | `/api/admin/users` | Create user `{username, password, role, repo_patterns}` (admin) |
| `PUT` | `/api/admin/users/:username` | Update role/patterns/password (admin) |
| `DELETE` | `/api/admin/users/:username` | Delete user (admin, last admin and self-deletion refused) |
| `GET` | `/api/admin/robots` | List robot accounts (admin) |
| `POST` | `/api/admin/robots` | Create a robot `{name, description, role, repo_patterns, expires_in_days}` — registry-only credentials `robot+<name>`, secret returned once (admin) |
| `POST` | `/api/admin/robots/:id/secret` | Rotate a robot's secret `{expires_in_days}` (admin) |
| `DELETE` | `/api/admin/robots/:id` | Delete a robot; its registry tokens stop working at once (admin) |
| `GET` | `/api/admin/sessions` | List all active sessions (admin) |
| `DELETE` | `/api/admin/sessions/:id` | Revoke a session (admin) |
| `GET` | `/api/admin/audit?repo=&actor=&limit=&offset=` | Audit trail: logins, pushes, deletions, GC, scan exports… (admin) |
| `GET` | `/api/admin/config` | Read-only summary of the server configuration, secrets left out (admin) |

### Repositories and storage

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/admin/repositories` | List all repositories with tags and last-pushed time |
| `GET` | `/api/admin/repositories/search?q=&signed=&limit=&offset=` | Search repositories and tags server-side |
| `GET` | `/api/admin/repositories/tags?name=<image>` | List tags with digests and push time |
| `GET` | `/api/admin/repositories/manifest?name=<image>&reference=<tag-or-digest>` | Manifest details (size, layers, platforms for multi-arch) |
| `GET` | `/api/admin/repositories/diff?name=<image>&reference_a=<ref>&reference_b=<ref>` | Compare two tags: layers only in A, only in B, shared, and the size delta |
| `GET` | `/api/admin/repositories/layer?name=<image>&digest=sha256:<hash>` | List the files inside a layer |
| `DELETE` | `/api/admin/repositories/manifests?name=<image>&digest=sha256:<hash>` | Delete a manifest (admin) |
| `DELETE` | `/api/admin/repositories?name=<image>` | Delete a repository and all its tags (admin) |
| `GET` | `/api/admin/repositories/export?name=<repo>` | Export a repository as an OCI image-layout tarball (skopeo/crane-compatible, admin) |
| `POST` | `/api/admin/repositories/import?name=<repo>` | Import an OCI image-layout tarball (admin) |
| `GET` | `/api/admin/storage/stats` | Storage usage (size, blob count, repo count) |
| `GET` | `/api/admin/storage/tree` | Raw filesystem tree (local only, admin) |
| `GET` | `/api/admin/insights` | Storage growth history and largest repositories (admin, embedded/mirror) |
| `POST` | `/api/admin/gc` | Garbage collect unreferenced blobs — `?dryRun=true` previews without deleting (admin) |

### Vulnerability scanning and policy

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/admin/scans` | Scan an image `{name, reference}`; a scan of the same digest within `SCAN_DEDUP_WINDOW` is reused (admin) |
| `GET` | `/api/admin/scans?name=&digest=&limit=&offset=` | List scans (admin) |
| `GET` | `/api/admin/scans/:id` | One scan: status, counts by severity, Trivy version (admin) |
| `GET` | `/api/admin/scans/:id/report` | The raw Trivy JSON report (admin) |
| `GET` | `/api/admin/scans/:id/report?format=pdf\|csv\|json&lang=en\|fr` | Download the report: a PDF to share, the findings as CSV, or the raw JSON — `Content-Disposition: attachment`, named after the image; each download is audited (admin) |
| `GET` / `PUT` | `/api/admin/vulnerability-policy` | Read / set `{scan_on_push, block_severity}`; the GET also returns the allowlist (admin) |
| `POST` | `/api/admin/vulnerability-policy/allowlist` | Accept a CVE `{cve_id, reason, expires_in_days}` — it no longer blocks pulls until it expires (admin) |
| `DELETE` | `/api/admin/vulnerability-policy/allowlist/:cve` | Remove a CVE from the allowlist (admin) |

Download a report from CI:

```bash
curl -fsSL -H "Authorization: Bearer $TOKEN" -OJ \
  "https://registry.example.com/api/admin/scans/42/report?format=pdf&lang=en"
# → dockyard-scan-team-app-1.0.pdf
```

### Signing, quotas, retention, replication, webhooks

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/admin/signing` | Cosign enforcement status and loaded public keys (admin) |
| `GET` / `POST` | `/api/admin/signing/policies` | List / create signed-push policies `{repo_pattern, required}` (admin) |
| `DELETE` | `/api/admin/signing/policies/:id` | Delete a signing policy (admin) |
| `GET` | `/api/admin/quotas` | List byte quotas with current usage (admin) |
| `PUT` | `/api/admin/quotas` | Set a quota `{scope_type: repo\|user, scope_value, max_bytes, warn_percent}` (admin) |
| `DELETE` | `/api/admin/quotas/:id` | Delete a quota (admin) |
| `POST` | `/api/admin/quotas/usage/reset` | Recompute a scope's usage `{scope_type, scope_value}` (admin) |
| `GET` / `POST` | `/api/admin/retention` | List / create retention policies `{repo_pattern, keep_n, unpulled_days, keep_patterns, protected_tags}` (admin) |
| `DELETE` | `/api/admin/retention/:id` | Delete a retention policy (admin) |
| `POST` | `/api/admin/retention/run?dryRun=true` | Preview or apply the retention plan (admin) — also runs daily before the GC |
| `GET` / `POST` | `/api/admin/replication/targets` | List / create replication targets `{name, base_url, username, password, repo_pattern}` — pushes are copied there (admin) |
| `DELETE` | `/api/admin/replication/targets/:id` | Delete a replication target (admin) |
| `POST` | `/api/admin/replication/targets/:id/test` | Ping the target's `/v2/` endpoint and report reachability, without replicating anything (admin) |
| `GET` / `POST` | `/api/admin/webhooks` | List / create webhooks `{url, secret, events: [push,delete,retention,gc], format: generic\|slack\|discord}` (admin) |
| `DELETE` | `/api/admin/webhooks/:id` | Delete a webhook (admin) |
| `POST` | `/api/admin/webhooks/:id/test` | Send a synchronous test event (admin) |

Example login + authenticated call:

```bash
curl -X POST http://localhost:8080/api/admin/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"changeme123"}'
# → { "token": "<access token>", "refresh_token": "9f2c...", "role": "admin", "expires_in": 900 }

curl http://localhost:8080/api/admin/repositories \
  -H "Authorization: Bearer <access token>"
```

Access tokens are valid 15 minutes; the refresh token (single-use, rotated on every call) keeps the session alive 30 days via `/api/admin/auth/refresh`.

## Docker Registry V2 API

Dockyard implements the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/). Standard `docker push` / `docker pull` work out of the box.

| Endpoint | Description |
|---|---|
| `GET /v2/token` | Docker token auth: trades Basic credentials (password, CLI secret or robot secret) for a short-lived registry token; with `V2_ANONYMOUS_PULL`, a pull-only token without credentials |
| `GET /v2/` | Version check |
| `GET /v2/_catalog` | List repositories (filtered to the caller's repository patterns) |
| `GET /v2/<name>/tags/list` | List tags |
| `HEAD\|GET /v2/<name>/manifests/<ref>` | Get manifest |
| `PUT /v2/<name>/manifests/<ref>` | Push manifest |
| `DELETE /v2/<name>/manifests/<digest>` | Delete manifest |
| `HEAD\|GET /v2/<name>/blobs/<digest>` | Get blob |
| `POST /v2/<name>/blobs/uploads/` | Initiate blob upload |
| `PATCH /v2/<name>/blobs/uploads/<uuid>` | Upload blob chunk |
| `PUT /v2/<name>/blobs/uploads/<uuid>` | Commit blob upload |

Registry tokens only work on `/v2/*`: the admin API refuses them.

## dockyard-cli

A command-line client for the admin API (binaries attached to GitHub releases, or `go build ./cmd/dockyard-cli`):

```bash
dockyard-cli login https://registry.example.com -u admin -p …   # session in ~/.dockyard/config.json
dockyard-cli repos                       # list repositories
dockyard-cli tags team/app               # tags + digests
dockyard-cli delete team/app v1          # delete one manifest (resolves the tag)
dockyard-cli gc --dry-run                # preview the garbage collection
dockyard-cli export team/app -o app.oci.tar    # OCI image-layout dump
dockyard-cli import team/app -i app.oci.tar
dockyard-cli users create ci --role pusher -p … --repos "team/*"
dockyard-cli users list / users delete <username>
dockyard-cli sessions list / sessions revoke <id>
```

`login` also reads the password from `DOCKYARD_PASSWORD` when `-p` is not given, which keeps it out of the shell history. Sessions refresh silently (single-use rotating refresh tokens), like the web UI.

## Makefile

```bash
make run       # Start the Go server (reads .env automatically)
make ui        # Build the React UI → internal/ui/dist/
make ui-dev    # Start Vite dev server on :5173 (proxy /api → Go)
make release   # Build UI then compile binary (production)
make build     # Compile binary only (UI must be built first)
make test      # Run all tests with -v
make lint      # golangci-lint
make watch     # Live reload via air
make clean     # Remove binary and reset UI placeholder
```
