#!/bin/bash
set -euo pipefail

DATA_DIR="${XGIFT_DATA_DIR:-/app/data}"
CONFIG_DIR="/app/config"

# Prefer an explicitly configured HTTPS origin. If the template contains a
# bare DOMAIN value (for example "gift.zeabur.app"), fall back to Zeabur's
# canonical URL. During first deployment there may be no domain yet, so an
# empty origin is allowed; the app will then skip origin matching.
ORIGIN="${XGIFT_ORIGIN:-}"
case "$ORIGIN" in
    https://*) ;;
    "") ORIGIN="${ZEABUR_WEB_URL:-}" ;;
    *) ORIGIN="${ZEABUR_WEB_URL:-}" ;;
esac

LISTEN_ADDR="${XGIFT_LISTEN:-127.0.0.1:8787}"
PASSWORD_FILE="${XGIFT_PASSWORD_FILE:-${CONFIG_DIR}/vault-password}"
ADMIN_PASSWORD_FILE="${XGIFT_ADMIN_PASSWORD_FILE:-${CONFIG_DIR}/admin-password}"

export XGIFT_DATA_DIR="$DATA_DIR"
export XGIFT_LISTEN="$LISTEN_ADDR"
export XGIFT_ORIGIN="$ORIGIN"
export XGIFT_PASSWORD_FILE="$PASSWORD_FILE"
export XGIFT_ADMIN_PASSWORD_FILE="$ADMIN_PASSWORD_FILE"

mkdir -p "$DATA_DIR" "$CONFIG_DIR"
chmod 700 "$DATA_DIR" "$CONFIG_DIR"

# If an origin is configured, it must be HTTPS. An origin is optional before
# a Zeabur domain has been generated.
if [ -n "$ORIGIN" ]; then
    case "$ORIGIN" in
        https://*) ;;
        *)
            echo "ERROR: XGIFT_ORIGIN/ZEABUR_WEB_URL must start with https://"
            exit 1
            ;;
    esac
fi

# ------------------------------------------------------------
# Password files
# ------------------------------------------------------------

if [ ! -s "$PASSWORD_FILE" ]; then
    echo "Generating vault password..."
    umask 077
    openssl rand -base64 48 | tr -d '\n' > "$PASSWORD_FILE"
    chmod 600 "$PASSWORD_FILE"
fi

if [ ! -s "$ADMIN_PASSWORD_FILE" ] && [ -s "$DATA_DIR/vault.db" ]; then
    echo "Generating admin password for an existing vault..."
    umask 077
    openssl rand -base64 48 | tr -d '\n' > "$ADMIN_PASSWORD_FILE"
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

if [ -s "$DATA_DIR/payments-enabled" ]; then
    XGIFT_PAYMENTS_ENABLED="$(tr -d '\r\n' < "$DATA_DIR/payments-enabled")"
    export XGIFT_PAYMENTS_ENABLED
elif [ -z "${XGIFT_PAYMENTS_ENABLED+x}" ]; then
    XGIFT_PAYMENTS_ENABLED="false"
    export XGIFT_PAYMENTS_ENABLED
fi

chmod 600 "$PASSWORD_FILE"
if [ -s "$ADMIN_PASSWORD_FILE" ]; then
    chmod 600 "$ADMIN_PASSWORD_FILE"
fi

echo "Starting xgift-web..."
/app/bin/xgift-web &
XGIFT_PID=$!

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

# During first-run setup, inject the server-generated one-time token into
# the private loopback request. The browser never needs to see or copy it.
SETUP_TOKEN=""
if [ -s "$DATA_DIR/setup-token" ]; then
    SETUP_TOKEN="$(tr -d '\r\n' < "$DATA_DIR/setup-token")"
fi

cat > /etc/caddy/Caddyfile <<EOF
:8080 {
    reverse_proxy 127.0.0.1:8787 {
        header_up X-XGift-Setup-Token "$SETUP_TOKEN"
    }

    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "SAMEORIGIN"
        Referrer-Policy "same-origin"
    }

    encode gzip
}
EOF

echo "Starting Caddy on :8080..."
caddy run --config /etc/caddy/Caddyfile --adapter caddyfile &
CADDY_PID=$!

shutdown() {
    echo "Stopping services..."
    kill -TERM "$CADDY_PID" 2>/dev/null || true
    kill -TERM "$XGIFT_PID" 2>/dev/null || true
    wait "$CADDY_PID" 2>/dev/null || true
    wait "$XGIFT_PID" 2>/dev/null || true
}

trap shutdown SIGTERM SIGINT

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
