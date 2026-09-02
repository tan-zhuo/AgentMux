#!/usr/bin/env bash
# Installs (or upgrades) the AgentMux headless server on Linux — and does not
# say "done" until the server is actually answering.
#
# Everything happens as the invoking user: the binary lands in ~/.local/bin,
# state in ~/.config/AgentMux, and the service — when systemd offers user
# units — in ~/.config/systemd/user. Where systemd is absent, the server is
# started directly and pinned to reboot via crontab. No root, no /usr, no
# global anything, so it works on a box where you are one account among many.
#
#   curl -fsSL https://raw.githubusercontent.com/tan-zhuo/AgentMux/main/scripts/install-server.sh | bash
#
# Options (after `bash -s --` when piping):
#   --mirror URL     proxy prefix for networks that cannot reach GitHub,
#                    e.g. --mirror https://ghfast.top  (the in-app update
#                    mirror setting uses the same convention)
#   --version vX.Y.Z install a specific release instead of the latest
#   --prefix DIR     where the binary goes (default ~/.local/bin)
#   --addr ADDR      listen address (default :8642)
#   --no-tls         plain HTTP (default is HTTPS with a self-signed
#                    certificate; clients pin its fingerprint on first use)
#   --no-service     install the binary only; nothing is started
#
# Re-running the script upgrades in place. A previously configured service
# keeps its address and TLS choice unless you pass them explicitly again.
# Nothing is ever half-installed: the binary is swapped only after its
# checksum verifies, by rename.
set -euo pipefail

say() { printf '%s\n' "$*"; }
die() { printf '\nerror: %s\n' "$*" >&2; exit 1; }

REPO="tan-zhuo/AgentMux"
MIRROR="${AGENTMUX_MIRROR:-}"
VERSION=""
PREFIX="$HOME/.local/bin"
ADDR=":8642"
TLS=1
SERVICE=1
ADDR_SET=0
TLS_SET=0

while [ $# -gt 0 ]; do
  case "$1" in
    --mirror)  MIRROR="${2:?--mirror needs a value}"; shift 2 ;;
    --version) VERSION="${2:?--version needs a value}"; shift 2 ;;
    --prefix)  PREFIX="${2:?--prefix needs a value}"; shift 2 ;;
    --addr)    ADDR="${2:?--addr needs a value}"; ADDR_SET=1; shift 2 ;;
    --no-tls)  TLS=0; TLS_SET=1; shift ;;
    --tls)     TLS=1; TLS_SET=1; shift ;;
    --no-service) SERVICE=0; shift ;;
    *) die "unknown option: $1" ;;
  esac
done

case "$(uname -s)" in
  Linux) ;;
  *) die "this script installs the Linux server build; on $(uname -s) use the desktop app or build from source" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "no server build is published for $(uname -m) — build from source: go build -tags headless" ;;
esac

# ── fetch ────────────────────────────────────────────────────────────────────
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
    curl -fsSL --connect-timeout 15 --retry 3 --retry-delay 2 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 20 -t 3 -O "$2" "$1"
  else
    die "neither curl nor wget is available — install one and re-run"
  fi
}

probe() { # probe URL — is the server answering?
  if command -v curl >/dev/null 2>&1; then
    curl -fsk -o /dev/null -m 3 "$1"
  else
    wget -q --no-check-certificate -T 3 -O /dev/null "$1"
  fi
}

sha256_check() { # sha256_check FILE EXPECTED
  local got
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$1" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$1" | awk '{print $1}')
  else
    die "neither sha256sum nor shasum is available — cannot verify the download"
  fi
  [ "$got" = "$2" ] || die "checksum mismatch: the download does not match what was published (got $got, want $2). A mirror or proxy may be tampering; try without --mirror."
}

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

say "downloading $ASSET${VERSION:+ ($VERSION)}${MIRROR:+ via $MIRROR}..."
fetch "$(url "$BASE/$ASSET")" "$TMP/$ASSET" \
  || die "could not download $ASSET — check the network, or pass --mirror https://ghfast.top on a network where GitHub is blocked"
