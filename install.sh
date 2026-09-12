#!/bin/sh
# Captain installer. Interactive by default; every answer can be passed as a flag.
#   curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh
#   ... | sh -s -- --mode docker --domain panel.example.com --email you@example.com \
#                  --admin-email you@example.com --admin-password 'long-secret'
#   ... | sh -s -- uninstall [--keep-data]     remove everything this script set up
# Modes: docker (default when Docker is present) or binary (systemd service).
# Piped through sh the script never touches the disk; nothing to clean up
# afterwards besides the installation itself.
# TLS: Captain gets a Let's Encrypt certificate itself; ports 80/443 must be free.
# Use --behind-proxy when something else on the host terminates TLS (Captain
# then listens on 127.0.0.1:8080 over plain HTTP).
set -eu

REPO="zeptop-dev/captain"
IMAGE="zeptop/captain:latest"
MODE="" DOMAIN="" EMAIL="" CF_TOKEN="" ADMIN_EMAIL="" ADMIN_PASS="" PROXY=0 VERSION="" ACTION=install KEEP_DATA=0
while [ $# -gt 0 ]; do
  case "$1" in
    uninstall) ACTION=uninstall; shift ;;
    --keep-data) KEEP_DATA=1; shift ;;
    --mode) MODE="$2"; shift 2 ;;
    --domain) DOMAIN="$2"; shift 2 ;;
    --email) EMAIL="$2"; shift 2 ;;
    --cloudflare-token) CF_TOKEN="$2"; shift 2 ;;
    --admin-email) ADMIN_EMAIL="$2"; shift 2 ;;
    --admin-password) ADMIN_PASS="$2"; shift 2 ;;
    --behind-proxy) PROXY=1; shift ;;
    --version) VERSION="$2"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

if [ "$ACTION" = uninstall ]; then
  echo "This removes Captain: service/containers, /opt/captain, /etc/captain$( [ "$KEEP_DATA" = 1 ] || echo ', the database, backups and certificates')."
  if [ -r /dev/tty ]; then printf 'Type yes to continue: ' >/dev/tty; read -r ans </dev/tty; [ "$ans" = yes ] || { echo "aborted"; exit 1; }; fi
  if [ -f /opt/captain/docker-compose.yml ] && command -v docker >/dev/null 2>&1; then
    (cd /opt/captain && if [ "$KEEP_DATA" = 1 ]; then docker compose down; else docker compose down -v; fi) || true
  fi
  if systemctl list-unit-files captain.service >/dev/null 2>&1; then
    systemctl disable --now captain 2>/dev/null || true
    rm -f /etc/systemd/system/captain.service; systemctl daemon-reload
  fi
  rm -rf /opt/captain /etc/captain
  [ "$KEEP_DATA" = 1 ] || rm -rf /var/lib/captain
  id captain >/dev/null 2>&1 && userdel captain 2>/dev/null || true
  echo "Captain removed.$( [ "$KEEP_DATA" = 1 ] && echo ' Data kept in /var/lib/captain (binary install) or the captain-data docker volume.')"
  exit 0
fi

# Read from the terminal even when the script itself comes through a pipe.
ask() { # var prompt [default]
  eval "cur=\${$1:-}"
  if [ -n "$cur" ]; then return; fi
  if [ ! -r /dev/tty ]; then echo "$2 is required; pass --$3" >&2; exit 1; fi
  printf '%s%s: ' "$2" "${4:+ [$4]}" >/dev/tty
  read -r val </dev/tty
  [ -n "$val" ] || val="${4:-}"
  eval "$1=\"\$val\""
}
ask_secret() {
  eval "cur=\${$1:-}"
  if [ -n "$cur" ]; then return; fi
  [ -r /dev/tty ] || { echo "$2 is required; pass --$3" >&2; exit 1; }
  printf '%s: ' "$2" >/dev/tty; stty -echo </dev/tty; read -r val </dev/tty; stty echo </dev/tty; echo >/dev/tty
  eval "$1=\"\$val\""
}

if [ -z "$MODE" ]; then
  if command -v docker >/dev/null 2>&1; then MODE=docker; else MODE=binary; fi
  ask MODE "Install with docker or binary (systemd)?" mode "$MODE"
fi
case "$MODE" in docker|binary) ;; *) echo "mode must be docker or binary" >&2; exit 1 ;; esac
if [ "$MODE" = docker ]; then
  command -v docker >/dev/null 2>&1 || { echo "Docker is not installed. Install it first (https://docs.docker.com/engine/install/ or 'curl -fsSL https://get.docker.com | sh'), then run this script again." >&2; exit 1; }
  docker compose version >/dev/null 2>&1 || { echo "Docker Compose plugin missing (docker compose version fails). Install docker-compose-plugin, then retry." >&2; exit 1; }
