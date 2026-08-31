#!/usr/bin/env bash
# Installs (or upgrades) the AgentMux headless server on Linux.
#
# Everything happens as the invoking user: the binary lands in ~/.local/bin,
# state in ~/.config/AgentMux, and the service — when systemd offers user
# units — in ~/.config/systemd/user. No root, no /usr, no global anything,
# so it works on a box where you are one account among many.
#
#   curl -fsSL https://raw.githubusercontent.com/tan-zhuo/AgentMux/main/scripts/install-server.sh | bash
#
# Options (after `bash -s --` when piping):
#   --mirror URL     proxy prefix for networks that cannot reach GitHub,
#                    e.g. --mirror https://ghfast.top  (the in-app update
#                    mirror setting uses the same convention)
#   --version vX.Y.Z install a specific release instead of the latest
#   --prefix DIR     where the binary goes (default ~/.local/bin)
#   --addr ADDR      listen address for the service (default :8642)
#   --no-service     install the binary only; print how to run it by hand
set -euo pipefail

REPO="tan-zhuo/AgentMux"
MIRROR="${AGENTMUX_MIRROR:-}"
VERSION=""
PREFIX="$HOME/.local/bin"
ADDR=":8642"
SERVICE=1

while [ $# -gt 0 ]; do
  case "$1" in
    --mirror)  MIRROR="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --prefix)  PREFIX="$2"; shift 2 ;;
    --addr)    ADDR="$2"; shift 2 ;;
    --no-service) SERVICE=0; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

case "$(uname -s)" in
  Linux) ;;
  *) echo "this script installs the Linux server build; on $(uname -s) use the desktop app or build from source" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "no server build is published for $(uname -m) — build from source: go build -tags headless" >&2; exit 1 ;;
esac

ASSET="agentmux-server-linux-$ARCH.tar.gz"
if [ -n "$VERSION" ]; then
  BASE="https://github.com/$REPO/releases/download/$VERSION"
else
  BASE="https://github.com/$REPO/releases/latest/download"
fi

# The mirror convention is a plain prefix: MIRROR/https://github.com/…
url() {
  if [ -n "$MIRROR" ]; then echo "${MIRROR%/}/$1"; else echo "$1"; fi
}

fetch() { # fetch URL FILE
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 15 --retry 2 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 20 -O "$2" "$1"
  else
    echo "neither curl nor wget is available" >&2; exit 1
  fi
}

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "downloading $ASSET${VERSION:+ ($VERSION)}${MIRROR:+ via $MIRROR}..."
fetch "$(url "$BASE/$ASSET")" "$TMP/$ASSET"
fetch "$(url "$BASE/$ASSET.sha256")" "$TMP/$ASSET.sha256"

# The checksum file reads "<hex>  <name>"; the name must match what sits in
# this directory, so rewrite it rather than trusting the published one.
SUM="$(awk '{print $1}' "$TMP/$ASSET.sha256")"
echo "$SUM  $ASSET" | (cd "$TMP" && sha256sum -c - >/dev/null)
echo "checksum verified: $SUM"

tar -xzf "$TMP/$ASSET" -C "$TMP"
BIN="$TMP/agentmux-server-linux-$ARCH/agentmux"
[ -f "$BIN" ] || { echo "the archive does not contain the agentmux binary" >&2; exit 1; }

mkdir -p "$PREFIX"
# Replace via rename so an already-running server keeps its old inode and a
# supervisor restart picks up the new one — never a half-written binary.
install -m 0755 "$BIN" "$PREFIX/agentmux.new"
mv -f "$PREFIX/agentmux.new" "$PREFIX/agentmux"
echo "installed to $PREFIX/agentmux"

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) echo "note: $PREFIX is not on your PATH" ;;
esac

if [ "$SERVICE" = 1 ] && command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
  UNIT_DIR="$HOME/.config/systemd/user"
  mkdir -p "$UNIT_DIR"
  cat > "$UNIT_DIR/agentmux.service" <<EOF
[Unit]
Description=AgentMux headless server
After=network-online.target

[Service]
ExecStart=$PREFIX/agentmux --addr $ADDR
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable agentmux >/dev/null 2>&1 || true
  # restart rather than start: a re-run of this script is an upgrade, and the
  # running service should move onto the binary that was just installed.
  systemctl --user restart agentmux
  # Without linger the whole user session — and this service with it — ends
  # when the last SSH connection closes, which on a server is always.
  if ! loginctl enable-linger "$USER" 2>/dev/null; then
    echo "warning: could not enable linger — the service will stop when you log out."
    echo "         ask an administrator to run: loginctl enable-linger $USER"
  fi
  sleep 1
  echo
  echo "AgentMux is running on $ADDR (systemd user service 'agentmux')."
  echo "  status:  systemctl --user status agentmux"
  echo "  logs:    journalctl --user -u agentmux -f"
else
  [ "$SERVICE" = 1 ] && echo "systemd user services are not available here; run it by hand:"
  echo
  echo "  mkdir -p ~/.config/AgentMux && nohup $PREFIX/agentmux --addr $ADDR >> ~/.config/AgentMux/serve.log 2>&1 &"
fi

PORT="${ADDR##*:}"
echo
echo "access token (created on first start):"
echo "  cat ~/.config/AgentMux/serve-token"
echo "open http://<this-host>:$PORT and enter the token once. Upgrades can be"
echo "applied from the web UI's update banner, or by re-running this script."
