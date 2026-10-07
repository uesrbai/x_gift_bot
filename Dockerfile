# syntax=docker/dockerfile:1

FROM node:22-bookworm AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY frontend ./frontend
RUN mkdir -p internal/site/assets && npm run build

FROM golang:1.27-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/internal/site/assets ./internal/site/assets
RUN mkdir -p /out/bin \
    && CGO_ENABLED=1 go build -tags with_quic,with_utls -o /out/bin/xgift ./cmd/xgift \
    && CGO_ENABLED=1 go build -tags with_quic,with_utls -o /out/bin/xgift-web ./cmd/xgift-web

FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates caddy curl openssl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/bin/xgift /app/bin/xgift
COPY --from=builder /out/bin/xgift-web /app/bin/xgift-web
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh && mkdir -p /app/data /app/config
ENV XGIFT_LISTEN=127.0.0.1:8787 \
    XGIFT_DATA_DIR=/app/data \
    XGIFT_PASSWORD_FILE=/app/config/vault-password \
    XGIFT_ADMIN_PASSWORD_FILE=/app/config/admin-password \
    XGIFT_PAYMENTS_ENABLED=false
EXPOSE 8080
ENTRYPOINT ["/app/docker-entrypoint.sh"]
