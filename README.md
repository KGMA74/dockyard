# Dockyard — Self-hosted Docker Registry

A lightweight, self-hosted Docker Registry V2 server written in Go. Ships as a **single binary** with an embedded React UI — no external dependencies for local mode.

> Built on the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/) specification — the same protocol implemented by [distribution/distribution](https://github.com/distribution/distribution).

## Features

- **Embedded UI** — React + Tailwind dashboard served directly by the binary
- **Two storage backends** — local filesystem or any S3-compatible object store
- **Two modes** — embedded registry or proxy in front of an existing registry
- **Garbage collection** — manual trigger via UI or API, automatic daily cron at midnight UTC
- **JWT auth** on the admin API, optional Basic Auth on `/v2/*`
- **Structured JSON logging** via `log/slog`
- **Single Docker image** — multi-stage build, final image from `scratch`

---

## Modes

| Mode | Description |
|---|---|
| `embedded` | Dockyard **is** the registry — stores blobs and manifests itself |
| `proxy` | Dockyard sits in front of an existing registry and exposes the admin UI/API |

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

# Basic auth on /v2/* (false = open in dev · true = required in prod)
V2_AUTH_ENABLED=false

# ── Proxy mode ────────────────────────────────────────────────────────────────
# REGISTRY_MODE=proxy
# REGISTRY_URL=http://your-registry:5000
# REGISTRY_USERNAME=
# REGISTRY_PASSWORD=
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
# → { "token": "eyJhbGci..." }

# Use the token
curl http://localhost:8080/api/admin/repositories \
  -H "Authorization: Bearer eyJhbGci..."
```

Tokens are valid for **24 hours**. Logout invalidates the token immediately.

```bash
# Change password
curl -X POST http://localhost:8080/api/admin/auth/password \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"current_password":"changeme123","new_password":"newpassword"}'
```

The new password is saved as a bcrypt hash in `data/registry/auth/password.bcrypt` and persists across restarts. `AUTH_PASSWORD` is only used on first startup (when no hash file exists).

> Change `JWT_SECRET` to a long random string in production — all tokens are invalidated if this value changes.

### Registry V2 (`/v2/*`) — Basic Auth

Open by default (`V2_AUTH_ENABLED=false`). Enable for preprod/production:

```env
V2_AUTH_ENABLED=true
```

Uses the same credentials as the admin panel (`AUTH_USERNAME` / `AUTH_PASSWORD`).

```bash
# Login from Docker CLI
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
| `GET` | `/health` | Health check + mode info |
| `POST` | `/api/admin/auth/login` | Get JWT token |
| `POST` | `/api/admin/auth/logout` | Invalidate token |
| `POST` | `/api/admin/auth/password` | Change password |
| `GET` | `/api/admin/repositories` | List all repositories with tags |
| `GET` | `/api/admin/repositories/tags?name=<image>` | List tags with digests |
| `DELETE` | `/api/admin/repositories/manifests?name=<image>&digest=sha256:<hash>` | Delete a manifest |
| `GET` | `/api/admin/storage/stats` | Storage usage (size, blob count, repo count) |
| `GET` | `/api/admin/storage/tree` | Raw filesystem tree (local only) |
| `POST` | `/api/admin/gc` | Garbage collect unreferenced blobs (local only) |

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

- **UI embarquée** — dashboard React + Tailwind servi directement par le binaire
- **Deux backends de stockage** — filesystem local ou tout stockage objet compatible S3
- **Deux modes** — registry embarquée ou proxy devant une registry existante
- **Garbage collection** — déclenchement manuel via UI ou API, cron automatique quotidien à minuit UTC
- **Auth JWT** sur l'API admin, Basic Auth optionnelle sur `/v2/*`
- **Logs structurés JSON** via `log/slog`
- **Image Docker unique** — build multi-stage, image finale depuis `scratch`

---

## Modes

| Mode | Description |
|---|---|
| `embedded` | Dockyard **est** la registry — stocke blobs et manifests lui-même |
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

# Basic auth sur /v2/* (false = ouvert en dev · true = obligatoire en prod)
V2_AUTH_ENABLED=false

# ── Mode proxy ────────────────────────────────────────────────────────────────
# REGISTRY_MODE=proxy
# REGISTRY_URL=http://votre-registry:5000
# REGISTRY_USERNAME=
# REGISTRY_PASSWORD=
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
# → { "token": "eyJhbGci..." }

# Utiliser le token
curl http://localhost:8080/api/admin/repositories \
  -H "Authorization: Bearer eyJhbGci..."
```

Les tokens sont valides **24 heures**. La déconnexion invalide le token immédiatement.

```bash
# Changer le mot de passe
curl -X POST http://localhost:8080/api/admin/auth/password \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"current_password":"changeme123","new_password":"nouveaumotdepasse"}'
```

Le nouveau mot de passe est sauvegardé sous forme de hash bcrypt dans `data/registry/auth/password.bcrypt` et persiste entre les redémarrages. `AUTH_PASSWORD` n'est utilisé qu'au premier démarrage (quand le fichier hash n'existe pas encore).

> Changez `JWT_SECRET` pour une longue chaîne aléatoire en production — tous les tokens sont invalidés si cette valeur change.

### Registry V2 (`/v2/*`) — Basic Auth

Ouvert par défaut (`V2_AUTH_ENABLED=false`). Activer pour la préprod/production :

```env
V2_AUTH_ENABLED=true
```

Utilise les mêmes identifiants que le panel admin (`AUTH_USERNAME` / `AUTH_PASSWORD`).

```bash
# Connexion depuis le CLI Docker
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
| `GET` | `/health` | Vérification d'état + infos mode |
| `POST` | `/api/admin/auth/login` | Obtenir un token JWT |
| `POST` | `/api/admin/auth/logout` | Invalider le token |
| `POST` | `/api/admin/auth/password` | Changer le mot de passe |
| `GET` | `/api/admin/repositories` | Lister tous les dépôts avec leurs tags |
| `GET` | `/api/admin/repositories/tags?name=<image>` | Lister les tags avec leurs digests |
| `DELETE` | `/api/admin/repositories/manifests?name=<image>&digest=sha256:<hash>` | Supprimer un manifest |
| `GET` | `/api/admin/storage/stats` | Utilisation du stockage (taille, blobs, dépôts) |
| `GET` | `/api/admin/storage/tree` | Arbre du filesystem (local uniquement) |
| `POST` | `/api/admin/gc` | Supprimer les blobs non référencés (local uniquement) |

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