fetch "$(url "$BASE/$ASSET.sha256")" "$TMP/$ASSET.sha256" \
  || die "could not download the checksum file that verifies the build"

SUM="$(awk '{print $1}' "$TMP/$ASSET.sha256")"
[ ${#SUM} -eq 64 ] || die "the checksum file is not in sha256sum format"
sha256_check "$TMP/$ASSET" "$SUM"
say "checksum verified: $SUM"

tar -xzf "$TMP/$ASSET" -C "$TMP" || die "could not unpack the archive"
BIN="$TMP/agentmux-server-linux-$ARCH/agentmux"
[ -f "$BIN" ] || die "the archive does not contain the agentmux binary"

mkdir -p "$PREFIX" || die "cannot create $PREFIX"
# Replace via rename so an already-running server keeps its old inode and a
# restart picks up the new one — never a half-written binary.
install -m 0755 "$BIN" "$PREFIX/agentmux.new"
mv -f "$PREFIX/agentmux.new" "$PREFIX/agentmux"
say "installed to $PREFIX/agentmux"

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) say "note: $PREFIX is not on your PATH" ;;
esac

if [ "$SERVICE" = 0 ]; then
  TLS_FLAG=""; [ "$TLS" = 1 ] && TLS_FLAG=" --tls"
  say ""
  say "binary installed; nothing was started (--no-service). Run it with:"
  say "  $PREFIX/agentmux --addr $ADDR$TLS_FLAG"
  exit 0
fi

# ── run it ───────────────────────────────────────────────────────────────────
UNIT_DIR="$HOME/.config/systemd/user"
UNIT="$UNIT_DIR/agentmux.service"
DATA_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/AgentMux"

# An SSH session on a minimal box often lacks the variables systemd --user
# needs even though the manager is running; point at the standard runtime
# directory before concluding it is absent.
if [ -z "${XDG_RUNTIME_DIR:-}" ] && [ -d "/run/user/$(id -u)" ]; then
  export XDG_RUNTIME_DIR="/run/user/$(id -u)"
fi
HAVE_SYSTEMD=0
if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
  HAVE_SYSTEMD=1
fi

# A re-run is an upgrade: a service that was configured before keeps its
# address and TLS choice unless this run set them explicitly.
if [ -f "$UNIT" ]; then
  EXISTING_EXEC=$(grep -m1 '^ExecStart=' "$UNIT" | cut -d= -f2- || true)
  if [ -n "$EXISTING_EXEC" ]; then
    if [ "$ADDR_SET" = 0 ]; then
      KEPT=$(printf '%s\n' "$EXISTING_EXEC" | grep -o '\-\-addr [^ ]*' | awk '{print $2}' || true)
      [ -n "$KEPT" ] && ADDR="$KEPT"
    fi
    if [ "$TLS_SET" = 0 ]; then
      case "$EXISTING_EXEC" in *--tls*) TLS=1 ;; *) TLS=0 ;; esac
    fi
  fi
fi
# Ask the binary itself what it can do, and configure only that: a release
# from before TLS existed would otherwise be handed a flag it does not know
# and crash-loop under systemd — the exact opposite of installed.
if [ "$TLS" = 1 ] && ! "$PREFIX/agentmux" --serve --help 2>&1 | grep -q -- '-tls'; then
  say "note: this release predates built-in TLS; serving plain HTTP."
  say "      re-run this script after the next release to turn HTTPS on."
  TLS=0
fi

TLS_FLAG=""; [ "$TLS" = 1 ] && TLS_FLAG=" --tls"
PORT="${ADDR##*:}"
SCHEME=$([ "$TLS" = 1 ] && echo https || echo http)

if [ "$HAVE_SYSTEMD" = 1 ]; then
  mkdir -p "$UNIT_DIR"
  cat > "$UNIT" <<EOF
[Unit]
Description=AgentMux headless server
After=network-online.target

