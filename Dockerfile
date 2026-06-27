# ── Stage 1 : Build UI (always runs on the host platform, no QEMU) ───────────
FROM --platform=$BUILDPLATFORM node:26-alpine AS ui

WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ── Stage 2 : Build Go binary ────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS builder

ARG TARGETOS TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui /internal/ui/dist ./internal/ui/dist

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
    -ldflags="-w -s" \
    -trimpath \
    -o dockyard \
    ./cmd/dockyard

# ── Stage 3 : Final image ─────────────────────────────────────────────────────
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/dockyard /dockyard

EXPOSE 8080

ENTRYPOINT ["/dockyard"]
