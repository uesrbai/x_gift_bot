#!/bin/bash
set -euo pipefail

DATA_DIR="${XGIFT_DATA_DIR:-/app/data}"
CONFIG_DIR="/app/config"

ORIGIN="${XGIFT_ORIGIN:-}"

LISTEN_ADDR="${XGIFT_LISTEN:-127.0.0.1:8787}"
PASSWORD_FILE="${XGIFT_PASSWORD_FILE:-${CONFIG_DIR}/vault-password}"
ADMIN_PASSWORD_FILE="${XGIFT_ADMIN_PASSWORD_FILE:-${CONFIG_DIR}/admin-password}"

export XGIFT_DATA_DIR="$DATA_DIR"
export XGIFT_LISTEN="$LISTEN_ADDR"
export XGIFT_PASSWORD_FILE="$PASSWORD_FILE"
export XGIFT_ADMIN_PASSWORD_FILE="$ADMIN_PASSWORD_FILE"

mkdir -p "$DATA_DIR" "$CONFIG_DIR"

chmod 700 "$DATA_DIR" "$CONFIG_DIR"

# ------------------------------------------------------------
# Basic configuration validation
# ------------------------------------------------------------

if 
[ -z "$ORIGIN" ]
; then
    echo "ERROR: XGIFT_ORIGIN is required."
    echo "Example: https://gift.example.com"
    exit 1
fi

case "$ORIGIN" in
    https://*)
        ;;
    *)
        echo "ERROR: XGIFT_ORIGIN must start with https://"
        exit 1
        ;;
esac

# ------------------------------------------------------------
# Password files
#
# The application requires:
#   - admin password >= 32 characters
#   - vault password file
#
# These are generated once and then kept on the persistent volume.
# ------------------------------------------------------------

if 
[ ! -s "$PASSWORD_FILE" ]
; then
    echo "Generating vault password..."

    umask 077

    openssl rand -base64 48 \
        | tr -d '\n' \
        > "$PASSWORD_FILE"

    chmod 600 "$PASSWORD_FILE"
fi

if 
[ ! -s "$ADMIN_PASSWORD_FILE" ]
; then
    echo "Generating admin password..."

    umask 077

    openssl rand -base64 48 \
        | tr -d '\n' \
        > "$ADMIN_PASSWORD_FILE"

    chmod 600 "$ADMIN_PASSWORD_FILE"

    echo
    echo "============================================================"
    echo "XGift admin password generated:"
    cat "$ADMIN_PASSWORD_FILE"
    echo
    echo "SAVE THIS PASSWORD."
    echo "It is stored in the persistent /app/config volume."
    echo "============================================================"
    echo
fi

chmod 600 "$PASSWORD_FILE" "$ADMIN_PASSWORD_FILE"

# ------------------------------------------------------------
# Caddy configuration
#
# Zeabur exposes port 8080.
# Caddy proxies internally to xgift-web on 127.0.0.1:8787.
#
# TLS is handled by Zeabur's public proxy, so Caddy runs HTTP
# internally.
# ------------------------------------------------------------

cat > /etc/caddy/Caddyfile <<EOF
:8080 {
    reverse_proxy 127.0.0.1:8787

    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "SAMEORIGIN"
        Referrer-Policy "same-origin"
    }

    encode gzip
}
EOF

# ------------------------------------------------------------
# Start xgift-web
# ------------------------------------------------------------

echo "Starting xgift-web..."

/app/bin/xgift-web &
XGIFT_PID=$!

# ------------------------------------------------------------
# Wait until backend is alive
# ------------------------------------------------------------

echo "Waiting for xgift-web..."

for i in $(seq 1 60); do
    if curl -fsS http://127.0.0.1:8787/healthz >/dev/null 2>&1; then
        echo "xgift-web is ready."
        break
    fi

    if ! kill -0 "$XGIFT_PID" 2>/dev/null; then
        echo "ERROR: xgift-web exited during startup."
        wait "$XGIFT_PID"
        exit 1
    fi

    sleep 1
done

if ! curl -fsS http://127.0.0.1:8787/healthz >/dev/null 2>&1; then
    echo "ERROR: xgift-web did not become ready."
    exit 1
fi

# ------------------------------------------------------------
# Start Caddy
# ------------------------------------------------------------

echo "Starting Caddy on :8080..."

caddy run --config /etc/caddy/Caddyfile --adapter caddyfile &
CADDY_PID=$!

# ------------------------------------------------------------
# Graceful shutdown
# ------------------------------------------------------------

shutdown() {
    echo "Stopping services..."

    kill -TERM "$CADDY_PID" 2>/dev/null || true
    kill -TERM "$XGIFT_PID" 2>/dev/null || true

    wait "$CADDY_PID" 2>/dev/null || true
    wait "$XGIFT_PID" 2>/dev/null || true
}

trap shutdown SIGTERM SIGINT

# ------------------------------------------------------------
# Keep container alive while either service is running
# ------------------------------------------------------------

while true; do
    if ! kill -0 "$XGIFT_PID" 2>/dev/null; then
        echo "xgift-web stopped."
        wait "$XGIFT_PID" || true
        exit 1
    fi

    if ! kill -0 "$CADDY_PID" 2>/dev/null; then
        echo "Caddy stopped."
        wait "$CADDY_PID" || true
        exit 1
    fi

    sleep 5
done
