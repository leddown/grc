#!/usr/bin/env bash
# Installs grc-render on this host, so a GRC installation elsewhere can send it
# Typst and Pandoc work (DOCUMENT_TEMPLATES.md, "Rendering on another host").
# Run it from the repository as a user with sudo rights, on the host that
# should typeset -- a Wintermute rig node such as coven.
#
# It copies typst and pandoc into /usr/local/bin when they are only on your own
# PATH (the service cannot see your home directory), builds and installs the
# binary, creates the grc-render service user, writes /etc/grc/grc-render.env
# with a generated token, installs and starts the systemd unit, and prints the
# two lines to add to GRC's env file. Safe to re-run: an existing env file and
# its token are kept, and the binary and unit are updated.
#
#   GRC_HOST_IP=10.232.231.9   also add a ufw rule letting only that host in
#   GRC_RENDER_ADDR=0.0.0.0:8892   where to listen (default)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_PATH="/usr/local/bin/grc-render"
ENV_FILE="/etc/grc/grc-render.env"
UNIT_PATH="/etc/systemd/system/grc-render.service"
SERVICE_USER="grc-render"
ADDR="${GRC_RENDER_ADDR:-0.0.0.0:8892}"
PORT="${ADDR##*:}"
GRC_HOST_IP="${GRC_HOST_IP:-}"

for tool in go openssl curl; do
  command -v "$tool" >/dev/null 2>&1 || { echo "missing: $tool" >&2; exit 1; }
done

echo "==> typesetters"
for tool in typst pandoc; do
  if [ -x "/usr/local/bin/$tool" ] || [ -x "/usr/bin/$tool" ]; then
    echo "    $tool: $(command -v "$tool")"
    continue
  fi
  src="$(command -v "$tool" || true)"
  if [ -n "$src" ]; then
    sudo install -m 0755 "$src" "/usr/local/bin/$tool"
    echo "    $tool: copied $src to /usr/local/bin"
  else
    echo "    warning: $tool not found; its jobs are refused until it is in /usr/local/bin" >&2
  fi
done

echo "==> binary"
BUILD_TMP="$(mktemp)"
trap 'rm -f "$BUILD_TMP"' EXIT
(cd "$REPO_ROOT" && CGO_ENABLED=0 go build -trimpath -o "$BUILD_TMP" ./cmd/grc-render)
sudo install -m 0755 "$BUILD_TMP" "$BIN_PATH"
echo "    $BIN_PATH"

echo "==> service user"
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  sudo useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

echo "==> env file"
NEW_TOKEN=""
if sudo test -f "$ENV_FILE"; then
  echo "    $ENV_FILE exists; kept"
else
  NEW_TOKEN="$(openssl rand -hex 32)"
  sudo install -d -m 0755 /etc/grc
  printf 'GRC_RENDER_ADDR=%s\nGRC_RENDER_TOKEN=%s\nGRC_RENDER_TLS_CERT=\nGRC_RENDER_TLS_KEY=\nGRC_RENDER_CONCURRENCY=2\n' \
    "$ADDR" "$NEW_TOKEN" | sudo tee "$ENV_FILE" >/dev/null
  sudo chown "root:$SERVICE_USER" "$ENV_FILE"
  sudo chmod 0640 "$ENV_FILE"
  echo "    $ENV_FILE written"
fi

echo "==> systemd unit"
sudo install -m 0644 "$REPO_ROOT/deploy/grc-render.service" "$UNIT_PATH"
sudo systemctl daemon-reload
sudo systemctl enable grc-render >/dev/null 2>&1
sudo systemctl restart grc-render

if [ -n "$GRC_HOST_IP" ]; then
  echo "==> firewall"
  if command -v ufw >/dev/null 2>&1; then
    sudo ufw allow from "$GRC_HOST_IP" to any port "$PORT" proto tcp
  else
    echo "    ufw not found: allow $GRC_HOST_IP to port $PORT by hand" >&2
  fi
fi

echo "==> check"
TOKEN="$(sudo sed -n 's/^GRC_RENDER_TOKEN=//p' "$ENV_FILE")"
SCHEME=http
if sudo grep -q '^GRC_RENDER_TLS_CERT=.\+' "$ENV_FILE"; then SCHEME=https; fi
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsSk -H "Authorization: Bearer $TOKEN" "$SCHEME://127.0.0.1:$PORT/v1/engines" 2>/dev/null; then
    echo
    break
  fi
  sleep 1
done || true
sudo systemctl --no-pager --lines=5 status grc-render || true

HOST="$(hostname -f 2>/dev/null || hostname)"
echo
echo "On the GRC host, add to its env file (/etc/grc/grc.env) and restart grc:"
echo "  RENDER_URL=$SCHEME://$HOST:$PORT"
if [ -n "$NEW_TOKEN" ]; then
  echo "  RENDER_TOKEN=$NEW_TOKEN"
else
  echo "  RENDER_TOKEN=<GRC_RENDER_TOKEN from $ENV_FILE on this host>"
fi
if [ "$SCHEME" = https ]; then
  echo "  RENDER_CA_FILE=<the fleet CA's ca.pem, copied to the GRC host>"
fi