fi

ask DOMAIN "Panel domain (DNS A record must point here)" domain
if [ "$PROXY" = 0 ]; then
  ask EMAIL "Email for the Let's Encrypt account" email
  if [ -z "$CF_TOKEN" ] && [ -r /dev/tty ]; then
    printf '%s' "Cloudflare API token for a wildcard certificate via DNS-01 (Enter to skip and use HTTP-01 on port 80): " >/dev/tty
    stty -echo </dev/tty; read -r CF_TOKEN </dev/tty; stty echo </dev/tty; echo >/dev/tty
  fi
fi
ask ADMIN_EMAIL "Admin login email" admin-email "$EMAIL"
ask_secret ADMIN_PASS "Admin password (8+ characters)" admin-password
[ ${#ADMIN_PASS} -ge 8 ] || { echo "password too short" >&2; exit 1; }

DIR=/opt/captain
mkdir -p "$DIR" /etc/captain
if [ "$PROXY" = 1 ]; then
  LISTEN="0.0.0.0:8080"; [ "$MODE" = binary ] && LISTEN="127.0.0.1:8080"
  TLS="tls:
  auto: false"
else
  LISTEN="0.0.0.0:443"
  TLS="tls:
  auto: true
  email: $EMAIL"
  [ -n "$CF_TOKEN" ] && TLS="$TLS
  cloudflare_token: $CF_TOKEN"
fi
CFG=/etc/captain/config.yaml
[ "$MODE" = docker ] && CFG="$DIR/config.yaml"
if [ ! -f "$CFG" ]; then
  cat > "$CFG" <<YAML
listen: $LISTEN
base_url: https://$DOMAIN
data_dir: /var/lib/captain
log_level: info

$TLS

database:
  driver: sqlite
  dsn: /var/lib/captain/captain.db

agent:
  pull_seconds: 60
  push_seconds: 60

site_name: Captain

portal:
  registration: true

payments: {}
YAML
  chmod 0600 "$CFG"
  echo "wrote $CFG"
else
  echo "keeping existing $CFG"
fi

if [ "$MODE" = docker ]; then
  if [ "$PROXY" = 1 ]; then PORTS='["127.0.0.1:8080:8080"]'; else PORTS='["80:80", "443:443"]'; fi
  cat > "$DIR/docker-compose.yml" <<YAML
services:
  captain:
    image: $IMAGE
    restart: unless-stopped
    ports: $PORTS
    volumes:
      - ./config.yaml:/etc/captain/config.yaml:ro
      - captain-data:/var/lib/captain
volumes:
  captain-data:
YAML
  cd "$DIR"
  docker compose pull -q
  docker compose up -d
  sleep 4
  docker compose exec -T captain captain admin create -c /etc/captain/config.yaml -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" || true
  echo
  echo "Captain is running (docker). Console: https://$DOMAIN/admin/   Users: https://$DOMAIN/portal/"
  echo "The certificate is requested on the first visit; watch it with: docker compose logs -f"
  echo "Manage: cd $DIR && docker compose logs -f | docker compose pull && docker compose up -d"
else
  case "$(uname -m)" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) echo "unsupported arch" >&2; exit 1 ;; esac
  if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  fi
  B="https://github.com/$REPO/releases/download/$VERSION"
  TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
  echo "downloading captain $VERSION ($ARCH)"
  curl -fsSL -o "$TMP/captain" "$B/captain-linux-$ARCH"
  curl -fsSL -o "$TMP/SHA256SUMS" "$B/SHA256SUMS"
  WANT=$(grep " captain-linux-$ARCH\$" "$TMP/SHA256SUMS" | cut -d' ' -f1); GOT=$(sha256sum "$TMP/captain" | cut -d' ' -f1)
  [ "$WANT" = "$GOT" ] || { echo "checksum mismatch" >&2; exit 1; }
  id captain >/dev/null 2>&1 || useradd --system --home /var/lib/captain --shell /usr/sbin/nologin captain
  install -m 0755 "$TMP/captain" "$DIR/captain"
  mkdir -p /var/lib/captain; chown -R captain:captain /var/lib/captain "$DIR"; chown captain "$CFG"
  curl -fsSL -o /etc/systemd/system/captain.service "https://raw.githubusercontent.com/$REPO/$VERSION/deploy/captain.service"
  systemctl daemon-reload; systemctl enable --now captain; systemctl restart captain
  sleep 3
  sudo -u captain "$DIR/captain" admin create -c "$CFG" -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" || true
  echo
  echo "Captain is running (systemd, $VERSION). Console: https://$DOMAIN/admin/   Users: https://$DOMAIN/portal/"
  echo "Manage: journalctl -u captain -f | updates from the console (Settings -> Version)"
fi
