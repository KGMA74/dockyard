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

# ── Stage 3 : trivy binary (vulnerability scanning, see internal/scan) ────────
# Pinned explicitly — a version bump may require adjusting the JSON parsing in
# internal/scan/trivy.go, so treat it as a deliberate, linked change.
FROM aquasec/trivy:0.56.2 AS trivy

# ── Stage 4 : Final image ─────────────────────────────────────────────────────
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/dockyard /dockyard
COPY --from=trivy /usr/local/bin/trivy /trivy

EXPOSE 8080

ENTRYPOINT ["/dockyard"]
