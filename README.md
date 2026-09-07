# Dockyard — Self-hosted Docker Registry

[![CI](https://github.com/KGMA74/dockyard/actions/workflows/ci.yml/badge.svg)](https://github.com/KGMA74/dockyard/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dockyard)](https://artifacthub.io/packages/search?repo=dockyard)

**[🇫🇷 Version française](./README.fr.md)**

A lightweight, self-hosted Docker Registry V2 server written in Go. Ships as a **single binary** with an embedded React UI — no external dependencies for local mode.

> Built on the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/) specification — the same protocol implemented by [distribution/distribution](https://github.com/distribution/distribution).

## Features

- **Embedded UI** — React + Tailwind (shadcn/ui) dashboard, dark/light/system theme, EN/FR localization
- **Two storage backends** — local filesystem or any S3-compatible object store (RustFS, MinIO, AWS S3, …)
- **Two modes** — embedded registry, or proxy/mirror in front of an existing registry
- **Multi-arch aware** — manifest lists (OCI indexes) resolve per-platform, real total size instead of 0
- **Vulnerability scanning** — Trivy scans on demand via the admin API, results with severity counts
- **Signed-push enforcement** — reject pushes without a valid cosign signature, verified server-side
- **Garbage collection, retention policies, replication, byte quotas** — all with dry-run previews
- **Tag diff, layer browser, server-side search** — inspect and compare images from the UI
- **In-app notifications, audit log, webhooks** — over the same event feed
- **JWT auth** on the admin API, Docker token auth on `/v2/*`, structured JSON logs, optional OpenTelemetry tracing

## Modes

| Mode | Description |
|---|---|
| `embedded` | Dockyard **is** the registry — stores blobs and manifests itself |
| `proxy` | Dockyard sits in front of an existing registry and exposes the admin UI/API |
| `mirror` | Pull-through cache: serves from local storage, fetches misses from `REGISTRY_URL` (write-through), also accepts direct pushes |

## Getting Started

**Prerequisites:** Go 1.21+, Node.js 22+ (to build the UI), Docker.

```bash
cp .env.example .env   # adjust values as needed

# Terminal 1 — Go server
make run

# Terminal 2 — Vite dev server with hot reload (proxies /api → Go)
make ui-dev
```

Open `http://localhost:5173` for the UI with hot reload, or `http://localhost:<PORT>` to use the embedded UI.

**Production binary:** `make release` (builds the UI, then embeds it in `dockyard.exe`). See [Makefile targets](./API.md#makefile) for the individual steps.

**Docker:**

```bash
docker pull ghcr.io/kgma74/dockyard:latest
docker run -p 8080:8080 --env-file .env ghcr.io/kgma74/dockyard:latest
```

**Helm:**

```bash
helm upgrade --install dockyard oci://ghcr.io/kgma74/charts/dockyard \
  --namespace registry --create-namespace \
  --set auth.password=changeme
```

See `helm/dockyard/values.yaml` for the full set of options. A Terraform module (`terraform/`) provisions an S3 bucket + scoped IAM user and deploys the chart in one `terraform apply` — see `terraform/README.md`.

## Configuration

```env
PORT=8080
REGISTRY_MODE=embedded          # embedded | proxy | mirror

REGISTRY_STORAGE_BACKEND=local  # local | s3
REGISTRY_STORAGE_PATH=./data/registry

# S3 / MinIO / RustFS (when REGISTRY_STORAGE_BACKEND=s3)
S3_ENDPOINT=http://rustfs:9000
S3_ACCESS_KEY=your-access-key
S3_SECRET_KEY=your-secret-key
S3_BUCKET=dockyard-registry
S3_REGION=us-east-1
S3_SECURE=false

AUTH_USERNAME=admin
AUTH_PASSWORD=changeme123       # initial password (first startup only)
JWT_SECRET=change-this-to-a-long-random-secret

# Auth on /v2/* — Docker token auth + Basic fallback. false = open registry (dev only).
V2_AUTH_ENABLED=false
```

The bucket for the S3 backend is created automatically on first startup if it doesn't exist; GC and the storage-tree view are local-backend only.

Rate limiting, CORS, native TLS, Prometheus/OpenTelemetry, Trivy scanning, cosign enforcement, and proxy/mirror upstream credentials are all documented in the **[full configuration reference](./CONFIGURATION.md)**.

## Authentication

Accounts live in SQLite with three roles — **admin**, **pusher**, **reader** — and optional `repo_patterns` globs restricting a user to matching repositories. `/api/admin/*` uses short-lived JWTs (15 min, refreshed via a rotating 30-day refresh token); `/v2/*` uses Docker's own token dance (`docker login` against `/v2/token`, Basic auth as a fallback) and honors the same roles.

```bash
docker login localhost:8080 -u admin -p changeme123
docker push localhost:8080/myimage:latest
```

> **Docker Desktop on Windows/Mac:** use `host.docker.internal` instead of `localhost`, and add it to `insecure-registries` in `Settings → Docker Engine`.

Full login/refresh/password-change examples and the complete admin + registry API reference are in **[API.md](./API.md)**.

## License

MIT — see [LICENSE](./LICENSE).
