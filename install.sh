#!/bin/sh
# Captain installer. Interactive by default; every answer can be passed as a flag.
#   curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh
#   ... | sh -s -- --mode docker --domain panel.example.com --email you@example.com \
#                  --admin-email you@example.com --admin-password 'long-secret'
#   ... | sh -s -- uninstall [--keep-data]     remove everything this script set up
# Modes: docker (default when Docker is present) or binary (systemd service).
# Piped through sh the script never touches the disk; nothing to clean up
# afterwards besides the installation itself.
# TLS: Captain gets a Let's Encrypt certificate itself when ports 80/443 are free.
# When something else (nginx, OpenResty/1Panel, Caddy) already owns them the script
# switches to --behind-proxy: Captain listens on 127.0.0.1:8080 in plain HTTP and
# prints the reverse-proxy snippet; that proxy then terminates TLS.
# --reconfigure rewrites config.yaml (backup kept) when switching modes.
# Use --behind-proxy when something else on the host terminates TLS (Captain
# then listens on 127.0.0.1:8080 over plain HTTP).
set -eu

REPO="zeptop-dev/captain"
IMAGE="zeptop/captain:latest"
MODE="" DOMAIN="" EMAIL="" CF_TOKEN="" ADMIN_EMAIL="" ADMIN_PASS="" PROXY=0 VERSION="" ACTION=install KEEP_DATA=0 RECONFIG=0
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
    --reconfigure) RECONFIG=1; shift ;;
    --version) VERSION="$2"; shift 2 ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
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
# Read a secret from the terminal, echoing one * per character (backspace works).
read_masked() { # var
  val=""
  old=$(stty -g </dev/tty)
  stty raw -echo </dev/tty
  while :; do
    ch=$(dd bs=1 count=1 </dev/tty 2>/dev/null)
    case "$ch" in
      ""|"$(printf '\r')"|"$(printf '\n')") break ;;
      "$(printf '\177')"|"$(printf '\b')")
        if [ -n "$val" ]; then val=${val%?}; printf '\b \b' >/dev/tty; fi ;;
      "$(printf '\3')") stty "$old" </dev/tty; echo >/dev/tty; exit 130 ;;
      *) val="$val$ch"; printf '*' >/dev/tty ;;
    esac
  done
  stty "$old" </dev/tty
  echo >/dev/tty
  eval "$1=\"\$val\""
}
ask_secret() {
  eval "cur=\${$1:-}"
  if [ -n "$cur" ]; then return; fi
  [ -r /dev/tty ] || { echo "$2 is required; pass --$3" >&2; exit 1; }
  printf '%s: ' "$2" >/dev/tty
  read_masked "$1"
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

# Who owns a listening TCP port: "nginx", "docker:<container>" or "".
port_owner() { # port
  pid=$(ss -ltnpH "sport = :$1" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1)
  [ -n "$pid" ] || return 0
  name=$(ps -o comm= -p "$pid" 2>/dev/null)
  if [ "$name" = docker-proxy ] || [ -z "$name" ]; then
    c=$(docker ps --filter "publish=$1" --format '{{.Names}}' 2>/dev/null | head -1)
    [ -n "$c" ] && { echo "docker:$c"; return 0; }
  fi
  echo "$name"
}
OWNER=$(port_owner 80); [ -n "$OWNER" ] || OWNER=$(port_owner 443)
if [ "$PROXY" = 0 ]; then
  if [ -n "$OWNER" ]; then
    echo "Ports 80/443 are already used by: $OWNER"
    if [ -r /dev/tty ]; then
      printf '%s' "Run Captain behind it as a reverse proxy (127.0.0.1:8080, it terminates TLS)? [Y/n]: " >/dev/tty
      read -r yn </dev/tty
      case "$yn" in n|N) echo "Free ports 80/443 or rerun with --behind-proxy." >&2; exit 1 ;; esac
    else
      echo "Rerun with --behind-proxy, or free the ports." >&2; exit 1
    fi
    PROXY=1
  fi
fi

ask DOMAIN "Panel domain (DNS A record must point here)" domain
if [ "$PROXY" = 0 ]; then
  ask EMAIL "Email for the Let's Encrypt account" email
  if [ -z "$CF_TOKEN" ] && [ -r /dev/tty ]; then
    printf '%s' "Cloudflare API token for a wildcard certificate via DNS-01 (Enter to skip and use HTTP-01 on port 80): " >/dev/tty
    read_masked CF_TOKEN
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
if [ -f "$CFG" ] && [ "$RECONFIG" = 0 ]; then
  if grep -q '^  auto: true' "$CFG"; then HAD=0; else HAD=1; fi
  if [ "$HAD" != "$PROXY" ]; then echo "existing $CFG was written for another TLS mode; rewriting it (backup: $CFG.bak)"; RECONFIG=1; fi
fi
if [ -f "$CFG" ] && [ "$RECONFIG" = 1 ]; then cp "$CFG" "$CFG.bak"; rm -f "$CFG"; fi
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

UPSTREAM="http://127.0.0.1:8080"
if [ "$MODE" = docker ]; then
  if [ "$PROXY" = 1 ]; then PORTS='["127.0.0.1:8080:8080"]'; else PORTS='["80:80", "443:443"]'; fi
  NETS=""; NETDEF=""
  # A containerised proxy (1Panel's OpenResty, nginx-proxy...) on a bridge network
  # cannot reach 127.0.0.1 of the host: join its network so it can use http://captain:8080.
  if [ "$PROXY" = 1 ]; then
    case "$OWNER" in docker:*)
      PC=${OWNER#docker:}
      if [ "$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$PC" 2>/dev/null)" != host ]; then
        PNET=$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$PC" 2>/dev/null | tr ' ' '\n' | grep -v '^$' | head -1)
        if [ -n "$PNET" ]; then
          NETS="    networks: [default, $PNET]"
          NETDEF="networks:
  $PNET:
    external: true"
          UPSTREAM="http://captain:8080"
        fi
      fi ;;
    esac
  fi
  cat > "$DIR/docker-compose.yml" <<YAML
services:
  captain:
    image: $IMAGE
    container_name: captain
    restart: unless-stopped
    ports: $PORTS
$NETS
    volumes:
      - ./config.yaml:/etc/captain/config.yaml:ro
      - captain-data:/var/lib/captain
$NETDEF
volumes:
  captain-data:
YAML
  sed -i '/^$/d' "$DIR/docker-compose.yml"
  cd "$DIR"
  docker compose pull -q
  docker compose up -d
  sleep 4
  docker compose exec -T captain captain admin create -c /etc/captain/config.yaml -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" || true
  echo
  echo "Captain is running (docker). Console: https://$DOMAIN/admin/   Users: https://$DOMAIN/portal/"
  [ "$PROXY" = 1 ] || echo "The certificate is requested at startup; watch it with: docker compose logs -f"
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

if [ "$PROXY" = 1 ]; then
  cat <<EOT

Captain listens on $UPSTREAM in plain HTTP. Point your reverse proxy at it and
let the proxy hold the certificate for $DOMAIN (and any subscription domains):

  nginx / OpenResty (1Panel: Websites -> Create -> Reverse proxy, domain $DOMAIN,
  proxy address $UPSTREAM, then enable HTTPS on that site):

    location / {
        proxy_pass $UPSTREAM;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 120s;
    }

  Caddy:  $DOMAIN { reverse_proxy $UPSTREAM }
EOT
fi

