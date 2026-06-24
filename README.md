# Maestro — Self-hosted Docker Registry

A lightweight, self-hosted Docker Registry V2 server written in Go. Single binary, no external dependencies for local mode.

> Built on the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/) specification — the same protocol implemented by [distribution/distribution](https://github.com/distribution/distribution).

## Modes

| Mode | Description |
|---|---|
| `embedded` | Maestro **is** the registry — stores blobs and manifests itself |
| `proxy` | Maestro sits in front of an existing registry and exposes the admin API |

---

## Getting Started

### Prerequisites
- Go 1.21+
- Docker (to push/pull images)

### Run locally

```bash
cp .env.example .env   # adjust values as needed
make run
```

The server starts on `http://localhost:8080`.

### Build binary

```bash
make build   # produces maestro.exe / maestro
```

### Docker

```bash
docker build -t maestro .
docker run -p 8080:8080 --env-file .env maestro
```

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
S3_BUCKET=maestro-registry
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
S3_BUCKET=maestro-registry
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
kubectl create secret docker-registry maestro-secret \
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

Maestro implements the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/). Standard `docker push` / `docker pull` work out of the box.

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
make run     # Start the server (reads .env automatically)
make build   # Compile to maestro.exe
make test    # Run all tests with -v
make watch   # Live reload via air
```

---

---

# Maestro — Registry Docker auto-hébergée

Un serveur Docker Registry V2 léger, écrit en Go. Binaire unique, aucune dépendance externe en mode local.

> Basé sur la spécification [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/) — le même protocole qu'implémente [distribution/distribution](https://github.com/distribution/distribution).

## Modes

| Mode | Description |
|---|---|
| `embedded` | Maestro **est** la registry — stocke blobs et manifests lui-même |
| `proxy` | Maestro se place devant une registry existante et expose l'API admin |

---

## Démarrage rapide

### Prérequis
- Go 1.21+
- Docker (pour push/pull)

### Lancer en local

```bash
cp .env.example .env   # ajuster les valeurs si nécessaire
make run
```

Le serveur démarre sur `http://localhost:8080`.

### Compiler le binaire

```bash
make build   # produit maestro.exe / maestro
```

### Docker

```bash
docker build -t maestro .
docker run -p 8080:8080 --env-file .env maestro
```

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
S3_BUCKET=maestro-registry
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
S3_BUCKET=maestro-registry
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
kubectl create secret docker-registry maestro-secret \
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

Maestro implémente le [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/). Les commandes `docker push` / `docker pull` fonctionnent sans configuration supplémentaire.

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
make run     # Démarrer le serveur (lit .env automatiquement)
make build   # Compiler en maestro.exe
make test    # Lancer tous les tests avec -v
make watch   # Rechargement automatique via air
```
