# syntax=docker/dockerfile:1

# ============================================================
# Build stage
# ============================================================
FROM golang:1.27-bookworm AS builder

WORKDIR /src

# Node.js 22+ is required by the project
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        nodejs \
        npm \
    && rm -rf /var/lib/apt/lists/*

# Go dependencies first for better Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Frontend dependencies
COPY package.json package-lock.json ./
RUN npm ci

# Source
COPY . .

# Build frontend
RUN npm run build

# Build the CLI and web server.
# The project requires these build tags.
RUN mkdir -p /out/bin \
    && CGO_ENABLED=1 go build \
        -tags with_quic,with_utls \
        -o /out/bin/xgift \
        ./cmd/xgift \
    && CGO_ENABLED=1 go build \
        -tags with_quic,with_utls \
        -o /out/bin/xgift-web \
        ./cmd/xgift-web


# ============================================================
# Runtime stage
# ============================================================
FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        caddy \
        openssl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/bin/xgift /app/bin/xgift
COPY --from=builder /out/bin/xgift-web /app/bin/xgift-web

COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Persistent application data
RUN mkdir -p /app/data /app/config /app/logs

# xgift-web MUST remain on loopback.
# Caddy will expose the HTTP port to Zeabur.
ENV XGIFT_LISTEN=127.0.0.1:8787
ENV XGIFT_DATA_DIR=/app/data
ENV XGIFT_PASSWORD_FILE=/app/config/vault-password
ENV XGIFT_ADMIN_PASSWORD_FILE=/app/config/admin-password
ENV XGIFT_PAYMENTS_ENABLED=false

EXPOSE 8080

ENTRYPOINT 
["/app/docker-entrypoint.sh"]
