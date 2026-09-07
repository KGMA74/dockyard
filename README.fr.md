# Dockyard — Registry Docker auto-hébergée

[![CI](https://github.com/KGMA74/dockyard/actions/workflows/ci.yml/badge.svg)](https://github.com/KGMA74/dockyard/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dockyard)](https://artifacthub.io/packages/search?repo=dockyard)

**[🇬🇧 English version](./README.md)**

Un serveur Docker Registry V2 léger, écrit en Go. Livré sous forme d'un **binaire unique** avec une UI React embarquée — aucune dépendance externe en mode local.

> Basé sur la spécification [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/) — le même protocole qu'implémente [distribution/distribution](https://github.com/distribution/distribution).

## Fonctionnalités

- **UI embarquée** — dashboard React + Tailwind (shadcn/ui), thème clair/sombre/système, localisation FR/EN
- **Deux backends de stockage** — filesystem local ou tout stockage objet compatible S3 (RustFS, MinIO, AWS S3, …)
- **Deux modes** — registry embarquée, ou proxy/mirror devant une registry existante
- **Support multi-arch** — les manifest lists (index OCI) sont résolues par plateforme, vraie taille totale au lieu de 0
- **Scan de vulnérabilités** — scans Trivy à la demande via l'API admin, résultats avec comptes par sévérité
- **Application des push signés** — rejette les push sans signature cosign valide, vérifiée côté serveur
- **Garbage collection, politiques de rétention, réplication, quotas** — tous avec prévisualisation dry-run
- **Diff de tags, explorateur de layers, recherche serveur** — inspecter et comparer les images depuis l'UI
- **Notifications in-app, journal d'audit, webhooks** — sur le même flux d'événements
- **Auth JWT** sur l'API admin, token auth Docker sur `/v2/*`, logs JSON structurés, tracing OpenTelemetry optionnel

## Modes

| Mode | Description |
|---|---|
| `embedded` | Dockyard **est** la registry — stocke blobs et manifests lui-même |
| `proxy` | Dockyard se place devant une registry existante et expose l'UI/API admin |
| `mirror` | Cache pull-through : sert depuis le stockage local, va chercher les manquants sur `REGISTRY_URL` (write-through), accepte aussi les push directs |

## Démarrage rapide

**Prérequis :** Go 1.21+, Node.js 22+ (pour builder l'UI), Docker.

```bash
cp .env.example .env   # ajuster les valeurs si nécessaire

# Terminal 1 — serveur Go
make run

# Terminal 2 — serveur Vite avec hot reload (proxifie /api → Go)
make ui-dev
```

Ouvrir `http://localhost:5173` pour l'UI avec hot reload, ou `http://localhost:<PORT>` pour l'UI embarquée.

**Binaire de production :** `make release` (build l'UI puis l'embarque dans `dockyard.exe`). Voir les [cibles du Makefile](./API.md#makefile) pour le détail étape par étape.

**Docker :**

```bash
docker pull ghcr.io/kgma74/dockyard:latest
docker run -p 8080:8080 --env-file .env ghcr.io/kgma74/dockyard:latest
```

**Helm :**

```bash
helm upgrade --install dockyard oci://ghcr.io/kgma74/charts/dockyard \
  --namespace registry --create-namespace \
  --set auth.password=changeme
```

Voir `helm/dockyard/values.yaml` pour l'ensemble des options. Un module Terraform (`terraform/`) provisionne un bucket S3 + un utilisateur IAM scopé et déploie ce chart en un `terraform apply` — voir `terraform/README.md`.

## Configuration

```env
PORT=8080
REGISTRY_MODE=embedded          # embedded | proxy | mirror

REGISTRY_STORAGE_BACKEND=local  # local | s3
REGISTRY_STORAGE_PATH=./data/registry

# S3 / MinIO / RustFS (quand REGISTRY_STORAGE_BACKEND=s3)
S3_ENDPOINT=http://rustfs:9000
S3_ACCESS_KEY=votre-access-key
S3_SECRET_KEY=votre-secret-key
S3_BUCKET=dockyard-registry
S3_REGION=us-east-1
S3_SECURE=false

AUTH_USERNAME=admin
AUTH_PASSWORD=changeme123       # mot de passe initial (premier démarrage uniquement)
JWT_SECRET=changez-moi-pour-une-longue-chaine-aleatoire

# Auth sur /v2/* — token auth Docker + fallback Basic. false = registry ouverte (dev uniquement).
V2_AUTH_ENABLED=false
```

Le bucket du backend S3 est créé automatiquement au premier démarrage s'il n'existe pas ; le GC et l'arbre de stockage ne sont disponibles qu'avec le backend local.

Rate limiting, CORS, TLS natif, Prometheus/OpenTelemetry, scan Trivy, application cosign, et identifiants upstream proxy/mirror sont documentés dans la **[référence de configuration complète](./CONFIGURATION.md)** (en anglais).

## Authentification

Les comptes vivent en SQLite avec trois rôles — **admin**, **pusher**, **reader** — et des globs `repo_patterns` optionnels restreignant un utilisateur aux repositories correspondants. `/api/admin/*` utilise des JWT courts (15 min, renouvelés via un refresh token tournant de 30 jours) ; `/v2/*` utilise le token dance propre à Docker (`docker login` contre `/v2/token`, Basic auth en fallback) et respecte les mêmes rôles.

```bash
docker login localhost:8080 -u admin -p changeme123
docker push localhost:8080/monimage:latest
```

> **Docker Desktop sur Windows/Mac :** utiliser `host.docker.internal` au lieu de `localhost`, et l'ajouter aux `insecure-registries` dans `Settings → Docker Engine`.

Les exemples complets de login/refresh/changement de mot de passe et la référence complète des API admin + registry sont dans **[API.md](./API.md)** (en anglais).

## License

MIT — voir [LICENSE](./LICENSE).
