# ── Stage 1 : Build ──────────────────────────────────────────────────────────
FROM golang:1.26.4-alpine AS builder

WORKDIR /app

# Dependencies first to leverage Docker layer cache
COPY go.mod go.sum ./
RUN go mod download

# Source code
COPY . .

# Static binary — zero runtime dependencies
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-w -s" \
    -trimpath \
    -o maestro \
    ./cmd/maestro

# ── Stage 2 : Final image ─────────────────────────────────────────────────────
FROM scratch

# TLS certificates required for HTTPS calls (S3, etc.)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Binary only
COPY --from=builder /app/maestro /maestro

EXPOSE 8080

ENTRYPOINT ["/maestro"]