[Service]
ExecStart=$PREFIX/agentmux --addr $ADDR$TLS_FLAG
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable agentmux >/dev/null 2>&1 || true
  # restart rather than start: a re-run of this script is an upgrade, and the
  # running service must move onto the binary that was just installed.
  systemctl --user restart agentmux
  # Without linger the whole user session — and this service with it — ends
  # when the last SSH connection closes, which on a server is always.
  if ! loginctl enable-linger "$(id -un)" 2>/dev/null; then
    say "warning: could not enable linger — the service will stop when you log out."
    say "         ask an administrator to run: loginctl enable-linger $(id -un)"
  fi
else
  say "systemd user services are not available here; starting the server directly."
  # An upgrade must stop the old instance or the new one loses the port race.
  pkill -f "$PREFIX/agentmux --addr" 2>/dev/null && sleep 1 || true
  mkdir -p "$DATA_DIR"
  nohup "$PREFIX/agentmux" --addr "$ADDR"$TLS_FLAG >> "$DATA_DIR/serve.log" 2>&1 &
  disown || true
  # Pin it to reboots the one way a plain user account has.
  if command -v crontab >/dev/null 2>&1; then
    CRON_LINE="@reboot $PREFIX/agentmux --addr $ADDR$TLS_FLAG >> $DATA_DIR/serve.log 2>&1 # agentmux-server"
    ( crontab -l 2>/dev/null | grep -v '# agentmux-server$' ; printf '%s\n' "$CRON_LINE" ) | crontab - \
      && say "added a crontab @reboot entry so the server survives restarts" \
      || say "warning: could not write a crontab entry — start it by hand after a reboot"
  else
    say "warning: no crontab here — the server will not come back after a reboot on its own"
  fi
fi

# ── trust, but verify ────────────────────────────────────────────────────────
# Installed and started is not the same as working: wait for the server to
# answer before claiming success, and show its own words when it does not.
say "waiting for the server to answer..."
UP=0
for _ in $(seq 1 20); do
  if probe "$SCHEME://127.0.0.1:$PORT/manifest.webmanifest"; then UP=1; break; fi
  sleep 0.5
done
if [ "$UP" = 0 ]; then
  say ""
  say "the server did not come up on $ADDR. Its own account of why:"
  if [ "$HAVE_SYSTEMD" = 1 ]; then
    systemctl --user --no-pager -l status agentmux 2>&1 | tail -15 || true
    say ""
    say "  full log:  journalctl --user -u agentmux -e"
  else
    tail -15 "$DATA_DIR/serve.log" 2>/dev/null || true
    say ""
    say "  full log:  $DATA_DIR/serve.log"
  fi
  die "install completed but the server is not answering (a port already in use is the usual reason — pick another with --addr)"
fi

# ── the two values every device will ask for ─────────────────────────────────
TOKEN=""
for _ in $(seq 1 10); do
  TOKEN=$(cat "$DATA_DIR/serve-token" 2>/dev/null || true)
  [ -n "$TOKEN" ] && break
  sleep 0.5
done
FP=""
if [ "$TLS" = 1 ] && command -v openssl >/dev/null 2>&1 && [ -f "$DATA_DIR/serve-cert.pem" ]; then
  FP=$(openssl x509 -in "$DATA_DIR/serve-cert.pem" -noout -fingerprint -sha256 2>/dev/null | cut -d= -f2 || true)
fi
IP=$(hostname -I 2>/dev/null | awk '{print $1}' || true)

say ""
say "AgentMux is running."
say "  connect:      $SCHEME://${IP:-<this-host>}:$PORT"
say "  access token: ${TOKEN:-(read it later: cat $DATA_DIR/serve-token)}"
if [ "$TLS" = 1 ]; then
  say "  fingerprint:  ${FP:-(in the log: look for 'certificate SHA-256 fingerprint')}"
  say "the app shows this fingerprint on first connect — trust it only if it matches."
fi
say ""
say "upgrades: one click on the update banner in the web UI, or re-run this script."
