# ── Stage 1 : Build UI (always runs on the host platform, no QEMU) ───────────
FROM --platform=$BUILDPLATFORM node:26-alpine AS ui

WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ── Stage 2 : Build Go binary ────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS builder

ARG TARGETOS TARGETARCH
ARG VERSION=dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui /internal/ui/dist ./internal/ui/dist

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
    -ldflags="-w -s -X dockyard/internal/version.Version=${VERSION}" \
    -trimpath \
    -o dockyard \
    ./cmd/dockyard

# ── Stage 3 : trivy binary + pre-seeded vulnerability DB ─────────────────────
# Pinned explicitly — a version bump may require adjusting the JSON parsing in
# internal/scan/trivy.go, so treat it as a deliberate, linked change.
# We also download the vulnerability DB at build time and ship it in the final
# image, so the very first scan works offline and doesn't stall on a ~50 MB
# download. The server refreshes it on startup and every 12 hours (unless
# TRIVY_OFFLINE=true), together with the Java DB.
# The Java DB (needed to scan images with jars) is ~1 GB compressed, so it is
# not shipped by default: the server downloads it at startup into the cache
# volume. For an air-gapped install (TRIVY_OFFLINE=true), build with
# --build-arg TRIVY_JAVA_DB=true to ship it as well.
FROM aquasec/trivy:0.56.2 AS trivy
ARG TRIVY_JAVA_DB=false
RUN trivy --cache-dir /trivy-cache image --download-db-only \
    && if [ "$TRIVY_JAVA_DB" = "true" ]; then trivy --cache-dir /trivy-cache image --download-java-db-only; fi \
    && mkdir -p /empty-tmp

# ── Stage 4 : Final image ─────────────────────────────────────────────────────
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/dockyard /dockyard
COPY --from=trivy /usr/local/bin/trivy /trivy
COPY --from=trivy /trivy-cache /opt/trivy-cache
# scratch has no /tmp. Dockyard points trivy's TMPDIR at <cache>/tmp on the
# volume; this one is for running /trivy by hand (kubectl exec).
COPY --from=trivy --chmod=1777 /empty-tmp /tmp

EXPOSE 8080

ENTRYPOINT ["/dockyard"]
