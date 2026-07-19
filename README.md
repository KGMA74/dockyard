# Dockyard — Self-hosted Docker Registry

A lightweight, self-hosted Docker Registry V2 server written in Go. Ships as a **single binary** with an embedded React UI — no external dependencies for local mode.

> Built on the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/) specification — the same protocol implemented by [distribution/distribution](https://github.com/distribution/distribution).

## Features

- **Embedded UI** — React + Tailwind (shadcn/ui) dashboard served directly by the binary, with dark/light/system theme
- **Multi-arch aware** — manifest lists (OCI indexes) are resolved per-platform, so multi-arch images show their real total size instead of 0
- **Layer browser** — inspect the files inside any layer (path, size, symlinks) without pulling the image
- **Repository and tag deletion** — remove a single tag or an entire repository from the UI or API
- **Two storage backends** — local filesystem or any S3-compatible object store
- **Two modes** — embedded registry or proxy in front of an existing registry
- **Garbage collection** — manual trigger via UI or API, automatic daily cron at midnight UTC
- **Vulnerability scanning** — trigger a Trivy scan on any pushed image via the admin API (`SCAN_ENABLED` + `TRIVY_SERVER_URL`), results stored with severity counts and the full report
- **JWT auth** on the admin API, optional Basic Auth on `/v2/*`
- **Structured JSON logging** via `log/slog`
- **Single Docker image** — multi-stage build, final image from `scratch`

---

## Modes

| Mode | Description |
|---|---|
| `embedded` | Dockyard **is** the registry — stores blobs and manifests itself |
| `proxy` | Dockyard sits in front of an existing registry and exposes the admin UI/API |
| `mirror` | Pull-through cache: serves from local storage, fetches misses from `REGISTRY_URL` (write-through). Tags are revalidated upstream after `MIRROR_TAG_TTL` (default 5m); blobs are immutable and never re-fetched; if the upstream is down, cached content keeps being served. Also accepts direct pushes. |

---

## Getting Started

### Prerequisites

- Go 1.21+
- Node.js 22+ (to build the UI)
- Docker (to push/pull images)

### Run locally (dev)

```bash
cp .env.example .env   # adjust values as needed

# Terminal 1 — Go server
make run

# Terminal 2 — Vite dev server with hot reload (proxies /api → Go)
make ui-dev
```

Open `http://localhost:5173` for the UI with hot reload, or `http://localhost:<PORT>` to use the embedded UI.

### Build production binary

```bash
make release   # builds UI then embeds it in the Go binary → dockyard.exe
```

Or step by step:
```bash
make ui      # npm ci + vite build → internal/ui/dist/
make build   # go build with embedded UI
```

### Docker

Pull the pre-built image from GitHub Container Registry:

```bash
docker pull ghcr.io/kgma74/dockyard:latest
docker run -p 8080:8080 --env-file .env ghcr.io/kgma74/dockyard:latest
```

Or build locally:

```bash
docker build -t dockyard .
docker run -p 8080:8080 --env-file .env dockyard
```

The Dockerfile uses a **3-stage build**: Node.js → Go → scratch. The final image contains only the statically linked binary with the UI embedded inside. Multi-arch: `linux/amd64` and `linux/arm64`.

### Helm (Kubernetes)

```bash
helm upgrade --install dockyard oci://ghcr.io/kgma74/charts/dockyard \
  --namespace registry --create-namespace \
  --set auth.password=changeme
```

See `helm/dockyard/values.yaml` for the full set of options (storage backend, S3, ingress, proxy mode, etc.).

---

## Configuration

All settings are loaded from `.env` (or environment variables).

```env
PORT=8080
REGISTRY_MODE=embedded          # embedded | proxy

# ── Storage ───────────────────────────────────────────────────────────────────
REGISTRY_STORAGE_BACKEND=local  # local | s3
REGISTRY_STORAGE_PATH=./data/registry

# S3 / MinIO / RustFS (when REGISTRY_STORAGE_BACKEND=s3)
S3_ENDPOINT=http://rustfs:9000
S3_ACCESS_KEY=your-access-key
S3_SECRET_KEY=your-secret-key
S3_BUCKET=dockyard-registry
S3_REGION=us-east-1
S3_SECURE=false                 # true if TLS is enabled on S3

# ── Auth ──────────────────────────────────────────────────────────────────────
AUTH_USERNAME=admin
AUTH_PASSWORD=changeme123       # initial password (first startup only)
JWT_SECRET=change-this-to-a-long-random-secret
# JWT_SECRET_PREVIOUS=          # old secret during a rotation grace window

# Auth on /v2/* — default is now true (Docker token auth + Basic fallback).
# Set to false for an open registry (dev only).
V2_AUTH_ENABLED=false
# Allow unauthenticated pulls while pushes require login (public-read registry)
# V2_ANONYMOUS_PULL=true

# ── Hardening ─────────────────────────────────────────────────────────────────
# Brute-force throttle on login + /v2/token, per client IP (0 disables)
# RATE_LIMIT_LOGIN_PER_MIN=10
# Loose per-IP requests/second cap on everything else (0 disables)
# RATE_LIMIT_GLOBAL_RPS=100
# CORS is OFF by default (the UI is embedded, same-origin). Open the API to
# external browser clients with a comma-separated origin list:
# CORS_ALLOWED_ORIGINS=https://tools.example.com

# ── Native TLS (for deployments without a reverse proxy) ──────────────────────
# off (default) · static (TLS_CERT_FILE/TLS_KEY_FILE) · self-signed
# (autogenerated, persisted under <storage>/tls) · acme (Let's Encrypt,
# TLS-ALPN — needs :443 reachable from the internet and TLS_DOMAIN)
# TLS_MODE=self-signed
# TLS_DOMAIN=registry.example.com
# TLS_ACME_EMAIL=ops@example.com
# TLS_CERT_FILE= / TLS_KEY_FILE=   (static mode)
# Self-signed note: docker clients need the registry in "insecure-registries",
# or the cert (<storage>/tls/cert.pem) added to their trust store.

# ── Observability ─────────────────────────────────────────────────────────────
# Prometheus metrics on /metrics (unauthenticated — keep the port private or
# disable): HTTP by normalized route, storage gauges, GC runs, mirror cache.
# METRICS_ENABLED=true

# ── Vulnerability scanning (Trivy) ────────────────────────────────────────────
# Off by default. Dockyard shells out to the `trivy` binary bundled in its own
# image (--server mode) against an operator-managed `trivy server --listen`
# instance, which hosts the vulnerability DB. Dockyard does not run Trivy
# itself. Trigger a scan via POST /api/admin/scans {"name","reference"}.
# SCAN_ENABLED=true
# TRIVY_SERVER_URL=http://trivy:4954
# TRIVY_BIN_PATH=/trivy                # path to the trivy binary in the image
# SCAN_TIMEOUT=5m                      # per-scan subprocess timeout
# SCAN_MAX_REPORT_BYTES=20971520       # cap on the raw trivy JSON report (20 MiB)
# SCAN_DEDUP_WINDOW=1h                 # reuse a recent successful scan for the same digest
# TRIVY_INSECURE_REGISTRY=true         # trivy pulls from Dockyard over plain HTTP on localhost

# ── Proxy mode ────────────────────────────────────────────────────────────────
# REGISTRY_MODE=proxy
# REGISTRY_URL=http://your-registry:5000
# REGISTRY_USERNAME=
# REGISTRY_PASSWORD=

# ── Mirror mode (pull-through cache) ─────────────────────────────────────────
# REGISTRY_MODE=mirror
# REGISTRY_URL=https://registry-1.docker.io    # upstream to cache (Docker Hub,
#                                              # ghcr, quay — token auth handled)
# REGISTRY_USERNAME= / REGISTRY_PASSWORD=      # upstream creds (higher rate limits)
# MIRROR_TAG_TTL=5m                            # tag revalidation interval
# Example: docker pull host:8080/library/alpine:latest caches from Docker Hub.
```

---

## Storage Backends

### Local (default)

Stores everything on the local filesystem. Ideal for development and single-node setups.

```env
REGISTRY_STORAGE_BACKEND=local
REGISTRY_STORAGE_PATH=./data/registry
```

Storage layout:
```
data/registry/
├── blobs/sha256/<2-char>/<digest>/data
├── repositories/<name>/manifests/<digest>
├── repositories/<name>/tags/<tag>
└── uploads/<uuid>/data
```

### S3 / MinIO / RustFS

Object storage compatible with the S3 API. No PVC required — ideal for Kubernetes and production deployments.

```env
REGISTRY_STORAGE_BACKEND=s3
S3_ENDPOINT=http://rustfs:9000
S3_ACCESS_KEY=your-access-key
S3_SECRET_KEY=your-secret-key
S3_BUCKET=dockyard-registry
S3_REGION=us-east-1
S3_SECURE=false
```

The bucket is created automatically on first startup if it does not exist.

Compatible with any S3-compatible service: **RustFS**, **MinIO**, **AWS S3** (omit `S3_ENDPOINT` for AWS), Ceph, Backblaze B2, etc.

> GC (`POST /api/admin/gc`) and storage tree (`GET /api/admin/storage/tree`) are only available with the local backend.

---

## Authentication

### Admin API (`/api/admin/*`) — JWT

```bash
# Login
curl -X POST http://localhost:8080/api/admin/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"changeme123"}'
# → { "token": "eyJhbGci...", "refresh_token": "9f2c...", "role": "admin", "expires_in": 900 }

# Use the access token
curl http://localhost:8080/api/admin/repositories \
  -H "Authorization: Bearer eyJhbGci..."

# Renew it (refresh tokens are single-use and rotated on every call)
curl -X POST http://localhost:8080/api/admin/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"9f2c..."}'
```

Access tokens are valid for **15 minutes**; the refresh token keeps the session alive for **30 days**. Logout revokes the access token (persisted in SQLite — it survives restarts) and kills the session.

```bash
# Change password
curl -X POST http://localhost:8080/api/admin/auth/password \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"current_password":"changeme123","new_password":"newpassword"}'
```

### Users & roles

Accounts live in SQLite (`<storage>/dockyard.db`) with three roles: **admin** (everything), **pusher** (pull + push), **reader** (read-only). Optional `repo_patterns` globs restrict a user to matching repositories (`team-a/*` — `*` also crosses `/`). User and session management (`/api/admin/users`, `/api/admin/sessions`) is admin-only; deleting mutations (`DELETE`, `/gc`) require the admin role.

On the first boot after upgrading, the legacy single admin is migrated automatically — a password previously changed at runtime (stored in `auth/password.bcrypt`) is preserved. `AUTH_PASSWORD` is only used when the users table is empty.

> Rotate `JWT_SECRET` without logging everyone out: set the new value in `JWT_SECRET`, put the old one in `JWT_SECRET_PREVIOUS` during a grace window, then remove it.

### Registry V2 (`/v2/*`) — Docker token auth

> **Breaking change:** `/v2/*` now requires authentication **by default** (`V2_AUTH_ENABLED=true`). Set it to `false` to restore the old open registry, or `V2_ANONYMOUS_PULL=true` for a public-read registry (anonymous pulls, authenticated pushes).

Unauthenticated requests receive a `WWW-Authenticate: Bearer realm=".../v2/token"` challenge; `docker login` trades credentials for a short-lived JWT at `/v2/token` (Basic auth also works as a fallback). Any account from the users table can log in — roles apply: readers can pull but not push, pushers can push only to repositories matching their `repo_patterns`, deletes require admin.

```bash
# Login from Docker CLI (any user account)
docker login localhost:8080 -u admin -p changeme123

# Kubernetes imagePullSecret
kubectl create secret docker-registry dockyard-secret \
  --docker-server=registry.yourdomain.com \
  --docker-username=admin \
  --docker-password=changeme123 \
  --namespace=your-namespace
```

---

## Docker Usage

> **Docker Desktop on Windows/Mac:** The Docker daemon runs inside a Linux VM. Use `host.docker.internal` instead of `localhost` for push/pull commands, and add it to insecure registries.

`Settings → Docker Engine`:
```json
{
  "insecure-registries": ["host.docker.internal:8080"]
}
```

```bash
# Push
docker tag myimage:latest host.docker.internal:8080/myimage:latest
docker push host.docker.internal:8080/myimage:latest

# Pull
docker pull host.docker.internal:8080/myimage:latest
```

---

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

---

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

---

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

---

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

---

---

# Dockyard — Registry Docker auto-hébergée

Un serveur Docker Registry V2 léger, écrit en Go. Livré sous forme d'un **binaire unique** avec une UI React embarquée — aucune dépendance externe en mode local.

> Basé sur la spécification [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/) — le même protocole qu'implémente [distribution/distribution](https://github.com/distribution/distribution).

## Fonctionnalités

- **UI embarquée** — dashboard React + Tailwind (shadcn/ui) servi directement par le binaire, avec thème clair/sombre/système
- **Support multi-arch** — les manifest lists (index OCI) sont résolues par plateforme, donc les images multi-arch affichent leur vraie taille totale au lieu de 0
- **Explorateur de layers** — inspecter les fichiers d'une layer (chemin, taille, symlinks) sans puller l'image
- **Suppression de dépôts et de tags** — supprimer un tag ou un dépôt entier depuis l'UI ou l'API
- **Deux backends de stockage** — filesystem local ou tout stockage objet compatible S3
- **Deux modes** — registry embarquée ou proxy devant une registry existante
- **Garbage collection** — déclenchement manuel via UI ou API, cron automatique quotidien à minuit UTC
- **Scan de vulnérabilités** — déclencher un scan Trivy sur une image poussée via l'API admin (`SCAN_ENABLED` + `TRIVY_SERVER_URL`), résultats stockés avec comptes par sévérité et rapport complet
- **Auth JWT** sur l'API admin, Basic Auth optionnelle sur `/v2/*`
- **Logs structurés JSON** via `log/slog`
- **Image Docker unique** — build multi-stage, image finale depuis `scratch`

---

## Modes

| Mode | Description |
|---|---|
| `embedded` | Dockyard **est** la registry — stocke blobs et manifests lui-même |
| `mirror` | Cache pull-through : sert depuis le stockage local, va chercher les manquants sur `REGISTRY_URL` (write-through). Tags revalidés après `MIRROR_TAG_TTL` (5 min par défaut) ; blobs immuables jamais re-téléchargés ; si l'upstream tombe, le contenu en cache continue d'être servi. Accepte aussi les push directs. |
| `proxy` | Dockyard se place devant une registry existante et expose l'UI/API admin |

---

## Démarrage rapide

### Prérequis

- Go 1.21+
- Node.js 22+ (pour builder l'UI)
- Docker (pour push/pull)

### Lancer en local (dev)

```bash
cp .env.example .env   # ajuster les valeurs si nécessaire

# Terminal 1 — serveur Go
make run

# Terminal 2 — serveur Vite avec hot reload (proxifie /api → Go)
make ui-dev
```

Ouvrir `http://localhost:5173` pour l'UI avec hot reload, ou `http://localhost:<PORT>` pour utiliser l'UI embarquée.

### Compiler le binaire de production

```bash
make release   # build l'UI puis l'embarque dans le binaire Go → dockyard.exe
```

Ou étape par étape :
```bash
make ui      # npm ci + vite build → internal/ui/dist/
make build   # go build avec UI embarquée
```

### Docker

Récupérer l'image pré-compilée depuis GitHub Container Registry :

```bash
docker pull ghcr.io/kgma74/dockyard:latest
docker run -p 8080:8080 --env-file .env ghcr.io/kgma74/dockyard:latest
```

Ou compiler localement :

```bash
docker build -t dockyard .
docker run -p 8080:8080 --env-file .env dockyard
```

Le Dockerfile utilise un **build 3 stages** : Node.js → Go → scratch. L'image finale contient uniquement le binaire statiquement lié avec l'UI embarquée. Multi-arch : `linux/amd64` et `linux/arm64`.

### Helm (Kubernetes)

```bash
helm upgrade --install dockyard oci://ghcr.io/kgma74/charts/dockyard \
  --namespace registry --create-namespace \
  --set auth.password=changeme
```

Voir `helm/dockyard/values.yaml` pour l'ensemble des options (backend de stockage, S3, ingress, mode proxy, etc.).

---

## Configuration

Tous les paramètres sont chargés depuis `.env` (ou variables d'environnement).

```env
PORT=8080
REGISTRY_MODE=embedded          # embedded | proxy

# ── Stockage ──────────────────────────────────────────────────────────────────
REGISTRY_STORAGE_BACKEND=local  # local | s3
REGISTRY_STORAGE_PATH=./data/registry

# S3 / MinIO / RustFS (quand REGISTRY_STORAGE_BACKEND=s3)
S3_ENDPOINT=http://rustfs:9000
S3_ACCESS_KEY=votre-access-key
S3_SECRET_KEY=votre-secret-key
S3_BUCKET=dockyard-registry
S3_REGION=us-east-1
S3_SECURE=false                 # true si TLS activé sur S3

# ── Auth ──────────────────────────────────────────────────────────────────────
AUTH_USERNAME=admin
AUTH_PASSWORD=changeme123       # mot de passe initial (premier démarrage uniquement)
JWT_SECRET=changez-moi-pour-une-longue-chaine-aleatoire
# JWT_SECRET_PREVIOUS=          # ancien secret pendant une fenêtre de rotation

# Auth sur /v2/* — désormais true par défaut (token auth Docker + fallback Basic).
# Mettre à false pour une registry ouverte (dev uniquement).
V2_AUTH_ENABLED=false
# Autoriser le pull anonyme, push authentifié (registry publique en lecture)
# V2_ANONYMOUS_PULL=true

# ── Durcissement ──────────────────────────────────────────────────────────────
# Limitation brute-force sur login + /v2/token, par IP cliente (0 désactive)
# RATE_LIMIT_LOGIN_PER_MIN=10
# Plafond souple de requêtes/seconde par IP sur le reste (0 désactive)
# RATE_LIMIT_GLOBAL_RPS=100
# CORS désactivé par défaut (UI embarquée, même origine). Ouvrir l'API à des
# clients navigateur externes avec une liste d'origines séparées par virgules :
# CORS_ALLOWED_ORIGINS=https://outils.exemple.com

# ── TLS natif (déploiements sans reverse proxy) ───────────────────────────────
# off (défaut) · static (TLS_CERT_FILE/TLS_KEY_FILE) · self-signed
# (autogénéré, persisté sous <storage>/tls) · acme (Let's Encrypt,
# TLS-ALPN — nécessite :443 accessible depuis internet et TLS_DOMAIN)
# TLS_MODE=self-signed
# TLS_DOMAIN=registry.exemple.com
# TLS_ACME_EMAIL=ops@exemple.com
# TLS_CERT_FILE= / TLS_KEY_FILE=   (mode static)
# Note self-signed : les clients docker doivent mettre la registry en
# "insecure-registries", ou ajouter <storage>/tls/cert.pem à leur trust store.

# ── Observabilité ─────────────────────────────────────────────────────────────
# Métriques Prometheus sur /metrics (non authentifié — garder le port privé ou
# désactiver) : HTTP par route normalisée, jauges storage, runs de GC, cache mirror.
# METRICS_ENABLED=true

# ── Scan de vulnérabilités (Trivy) ────────────────────────────────────────────
# Désactivé par défaut. Dockyard appelle en subprocess le binaire `trivy`
# embarqué dans sa propre image (mode --server) contre un `trivy server
# --listen` géré par l'opérateur, qui héberge la base de vulnérabilités.
# Dockyard ne fait pas tourner Trivy lui-même. Déclencher un scan via
# POST /api/admin/scans {"name","reference"}.
# SCAN_ENABLED=true
# TRIVY_SERVER_URL=http://trivy:4954
# TRIVY_BIN_PATH=/trivy                # chemin du binaire trivy dans l'image
# SCAN_TIMEOUT=5m                      # timeout du subprocess par scan
# SCAN_MAX_REPORT_BYTES=20971520       # cap sur le rapport JSON brut (20 Mio)
# SCAN_DEDUP_WINDOW=1h                 # réutilise un scan récent réussi pour le même digest
# TRIVY_INSECURE_REGISTRY=true         # trivy pull depuis Dockyard en HTTP simple sur localhost

# ── Mode proxy ────────────────────────────────────────────────────────────────
# REGISTRY_MODE=proxy
# REGISTRY_URL=http://votre-registry:5000
# REGISTRY_USERNAME=
# REGISTRY_PASSWORD=

# ── Mode mirror (cache pull-through) ─────────────────────────────────────────
# REGISTRY_MODE=mirror
# REGISTRY_URL=https://registry-1.docker.io    # upstream à mettre en cache (Docker
#                                              # Hub, ghcr, quay — token auth gérée)
# REGISTRY_USERNAME= / REGISTRY_PASSWORD=      # creds upstream (meilleurs rate limits)
# MIRROR_TAG_TTL=5m                            # intervalle de revalidation des tags
# Exemple : docker pull host:8080/library/alpine:latest met Docker Hub en cache.
```

---

## Backends de stockage

### Local (par défaut)

Stocke tout sur le filesystem local. Idéal pour le développement et les déploiements mono-nœud.

```env
REGISTRY_STORAGE_BACKEND=local
REGISTRY_STORAGE_PATH=./data/registry
```

Structure du répertoire :
```
data/registry/
├── blobs/sha256/<2-char>/<digest>/data
├── repositories/<nom>/manifests/<digest>
├── repositories/<nom>/tags/<tag>
└── uploads/<uuid>/data
```

### S3 / MinIO / RustFS

Stockage objet compatible API S3. Aucun PVC nécessaire — idéal pour Kubernetes et la production.

```env
REGISTRY_STORAGE_BACKEND=s3
S3_ENDPOINT=http://rustfs:9000
S3_ACCESS_KEY=votre-access-key
S3_SECRET_KEY=votre-secret-key
S3_BUCKET=dockyard-registry
S3_REGION=us-east-1
S3_SECURE=false
```

Le bucket est créé automatiquement au premier démarrage s'il n'existe pas.

Compatible avec tout service S3 : **RustFS**, **MinIO**, **AWS S3** (omettre `S3_ENDPOINT` pour AWS), Ceph, Backblaze B2, etc.

> Le GC (`POST /api/admin/gc`) et l'arbre de stockage (`GET /api/admin/storage/tree`) ne sont disponibles qu'avec le backend local.

---

## Authentification

### API Admin (`/api/admin/*`) — JWT

```bash
# Connexion
curl -X POST http://localhost:8080/api/admin/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"changeme123"}'
# → { "token": "eyJhbGci...", "refresh_token": "9f2c...", "role": "admin", "expires_in": 900 }

# Utiliser le token d'accès
curl http://localhost:8080/api/admin/repositories \
  -H "Authorization: Bearer eyJhbGci..."

# Le renouveler (les refresh tokens sont à usage unique, rotation à chaque appel)
curl -X POST http://localhost:8080/api/admin/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"9f2c..."}'
```

Les tokens d'accès sont valides **15 minutes** ; le refresh token maintient la session **30 jours**. La déconnexion révoque le token d'accès (persisté en SQLite — survit aux redémarrages) et tue la session.

```bash
# Changer le mot de passe
curl -X POST http://localhost:8080/api/admin/auth/password \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"current_password":"changeme123","new_password":"nouveaumotdepasse"}'
```

### Utilisateurs & rôles

Les comptes vivent en SQLite (`<storage>/dockyard.db`) avec trois rôles : **admin** (tout), **pusher** (pull + push), **reader** (lecture seule). Des globs `repo_patterns` optionnels restreignent un utilisateur aux repositories correspondants (`team-a/*` — `*` traverse aussi les `/`). La gestion des utilisateurs et des sessions (`/api/admin/users`, `/api/admin/sessions`) est réservée aux admins ; les mutations destructrices (`DELETE`, `/gc`) exigent le rôle admin.

Au premier démarrage après mise à jour, l'admin unique historique est migré automatiquement — un mot de passe changé à chaud (stocké dans `auth/password.bcrypt`) est conservé. `AUTH_PASSWORD` n'est utilisé que si la table users est vide.

> Faites tourner `JWT_SECRET` sans déconnecter tout le monde : mettez la nouvelle valeur dans `JWT_SECRET`, l'ancienne dans `JWT_SECRET_PREVIOUS` pendant une fenêtre de grâce, puis retirez-la.

### Registry V2 (`/v2/*`) — Token auth Docker

> **Breaking change :** `/v2/*` exige désormais une authentification **par défaut** (`V2_AUTH_ENABLED=true`). Mettre `false` pour retrouver la registry ouverte, ou `V2_ANONYMOUS_PULL=true` pour une registry publique en lecture (pull anonyme, push authentifié).

Les requêtes non authentifiées reçoivent un challenge `WWW-Authenticate: Bearer realm=".../v2/token"` ; `docker login` échange les identifiants contre un JWT court à `/v2/token` (le Basic auth fonctionne aussi en fallback). Tout compte de la table users peut se connecter — les rôles s'appliquent : un reader pull mais ne push pas, un pusher ne push que vers les repositories couverts par ses `repo_patterns`, les suppressions exigent admin.

```bash
# Connexion depuis le CLI Docker (n'importe quel compte)
docker login localhost:8080 -u admin -p changeme123

# imagePullSecret Kubernetes
kubectl create secret docker-registry dockyard-secret \
  --docker-server=registry.votredomaine.com \
  --docker-username=admin \
  --docker-password=changeme123 \
  --namespace=votre-namespace
```

---

## Utilisation Docker

> **Docker Desktop sur Windows/Mac :** Le daemon Docker tourne dans une VM Linux. Utiliser `host.docker.internal` au lieu de `localhost` pour les commandes push/pull, et l'ajouter aux insecure registries.

`Settings → Docker Engine` :
```json
{
  "insecure-registries": ["host.docker.internal:8080"]
}
```

```bash
# Push
docker tag monimage:latest host.docker.internal:8080/monimage:latest
docker push host.docker.internal:8080/monimage:latest

# Pull
docker pull host.docker.internal:8080/monimage:latest
```

---

## API Admin

Tous les endpoints nécessitent `Authorization: Bearer <token>` (sauf login/logout).

| Méthode | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | État + mode + version, probe storage (`ok`, `latency_ms`, compteurs cachés, `free_bytes` en local), compteurs mirror ; `status: degraded` si le backend échoue |
| `GET` | `/metrics` | Métriques Prometheus (si `METRICS_ENABLED`) |
| `POST` | `/api/admin/auth/login` | Obtenir les tokens d'accès + refresh |
| `POST` | `/api/admin/auth/refresh` | Renouveler le token d'accès (rotation du refresh) |
| `POST` | `/api/admin/auth/logout` | Révoquer le token + tuer la session (persisté) |
| `POST` | `/api/admin/auth/password` | Changer son mot de passe |
| `GET` | `/api/admin/users` | Lister les utilisateurs (admin) |
| `POST` | `/api/admin/users` | Créer un utilisateur `{username, password, role, repo_patterns}` (admin) |
| `PUT` | `/api/admin/users/:username` | Modifier rôle/patterns/mot de passe (admin) |
| `DELETE` | `/api/admin/users/:username` | Supprimer un utilisateur (admin, dernier admin protégé) |
| `GET` | `/api/admin/sessions` | Lister les sessions actives (admin) |
| `DELETE` | `/api/admin/sessions/:id` | Révoquer une session (admin) |
| `GET` | `/api/admin/audit?repo=&actor=&limit=&offset=` | Journal d'audit : logins, pushes, suppressions, GC (admin) |
| `GET`/`POST` | `/api/admin/retention` | Lister / créer des politiques de rétention `{repo_pattern, keep_n, unpulled_days, keep_patterns, protected_tags}` (admin) |
| `DELETE` | `/api/admin/retention/:id` | Supprimer une politique (admin) |
| `POST` | `/api/admin/retention/run?dryRun=true` | Prévisualiser ou appliquer le plan de rétention (admin) — tourne aussi chaque nuit avant le GC |
| `GET` | `/api/admin/repositories/export?name=<repo>` | Exporter un dépôt en tarball OCI image-layout (compatible skopeo/crane, admin) |
| `POST` | `/api/admin/repositories/import?name=<repo>` | Importer un tarball OCI image-layout (admin) |
| `GET`/`POST` | `/api/admin/webhooks` | Lister / créer des webhooks `{url, secret, events: [push,delete,retention,gc], format: generic\|slack\|discord}` (admin) |
| `DELETE` | `/api/admin/webhooks/:id` | Supprimer un webhook (admin) |
| `POST` | `/api/admin/webhooks/:id/test` | Envoyer un événement de test synchrone (admin) |
| `GET` | `/api/admin/repositories` | Lister tous les dépôts avec leurs tags et la date du dernier push |
| `GET` | `/api/admin/repositories/tags?name=<image>` | Lister les tags avec leurs digests et date de push |
| `GET` | `/api/admin/repositories/manifest?name=<image>&reference=<tag-ou-digest>` | Détails du manifest (taille, layers, plateformes si multi-arch) |
| `GET` | `/api/admin/repositories/layer?name=<image>&digest=sha256:<hash>` | Lister les fichiers d'une layer |
| `DELETE` | `/api/admin/repositories/manifests?name=<image>&digest=sha256:<hash>` | Supprimer un manifest |
| `DELETE` | `/api/admin/repositories?name=<image>` | Supprimer un dépôt et tous ses tags |
| `GET` | `/api/admin/storage/stats` | Utilisation du stockage (taille, blobs, dépôts) |
| `GET` | `/api/admin/storage/tree` | Arbre du filesystem (local uniquement) |
| `POST` | `/api/admin/gc` | Supprimer les blobs non référencés — `?dryRun=true` prévisualise sans supprimer |

---

## API Docker Registry V2

Dockyard implémente le [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/). Les commandes `docker push` / `docker pull` fonctionnent sans configuration supplémentaire.

| Endpoint | Description |
|---|---|
| `GET /v2/` | Vérification de version |
| `GET /v2/_catalog` | Lister les dépôts |
| `GET /v2/<nom>/tags/list` | Lister les tags |
| `HEAD\|GET /v2/<nom>/manifests/<ref>` | Récupérer un manifest |
| `PUT /v2/<nom>/manifests/<ref>` | Pousser un manifest |
| `DELETE /v2/<nom>/manifests/<digest>` | Supprimer un manifest |
| `HEAD\|GET /v2/<nom>/blobs/<digest>` | Récupérer un blob |
| `POST /v2/<nom>/blobs/uploads/` | Initier un upload de blob |
| `PATCH /v2/<nom>/blobs/uploads/<uuid>` | Envoyer un chunk |
| `PUT /v2/<nom>/blobs/uploads/<uuid>` | Finaliser l'upload |

---

## Makefile

```bash
make run       # Démarrer le serveur Go (lit .env automatiquement)
make ui        # Builder l'UI React → internal/ui/dist/
make ui-dev    # Démarrer Vite sur :5173 (proxifie /api → Go)
make release   # Builder l'UI puis compiler le binaire (production)
make build     # Compiler le binaire uniquement (UI doit être buildée avant)
make test      # Lancer tous les tests avec -v
make watch     # Rechargement automatique via air
make clean     # Supprimer le binaire et réinitialiser le placeholder UI
```

---

## License / Licence

MIT — see [LICENSE](./LICENSE).
