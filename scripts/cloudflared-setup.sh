#!/usr/bin/env bash
#
# Creates a cloudflared named tunnel, adds a DNS route, and writes the config file.
#
# Requires:
#   - cloudflared installed and authenticated (cloudflared login)
#   - BACKFLOW_TUNNEL_NAME and BACKFLOW_DOMAIN set in .env
#
# Usage: make cloudflared-setup
set -euo pipefail

: "${BACKFLOW_TUNNEL_NAME:?Set BACKFLOW_TUNNEL_NAME in .env (e.g. bbell.dev)}"
: "${BACKFLOW_DOMAIN:?Set BACKFLOW_DOMAIN in .env (e.g. backflow.bbell.dev)}"

CONFIG_DIR="${HOME}/.cloudflared"
CONFIG_FILE="${CONFIG_DIR}/config.yml"

# --- 1. Ensure cloudflared is authenticated ---
if [ ! -f "${CONFIG_DIR}/cert.pem" ]; then
  echo "No cloudflared credentials found. Running 'cloudflared login'..."
  cloudflared login
fi

# --- 2. Create tunnel (idempotent — skips if it already exists) ---
if cloudflared tunnel list --name "${BACKFLOW_TUNNEL_NAME}" 2>/dev/null | grep -q "${BACKFLOW_TUNNEL_NAME}"; then
  echo "Tunnel '${BACKFLOW_TUNNEL_NAME}' already exists"
else
  echo "Creating tunnel '${BACKFLOW_TUNNEL_NAME}'..."
  cloudflared tunnel create "${BACKFLOW_TUNNEL_NAME}"
fi

# --- 3. Get tunnel UUID ---
TUNNEL_ID=$(cloudflared tunnel list --name "${BACKFLOW_TUNNEL_NAME}" -o json 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
CREDS_FILE="${CONFIG_DIR}/${TUNNEL_ID}.json"

if [ ! -f "${CREDS_FILE}" ]; then
  echo "ERROR: Credentials file not found at ${CREDS_FILE}"
  exit 1
fi

echo "Tunnel ID: ${TUNNEL_ID}"

# --- 4. Add DNS route (idempotent — cloudflared skips if already configured) ---
echo "Adding DNS route: ${BACKFLOW_DOMAIN} → ${BACKFLOW_TUNNEL_NAME}..."
cloudflared tunnel route dns "${BACKFLOW_TUNNEL_NAME}" "${BACKFLOW_DOMAIN}"

# --- 5. Write config file ---
echo "Writing ${CONFIG_FILE}..."
cat > "${CONFIG_FILE}" <<EOF
tunnel: ${BACKFLOW_TUNNEL_NAME}
credentials-file: ${CREDS_FILE}

ingress:
  - hostname: ${BACKFLOW_DOMAIN}
    service: http://localhost:8080
  - service: http_status:404
EOF

echo ""
echo "Setup complete. Run 'make tunnel' to start the tunnel."
echo "  ${BACKFLOW_DOMAIN} → http://localhost:8080"
