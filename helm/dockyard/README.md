# Dockyard

A self-hosted Docker Registry V2 server, with an embedded UI, S3-compatible storage, and an optional proxy mode in front of an existing registry.

## TL;DR

```bash
helm upgrade --install dockyard oci://ghcr.io/kgma74/charts/dockyard \
  --namespace registry --create-namespace \
  --set auth.password=changeme
```

## Introduction

This chart deploys Dockyard, either as:

- **`embedded`** — Dockyard is the registry itself, storing blobs/manifests on a local PVC or an S3-compatible bucket.
- **`proxy`** — Dockyard forwards `/v2/*` traffic to an existing upstream registry and exposes the same admin API/UI in front of it.

## Prerequisites

- Kubernetes 1.23+
- Helm 3.8+ (for OCI registry support)
- A `storageClass` available in the cluster if using the local storage backend

## Installing the Chart

```bash
helm upgrade --install dockyard oci://ghcr.io/kgma74/charts/dockyard \
  --namespace registry --create-namespace \
  --set auth.password=changeme
```

The JWT secret used to sign admin sessions is auto-generated on first install and preserved across upgrades — set `auth.jwtSecret` explicitly only if you need to pin it (e.g. to share it across releases).

## Uninstalling the Chart

```bash
helm uninstall dockyard --namespace registry
```

This does not delete the PVC created for local storage; remove it manually if you no longer need the data.

## Parameters

### Image

| Name                  | Description                                   | Value                       |
| ---------------------- | ---------------------------------------------- | ---------------------------- |
| `image.repository`      | Image repository                                | `ghcr.io/kgma74/dockyard`    |
| `image.pullPolicy`      | Image pull policy                               | `Always`                     |
| `image.tag`             | Image tag (defaults to the chart's appVersion) | `""`                          |
| `imagePullSecrets`      | Image pull secrets                              | `[]`                          |

### Registry

| Name                        | Description                                             | Value        |
| ----------------------------- | ---------------------------------------------------------- | -------------- |
| `registry.mode`                | `embedded` or `proxy`                                       | `embedded`    |
| `registry.storage.backend`     | `local` or `s3` (embedded mode only)                        | `local`       |
| `registry.storage.path`        | Filesystem path for local storage                           | `/data/registry` |
| `registry.s3.endpoint`         | S3-compatible endpoint (empty for AWS S3)                   | `""`           |
| `registry.s3.bucket`           | Bucket name (created automatically if missing)              | `dockyard-registry` |
| `registry.s3.region`           | Bucket region                                               | `us-east-1`    |
| `registry.s3.secure`           | Use TLS for the S3 endpoint                                 | `"true"`       |
| `registry.s3.accessKey`        | S3 access key                                                | `""`           |
| `registry.s3.secretKey`        | S3 secret key                                                | `""`           |
| `registry.proxy.url`           | Upstream registry URL (proxy mode only)                     | `""`           |
| `registry.proxy.username`      | Upstream registry username                                  | `""`           |
| `registry.proxy.password`      | Upstream registry password                                  | `""`           |

### Auth

| Name                    | Description                                                              | Value        |
| ------------------------- | --------------------------------------------------------------------------- | -------------- |
| `auth.username`            | Admin UI/API username                                                       | `admin`       |
| `auth.password`            | Admin UI/API password (only used on first install)                          | `changeme`    |
| `auth.v2Enabled`           | Require Basic Auth on `/v2/*` (Docker push/pull)                             | `false`       |
| `auth.jwtSecret`           | JWT signing secret — auto-generated on first install if left empty          | `""`           |
| `auth.existingSecret`      | Name of an existing Secret holding `jwt-secret`/`auth-password` (and optionally S3 keys) | `""`           |

### Ingress

| Name                       | Description                                                                                   | Value    |
| ---------------------------- | -------------------------------------------------------------------------------------------------- | ---------- |
| `ingress.enabled`             | Enable ingress                                                                                       | `false`   |
| `ingress.className`           | `traefik` (streams large blob uploads out of the box) or `nginx`                              | `""`       |
| `ingress.annotations`         | Extra ingress annotations. For `nginx`, set `proxy-body-size: "0"` and `proxy-request-buffering: "off"` to allow large blob uploads | `{}`       |
| `ingress.hosts`               | Ingress hosts/paths                                                                                   | see values.yaml |
| `ingress.tls`                 | Ingress TLS configuration                                                                             | `[]`       |

### Persistence

| Name                          | Description                              | Value          |
| ------------------------------- | ------------------------------------------- | ---------------- |
| `persistence.enabled`             | Create a PVC for local storage              | `true`         |
| `persistence.storageClass`        | StorageClass to use                         | `""`           |
| `persistence.accessMode`          | PVC access mode                             | `ReadWriteOnce`|
| `persistence.size`                | PVC size                                    | `10Gi`         |
| `persistence.existingClaim`       | Use an existing PVC instead of creating one | `""`           |

See [`values.yaml`](./values.yaml) for the complete list of configurable parameters, including `replicaCount`, `resources`, `podAnnotations`, `securityContext`, `nodeSelector`, `tolerations` and `affinity`.

## Large blob uploads behind an ingress

Pushing large images (many/large layers) through an ingress controller can fail with connection-reset errors unless request buffering is disabled. Traefik (`ingress.className: traefik`) streams both directions by default, so no extra configuration is needed — do not add a buffering `Middleware`, it fully buffers the request and response and breaks both large blob uploads and the SSE live-update feed. For `nginx`, add these annotations yourself:

```yaml
ingress:
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "0"
    nginx.ingress.kubernetes.io/proxy-request-buffering: "off"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "600"
```

## License

MIT — see the [project LICENSE](https://github.com/kgma74/dockyard/blob/main/LICENSE).
