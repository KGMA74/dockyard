# ── Stage 1 : Build UI ───────────────────────────────────────────────────────
FROM node:26-alpine AS ui

WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build
# output → ../internal/ui/dist  (relative path from ui/ in vite.config.ts)

# ── Stage 2 : Build Go binary ────────────────────────────────────────────────
FROM golang:1.26.4-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Overwrite the placeholder dist with the real Vite build
COPY --from=ui /internal/ui/dist ./internal/ui/dist

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
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
