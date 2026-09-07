# API reference

Full endpoint reference for the admin API, the Docker Registry V2 API, `dockyard-cli`, and the Makefile targets. See the [README](./README.md) for setup and configuration.

## Admin API

All endpoints require `Authorization: Bearer <token>` (except login/logout).

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Health + mode + version, storage probe (`ok`, `latency_ms`, cached counts, `free_bytes` on local), mirror cache counters; `status: degraded` when the backend fails |
| `GET` | `/metrics` | Prometheus metrics (when `METRICS_ENABLED`) |
| `POST` | `/api/admin/auth/login` | Get access + refresh tokens |
| `POST` | `/api/admin/auth/refresh` | Renew the access token (rotates the refresh token) |
| `POST` | `/api/admin/auth/logout` | Revoke token + kill session (persisted) |
| `POST` | `/api/admin/auth/password` | Change own password |
| `GET` | `/api/admin/users` | List users (admin) |
| `POST` | `/api/admin/users` | Create user `{username, password, role, repo_patterns}` (admin) |
| `PUT` | `/api/admin/users/:username` | Update role/patterns/password (admin) |
| `DELETE` | `/api/admin/users/:username` | Delete user (admin, last admin protected) |
| `GET` | `/api/admin/sessions` | List active sessions (admin) |
| `DELETE` | `/api/admin/sessions/:id` | Revoke a session (admin) |
| `GET` | `/api/admin/audit?repo=&actor=&limit=&offset=` | Audit trail: logins, pushes, deletions, GC (admin) |
| `GET`/`POST` | `/api/admin/retention` | List / create retention policies `{repo_pattern, keep_n, unpulled_days, keep_patterns, protected_tags}` (admin) |
| `DELETE` | `/api/admin/retention/:id` | Delete a retention policy (admin) |
| `POST` | `/api/admin/retention/run?dryRun=true` | Preview or apply the retention plan (admin) — also runs daily before the GC |
| `GET` | `/api/admin/repositories/export?name=<repo>` | Export a repository as an OCI image-layout tarball (skopeo/crane-compatible, admin) |
| `POST` | `/api/admin/repositories/import?name=<repo>` | Import an OCI image-layout tarball (admin) |
| `GET`/`POST` | `/api/admin/webhooks` | List / create webhooks `{url, secret, events: [push,delete,retention,gc], format: generic\|slack\|discord}` (admin) |
| `DELETE` | `/api/admin/webhooks/:id` | Delete a webhook (admin) |
| `POST` | `/api/admin/webhooks/:id/test` | Send a synchronous test event (admin) |
| `GET` | `/api/admin/repositories` | List all repositories with tags and last-pushed time |
| `GET` | `/api/admin/repositories/tags?name=<image>` | List tags with digests and push time |
| `GET` | `/api/admin/repositories/manifest?name=<image>&reference=<tag-or-digest>` | Manifest details (size, layers, platforms for multi-arch) |
| `GET` | `/api/admin/repositories/layer?name=<image>&digest=sha256:<hash>` | List the files inside a layer |
| `DELETE` | `/api/admin/repositories/manifests?name=<image>&digest=sha256:<hash>` | Delete a manifest |
| `DELETE` | `/api/admin/repositories?name=<image>` | Delete a repository and all its tags |
| `GET` | `/api/admin/storage/stats` | Storage usage (size, blob count, repo count) |
| `GET` | `/api/admin/storage/tree` | Raw filesystem tree (local only) |
| `POST` | `/api/admin/gc` | Garbage collect unreferenced blobs — `?dryRun=true` previews without deleting |

Example login + authenticated call:

```bash
curl -X POST http://localhost:8080/api/admin/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"changeme123"}'
# → { "token": "eyJhbGci...", "refresh_token": "9f2c...", "role": "admin", "expires_in": 900 }

curl http://localhost:8080/api/admin/repositories \
  -H "Authorization: Bearer eyJhbGci..."
```

Access tokens are valid 15 minutes; the refresh token (single-use, rotated on every call) keeps the session alive 30 days via `/api/admin/auth/refresh`.

## Docker Registry V2 API

Dockyard implements the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/). Standard `docker push` / `docker pull` work out of the box.

| Endpoint | Description |
|---|---|
| `GET /v2/` | Version check |
| `GET /v2/_catalog` | List repositories |
| `GET /v2/<name>/tags/list` | List tags |
| `HEAD\|GET /v2/<name>/manifests/<ref>` | Get manifest |
| `PUT /v2/<name>/manifests/<ref>` | Push manifest |
| `DELETE /v2/<name>/manifests/<digest>` | Delete manifest |
| `HEAD\|GET /v2/<name>/blobs/<digest>` | Get blob |
| `POST /v2/<name>/blobs/uploads/` | Initiate blob upload |
| `PATCH /v2/<name>/blobs/uploads/<uuid>` | Upload blob chunk |
| `PUT /v2/<name>/blobs/uploads/<uuid>` | Commit blob upload |

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
dockyard-cli sessions list
```

Sessions refresh silently (single-use rotating refresh tokens), like the web UI.

## Makefile

```bash
make run       # Start the Go server (reads .env automatically)
make ui        # Build the React UI → internal/ui/dist/
make ui-dev    # Start Vite dev server on :5173 (proxy /api → Go)
make release   # Build UI then compile binary (production)
make build     # Compile binary only (UI must be built first)
make test      # Run all tests with -v
make watch     # Live reload via air
make clean     # Remove binary and reset UI placeholder
```
