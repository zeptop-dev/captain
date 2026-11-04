#!/bin/sh
# Captain installer. Interactive by default; every answer can be passed as a flag.
#   curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh
#   ... | sh -s -- --mode docker --domain panel.example.com --email you@example.com \
#                  --admin-email you@example.com --admin-password 'long-secret'
#   ... | sh -s -- uninstall [--keep-data]     remove everything this script set up
#   ... | sh -s -- upgrade [--version vX.Y.Z]
#   ... | sh -s -- enable-web-upgrade     register optional Docker web updater
#   ... | sh -s -- uninstall --yes        permanently remove app and data
#   ... | sh -s -- enable-agent-https --agent-https-port 2096
#   New Docker installs may also pass --agent-https-port 2096.
# Modes: docker (default when Docker is present) or binary (systemd service).
# Piped through sh the script never touches the disk; nothing to clean up
# afterwards besides the installation itself.
# TLS: Captain gets a Let's Encrypt certificate itself when ports 80/443 are free.
# When something else (nginx, OpenResty/1Panel, Caddy) already owns them the script
# switches to --behind-proxy: Captain listens on 127.0.0.1:8080 in plain HTTP and
# prints the reverse-proxy snippet; that proxy then terminates TLS.
# --reconfigure rewrites config.yaml (backup kept) when switching modes.
# --network bridge|host (docker mode): bridge (default) publishes only 80/443 (or
# 127.0.0.1:8080 behind a proxy) and can join a proxy container's network; it gets
# IPv6 when the host has a global address, so v6 clients keep their real address.
# host binds Captain straight to the host's interfaces: no NAT, no port mapping.
# Use --behind-proxy when something else on the host terminates TLS (Captain
# then listens on 127.0.0.1:8080 over plain HTTP).
set -eu

PRODUCT=captain NATIVE=/opt/captain/captain YES=0 WEB_UPGRADE=0
REPO="zeptop-dev/captain"
IMAGE="zeptop/captain:latest"
MODE="" DOMAIN="" EMAIL="" CF_TOKEN="" ADMIN_EMAIL="" ADMIN_PASS="" PROXY=0 VERSION="" ACTION=install KEEP_DATA=0 RECONFIG=0 AGENT_HTTPS_PORT=""
while [ $# -gt 0 ]; do
  case "$1" in
    install|upgrade|uninstall|enable-web-upgrade|enable-agent-https) ACTION="$1"; shift ;;
    --agent-https-port) [ $# -ge 2 ] || { echo "--agent-https-port requires a port" >&2; exit 2; }; AGENT_HTTPS_PORT="$2"; [ -n "$AGENT_HTTPS_PORT" ] || { echo "Agent HTTPS port cannot be empty" >&2; exit 2; }; shift 2 ;;
    --web-upgrade) WEB_UPGRADE=1; shift ;;
    --yes|-y) YES=1; shift ;;
    --keep-data) KEEP_DATA=1; shift ;;
    --mode) MODE="$2"; shift 2 ;;
    --domain) DOMAIN="$2"; shift 2 ;;
    --email) EMAIL="$2"; shift 2 ;;
    --cloudflare-token) CF_TOKEN="$2"; shift 2 ;;
    --admin-email) ADMIN_EMAIL="$2"; shift 2 ;;
    --admin-password) ADMIN_PASS="$2"; shift 2 ;;
    --behind-proxy) PROXY=1; shift ;;
    --network) NETWORK="$2"; shift 2 ;;
    --reconfigure) RECONFIG=1; shift ;;
    --version) VERSION="$2"; shift 2 ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ "$(uname -s)" = Linux ] || { echo "Linux is required" >&2; exit 1; }
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
NETWORK=${NETWORK:-bridge}
case "$NETWORK" in bridge|host) ;; *) echo "--network must be bridge or host" >&2; exit 2 ;; esac

if [ -n "$AGENT_HTTPS_PORT" ]; then
  case "$AGENT_HTTPS_PORT" in *[!0-9]*|0*|??????*) echo 'invalid Agent HTTPS port' >&2; exit 2 ;; esac
  [ "$AGENT_HTTPS_PORT" -ge 1024 ] && [ "$AGENT_HTTPS_PORT" -le 65535 ] && [ "$AGENT_HTTPS_PORT" != 8080 ] || { echo 'Agent HTTPS port must be 1024-65535, excluding 8080' >&2; exit 2; }
  case "$ACTION" in install|enable-agent-https) ;; *) echo '--agent-https-port applies to install or enable-agent-https only' >&2; exit 2 ;; esac
fi
if [ "$ACTION" = enable-agent-https ]; then
  [ -n "$AGENT_HTTPS_PORT" ] || { echo 'pass --agent-https-port (for example 2096)' >&2; exit 2; }
  command -v python3 >/dev/null || { echo 'python3 is required for safe Compose configuration; alternatively add the port mapping manually' >&2; exit 1; }
  command -v docker >/dev/null || { echo 'Docker is required; binary/host deployments need no port mapping' >&2; exit 1; }
  python3 - "$AGENT_HTTPS_PORT" /opt/captain <<'CAPTAIN_AGENT_HTTPS_PY'
# BEGIN AGENT HTTPS HELPER
import contextlib
import fcntl
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import sys
import tempfile

UPDATER_DIR = Path('/etc/captain-updater')
UPDATER_STATE = Path('/var/lib/captain-updater')
UPDATER_WORKER = Path('/usr/local/lib/captain-updater/worker')
OVERRIDE = 'compose.agent-https.yaml'
MARKER = '# Managed by Captain enable-agent-https; only the Agent TCP mapping.\n'


def trusted(path, optional=False):
    path = Path(path)
    if optional and not path.exists() and not path.is_symlink():
        path = path.parent
    while True:
        info = path.lstat()
        if info.st_uid != 0 or info.st_mode & 0o022 or stat.S_ISLNK(info.st_mode):
            raise ValueError('deployment files and parents must be root-owned, non-writable and not symlinks')
        if path.parent == path:
            return
        path = path.parent


def run(args):
    # Docker inspect/config and .env may contain secrets. Never echo output.
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=120)
    if result.returncode:
        raise ValueError('deployment command failed; inspect Docker locally (output may contain secrets)')
    return result.stdout


def atomic(path, data, mode=0o600):
    fd, temp = tempfile.mkstemp(prefix='.agent-https-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as out:
            os.fchmod(out.fileno(), mode)
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
        os.replace(temp, path)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)


def port_available(port):
    # The bind check also catches non-Docker listeners; Docker remains the final
    # authority if another process claims a port between this check and `up`.
    for family, addr in [(socket.AF_INET, ('0.0.0.0', port)), (socket.AF_INET6, ('::', port))]:
        try:
            with socket.socket(family, socket.SOCK_STREAM) as sock:
                if family == socket.AF_INET6:
                    sock.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
                sock.bind(addr)
        except OSError as exc:
            if family == socket.AF_INET6 and exc.errno in (97, 99):
                continue
            raise ValueError('Agent HTTPS TCP port is already occupied') from None


def main(port, directory):
    with contextlib.ExitStack() as cleanup:
        configure(port, directory, cleanup)


def configure(port, directory, cleanup):
    if not re.fullmatch(r'[1-9][0-9]{0,4}', port) or not 1024 <= int(port) <= 65535 or int(port) == 8080:
        raise ValueError('Agent HTTPS port must be 1024-65535, excluding the main HTTP port 8080')
    directory = Path(directory)
    trusted(directory)
    env_path, override = directory / '.env', directory / OVERRIDE
    trusted(env_path, optional=True)
    trusted(override, optional=True)
    if override.exists() and not override.read_text().startswith(MARKER):
        raise ValueError('existing Agent override is not installer-managed; configure its ports manually')
    if os.environ.get('COMPOSE_FILE') or os.environ.get('COMPOSE_PROJECT_NAME') or os.environ.get('COMPOSE_PATH_SEPARATOR') or os.environ.get('COMPOSE_ENV_FILES'):
        raise ValueError('unset COMPOSE_FILE/PROJECT_NAME/PATH_SEPARATOR/ENV_FILES before configuring the installed project')
    ids = run(['docker', 'ps', '--all', '--filter', 'label=com.docker.compose.service=captain', '--format', '{{.ID}}']).split()
    found = []
    for ident in ids:
        item = json.loads(run(['docker', 'inspect', ident]))[0]
        if item['Config']['Labels'].get('com.docker.compose.project.working_dir') == str(directory):
            found.append(item)
    if len(found) != 1 or not found[0].get('State', {}).get('Running'):
        raise ValueError('expected one running Captain Compose service in /opt/captain; use the manual Compose instructions for custom deployments')
    item = found[0]
    if item['HostConfig'].get('NetworkMode') == 'host':
        print('Host networking needs no Docker port mapping. Enable the Agent HTTPS listener and check host/cloud firewall rules.')
        return
    labels = item['Config']['Labels']
    project = labels['com.docker.compose.project']
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]*', project):
        raise ValueError('unsupported Compose project name')
    files = labels['com.docker.compose.project.config_files'].split(',')
    if len(set(files)) != len(files):
        raise ValueError('duplicate Compose inputs; configure ports manually')
    for filename in files:
        if not re.fullmatch(r'/[a-zA-Z0-9_./-]+', filename) or str(Path(filename)) != filename or '..' in Path(filename).parts:
            raise ValueError('unsupported Compose input path; configure ports manually')
        trusted(filename)
        if re.search(r'^\s*(include|extends)\s*:', Path(filename).read_text(), re.M):
            raise ValueError('Compose include/extends requires manual port configuration')
    had_env = env_path.exists()
    env = env_path.read_bytes() if had_env else b''
    # Accept simple COMPOSE_FILE declarations, never source/eval .env. Reject
    # ambiguous/custom interpolation instead of silently changing its meaning.
    env_lines = env.decode().splitlines()
    declarations = [line.strip() for line in env_lines if re.match(r'^\s*(?:export\s+)?COMPOSE_FILE\s*=', line)]
    if len(declarations) > 1:
        raise ValueError('multiple COMPOSE_FILE declarations; configure ports manually')
    if any(re.match(r'^\s*(?:export\s+)?COMPOSE_(?:PATH_SEPARATOR|ENV_FILES)\s*=', line) for line in env_lines):
        raise ValueError('custom Compose environment requires manual port configuration')
    if declarations:
        value = declarations[0].split('=', 1)[1].strip()
        if not re.fullmatch(r'[a-zA-Z0-9_./:-]+', value):
            raise ValueError('complex COMPOSE_FILE declaration; configure ports manually')
        declared = [str(directory / name) if not name.startswith('/') else name for name in value.split(':')]
        if declared != files:
            raise ValueError('Compose files changed since container creation; apply those changes before configuring the Agent port')
    updater = UPDATER_DIR / 'config.json'
    worker_lock = None
    if updater.exists():
        trusted(updater)
        trusted(UPDATER_WORKER)
        trusted(UPDATER_STATE)
        lock_path = UPDATER_STATE / 'worker.lock'
        trusted(lock_path, optional=True)
        worker_lock = os.open(lock_path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        cleanup.callback(os.close, worker_lock)
        try:
            fcntl.flock(worker_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('Captain updater is running. Wait for any upgrade to finish, stop captain-updater, then run this command again; successful setup restarts it.') from None
        job_path = UPDATER_STATE / 'job.json'
        trusted(job_path, optional=True)
        job = json.loads(job_path.read_text()) if job_path.exists() else {}
        if job.get('id') and job.get('phase') != 'complete':
            raise ValueError('an unfinished updater job requires recovery before changing Docker ports')
        registration = json.loads(updater.read_text())
        if registration.get('dir') != str(directory) or registration.get('project') != project:
            raise ValueError('updater registration does not match the running Compose project')
    compose = ['docker', 'compose', '--project-directory', str(directory), '--project-name', project]
    for filename in files:
        compose.extend(['--file', filename])
    current = json.loads(run(compose + ['config', '--format', 'json']))
    actual_hash = run(compose + ['config', '--hash', 'captain']).strip().split()
    if len(actual_hash) != 2 or actual_hash[0] != 'captain' or actual_hash[1] != labels.get('com.docker.compose.config-hash'):
        raise ValueError('Compose configuration changed since container creation; apply those changes before configuring the Agent port')
    service = current['services']['captain']
    image = json.loads(run(['docker', 'image', 'inspect', service['image']]))[0]['Id']
    if image != item['Image']:
        raise ValueError('configured local image differs from the running image; finish that upgrade before changing ports')
    # This operation must not apply unrelated edits to the active deployment.
    if service.get('network_mode') == 'host':
        raise ValueError('Compose networking changed since container creation; apply that change first')
    existing = item['HostConfig'].get('PortBindings') or {}
    already = any(binding.get('HostPort') == port and binding.get('HostIp') in ('', '0.0.0.0', '::') for binding in existing.get(port + '/tcp', []) or [])
    if not already:
        port_available(int(port))
    for name, config in current['services'].items():
        for mapping in config.get('ports', []):
            if str(mapping.get('published')) == port and mapping.get('protocol', 'tcp') == 'tcp' and (name != 'captain' or str(mapping.get('target')) != port):
                raise ValueError('requested port conflicts with an existing Compose mapping')
    old_override = override.read_bytes() if override.exists() else None
    files = [filename for filename in files if filename != str(override)]
    # Keep the updater image pin last, so future upgrades still win.
    position = next((i for i, name in enumerate(files) if Path(name).name == 'compose.updater.yaml'), len(files))
    files.insert(position, str(override))
    override_data = (MARKER + 'services:\n  captain:\n    ports:\n      - "' + port + ':' + port + '/tcp"\n').encode()
    new_env = '\n'.join(line for line in env_lines if not re.match(r'^\s*(?:export\s+)?COMPOSE_FILE\s*=', line))
    new_env = (new_env.rstrip('\n') + '\nCOMPOSE_FILE=' + ':'.join(files) + '\n').encode()
    updated = ['docker', 'compose', '--project-directory', str(directory), '--project-name', project]
    for filename in files:
        updated.extend(['--file', filename])
    try:
        atomic(override, override_data)
        atomic(env_path, new_env)
        run(updated + ['config', '--quiet'])
        run(updated + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', 'captain'])
        active_ids = run(updated + ['ps', '--quiet', 'captain']).split()
        if len(active_ids) != 1:
            raise ValueError('recreated Captain container is unavailable')
        active = json.loads(run(['docker', 'inspect', active_ids[0]]))[0]
        bindings = (active['HostConfig'].get('PortBindings') or {}).get(port + '/tcp', []) or []
        if not active.get('State', {}).get('Running') or active['Image'] != item['Image'] or not any(value.get('HostPort') == port and value.get('HostIp') in ('', '0.0.0.0', '::') for value in bindings):
            raise ValueError('recreated Captain image or TCP mapping did not match the requested deployment')
    except Exception:
        if old_override is None:
            override.unlink(missing_ok=True)
        else:
            atomic(override, old_override)
        if had_env:
            atomic(env_path, env)
        else:
            env_path.unlink(missing_ok=True)
        try:
            run(compose + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', 'captain'])
        except Exception:
            raise ValueError('port configuration failed; deployment files restored, but container recovery failed; inspect Docker locally') from None
        raise ValueError('port configuration failed; original deployment files and service restored') from None
    finally:
        cleanup.close()
    if updater.exists():
        # Re-discover the recreated container's complete file list. No upgrade,
        # image pull, account initialization or database operation is requested.
        try:
            run([str(UPDATER_WORKER), 'docker-updater', 'setup', '--dir', str(directory)])
        except Exception:
            raise ValueError('port mapping was applied, but updater registration failed; leave updater stopped and run enable-web-upgrade before future upgrades') from None
    print('Docker TCP mapping configured. Select the matching Agent HTTPS port and certificate in Captain. This does not prove public/node reachability; check host and cloud firewall rules.')


if __name__ == '__main__':
    try:
        main(sys.argv[1], sys.argv[2])
    except (ValueError, KeyError, IndexError, TypeError, AttributeError, OSError, subprocess.TimeoutExpired):
        # Expected errors carry fixed messages only, never Docker/config text.
        error = sys.exc_info()[1]
        print(str(error) if type(error) is ValueError else 'unsupported or unavailable deployment; configure the Compose TCP mapping manually', file=sys.stderr)
        sys.exit(1)
# END AGENT HTTPS HELPER
CAPTAIN_AGENT_HTTPS_PY
  exit 0
fi

# have_tty says whether the script can actually reach a terminal. /dev/tty
# exists and passes [ -r ] even under "ssh host 'curl … | sh'", where
# opening it fails with ENXIO ("cannot create /dev/tty"), so the only
# honest test is to open it — in a subshell, because a redirection that
# fails on a special built-in ends the whole shell under POSIX sh.
# Without a terminal, questions are skipped and anything still missing
# has to come from a flag.
have_tty() { ( exec 3>/dev/tty ) 2>/dev/null; }

# Lifecycle commands never re-run installation prompts or rewrite deployment settings.
resolve_version() {
  if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  fi
  printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || { echo 'invalid release version' >&2; exit 1; }
}
download_worker() {
  command -v curl >/dev/null || { echo 'curl is required to download the release' >&2; exit 1; }
  resolve_version
  case "$(uname -m)" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) echo 'unsupported architecture' >&2; exit 1 ;; esac
  BASE="https://github.com/$REPO/releases/download/$VERSION"
  curl -fsSL -o "$TMP/$PRODUCT" "$BASE/$PRODUCT-linux-$ARCH"
  curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
  WANT=$(awk -v file="$PRODUCT-linux-$ARCH" '$2 == file {print $1}' "$TMP/SHA256SUMS")
  GOT=$(sha256sum "$TMP/$PRODUCT" | cut -d' ' -f1)
  [ -n "$WANT" ] && [ "$WANT" = "$GOT" ] || { echo 'checksum mismatch' >&2; exit 1; }
  chmod 0755 "$TMP/$PRODUCT"
  WORKER="$TMP/$PRODUCT"
}
prepare_worker() {
  TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT HUP INT TERM
  candidate="/usr/local/lib/$PRODUCT-updater/worker"
  if [ -x "$candidate" ] && [ ! -L "$candidate" ] && [ "$(stat -c %u "$candidate")" = 0 ] && [ "$("$candidate" installer-version 2>/dev/null || true)" = 1 ]; then
    cp "$candidate" "$TMP/$PRODUCT"; chmod 0755 "$TMP/$PRODUCT"; WORKER="$TMP/$PRODUCT"; return
  fi
  download_worker
}
native_command() {
  if [ "$PRODUCT" = captain ]; then runuser -u captain -- "$NATIVE" "$@"
  else "$NATIVE" "$@"; fi
}
if [ "$ACTION" = uninstall ]; then
  echo "Remove $PRODUCT containers, service, binaries, configuration, database, backups and certificates. Shared Docker and unrelated applications are preserved."
  if [ "$YES" != 1 ]; then
    have_tty || { echo 'pass --yes to confirm uninstall without a terminal' >&2; exit 1; }
    printf 'Type yes to permanently uninstall: ' >/dev/tty; read -r ans </dev/tty
    [ "$ans" = yes ] || { echo 'aborted'; exit 1; }
  fi
  prepare_worker
  if [ "$KEEP_DATA" = 1 ]; then "$WORKER" uninstall --yes --keep-data; else "$WORKER" uninstall --yes; fi
  exit 0
fi
if [ "$ACTION" = upgrade ] || [ "$ACTION" = enable-web-upgrade ]; then
  if [ -z "$MODE" ]; then
    if [ -f "/opt/$PRODUCT/docker-compose.yml" ] || [ -f "/opt/$PRODUCT/docker-compose.yaml" ] || [ -f "/opt/$PRODUCT/compose.yml" ] || [ -f "/opt/$PRODUCT/compose.yaml" ]; then MODE=docker
    elif [ -x "$NATIVE" ]; then MODE=binary
    else echo 'no existing installation found; install first' >&2; exit 1; fi
  fi
  case "$MODE" in docker|binary) ;; *) echo '--mode must be docker or binary' >&2; exit 1 ;; esac
  prepare_worker
  if [ "$MODE" = docker ]; then
    # One-time registration is also available to old Docker installations.
    if [ "$ACTION" = enable-web-upgrade ] || ! "$WORKER" docker-updater status >/dev/null 2>&1; then
      "$WORKER" docker-updater setup --dir "/opt/$PRODUCT"
    fi
    if [ "$ACTION" = upgrade ]; then
      resolve_version
      "$WORKER" docker-updater upgrade --version "$VERSION"
      echo "Upgrade queued. Status: /usr/local/lib/$PRODUCT-updater/worker docker-updater status"
    fi
  else
    [ "$ACTION" = upgrade ] || { echo 'binary installations already support web updates' >&2; exit 1; }
    # Stage and verify the new release before stopping the existing service.
    download_worker
    "$WORKER" installer-check-layout "/etc/$PRODUCT/config.yaml"
    CURRENT=$(native_command version | awk '{print $2}')
    "$WORKER" installer-check-upgrade "$CURRENT" || { echo 'target must be a newer release' >&2; exit 1; }
    install -m 0755 "$WORKER" "$TMP/next"
    if [ "$PRODUCT" = captain ]; then chown captain:captain "$TMP/next"; fi
    mkdir -p "/var/lib/$PRODUCT-updater/backups"
    chmod 0700 "/var/lib/$PRODUCT-updater" "/var/lib/$PRODUCT-updater/backups"
    BACKUP="/var/lib/$PRODUCT-updater/backups/binary-$(date +%Y%m%d%H%M%S)"
    mkdir -m 0700 "$BACKUP"
    cp -p "$NATIVE" "$TMP/previous"
    printf '%s\n' "$CURRENT" > "$TMP/previous.version"
    if command -v systemctl >/dev/null; then systemctl stop "$PRODUCT"; INIT=systemd
    else rc-service "$PRODUCT" stop; INIT=openrc; fi
    resume_old() { if [ "$INIT" = systemd ]; then systemctl start "$PRODUCT"; else rc-service "$PRODUCT" start; fi; }
    if ! tar -czf "$BACKUP/data.tar.gz" -C / "etc/$PRODUCT" "var/lib/$PRODUCT"; then resume_old; echo 'backup failed; old service restarted' >&2; exit 1; fi
    if ! { mv -T "$TMP/previous" "$NATIVE.backup" && mv -T "$TMP/previous.version" "$NATIVE.backup.version" && mv -T "$TMP/next" "$NATIVE"; }; then
      resume_old; echo 'binary replacement failed; existing service restarted' >&2; exit 1
    fi
    resume_old
    READY=0
    for _attempt in 1 2 3 4 5 6 7 8 9 10; do
      if native_command healthcheck --version "$VERSION" >/dev/null 2>&1; then READY=1; break; fi
      sleep 2
    done
    [ "$READY" = 1 ] || { echo "New service did not become ready. Data backup: $BACKUP. No automatic database downgrade was attempted." >&2; exit 1; }
    echo "$PRODUCT $VERSION installed. Pre-upgrade data: $BACKUP. Service configuration was preserved."
  fi
  exit 0
fi

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
# Read from the terminal even when the script itself comes through a pipe.
ask() { # var prompt [default]
  eval "cur=\${$1:-}"
  if [ -n "$cur" ]; then return; fi
  if ! have_tty; then echo "$2 is required; pass --$3" >&2; exit 1; fi
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
  have_tty || { echo "$2 is required; pass --$3" >&2; exit 1; }
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
if [ -n "$AGENT_HTTPS_PORT" ]; then
  [ "$MODE" = docker ] || { echo '--agent-https-port is a Docker deployment option; enable the listener directly for binary installations' >&2; exit 2; }
  [ ! -f /opt/captain/docker-compose.yml ] && [ ! -f /opt/captain/docker-compose.yaml ] && [ ! -f /opt/captain/compose.yml ] && [ ! -f /opt/captain/compose.yaml ] || { echo 'existing Docker installation: use enable-agent-https --agent-https-port instead of reinstalling' >&2; exit 1; }
  command -v ss >/dev/null || { echo 'ss (iproute2) is required to check the requested TCP port' >&2; exit 1; }
  PORT_STATUS=$(ss -ltnH "sport = :$AGENT_HTTPS_PORT") || { echo "could not inspect listening TCP ports" >&2; exit 1; }
  [ -z "$PORT_STATUS" ] || { echo 'Agent HTTPS TCP port is already occupied' >&2; exit 1; }
  [ -z "$(docker ps --filter "publish=$AGENT_HTTPS_PORT" --format '{{.ID}}')" ] || { echo 'Agent HTTPS TCP port is already published by another container' >&2; exit 1; }
fi
OWNER=$(port_owner 80); [ -n "$OWNER" ] || OWNER=$(port_owner 443)
if [ "$PROXY" = 0 ]; then
  if [ -n "$OWNER" ]; then
    echo "Ports 80/443 are already used by: $OWNER"
    if have_tty; then
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
  if [ -z "$CF_TOKEN" ] && have_tty; then
    printf '%s' "Cloudflare API token for a wildcard certificate via DNS-01 (Enter to skip and use HTTP-01 on port 80): " >/dev/tty
    read_masked CF_TOKEN
  fi
fi
ask ADMIN_EMAIL "Admin login email" admin-email "$EMAIL"
ask_secret ADMIN_PASS "Admin password (8+ characters)" admin-password
[ ${#ADMIN_PASS} -ge 8 ] || { echo "password too short" >&2; exit 1; }

DIR=/opt/captain
mkdir -p "$DIR" /etc/captain
# A containerised proxy (1Panel's OpenResty, nginx-proxy...) on a bridge network
# cannot reach 127.0.0.1 of the host. With a bridge network Captain joins the
# proxy's network so it can use http://captain:8080; with the host network
# Captain binds the proxy network's gateway address instead, which is the one
# address on the host such a proxy can reach.
PNET=""; PGW=""
if [ "$MODE" = docker ] && [ "$PROXY" = 1 ]; then
  case "$OWNER" in docker:*)
    PC=${OWNER#docker:}
    if [ "$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$PC" 2>/dev/null)" != host ]; then
      PNET=$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$PC" 2>/dev/null | tr ' ' '\n' | grep -v '^$' | head -1)
      [ -n "$PNET" ] && PGW=$(docker network inspect -f '{{(index .IPAM.Config 0).Gateway}}' "$PNET" 2>/dev/null)
    fi ;;
  esac
fi
if [ "$PROXY" = 1 ]; then
  LISTEN="0.0.0.0:8080"; [ "$MODE" = binary ] && LISTEN="127.0.0.1:8080"
  if [ "$MODE" = docker ] && [ "$NETWORK" = host ]; then LISTEN="127.0.0.1:8080"; [ -n "$PGW" ] && LISTEN="$PGW:8080"; fi
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
  if [ "$MODE" = docker ] && ! grep -q "^listen: $LISTEN\$" "$CFG"; then echo "existing $CFG listens elsewhere than $LISTEN; rewriting it (backup: $CFG.bak)"; RECONFIG=1; fi
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
  # The image runs Captain as uid 1000; the file stays 0600 but must be readable by it.
  [ "$MODE" = docker ] && chown 1000 "$CFG"
  echo "wrote $CFG"
else
  echo "keeping existing $CFG"
fi

UPSTREAM="http://127.0.0.1:8080"
if [ "$MODE" = docker ]; then
  [ -z "$VERSION" ] || IMAGE="zeptop/captain:$VERSION"
  PORTS=""; NETS=""; NETDEF=""; V6=0
  if [ "$NETWORK" = host ]; then
    NETS="    network_mode: host"
    [ "$PROXY" = 1 ] && [ -n "$PGW" ] && UPSTREAM="http://$PGW:8080"
  else
    if [ "$PROXY" = 1 ]; then PORTS='    ports:
      - "127.0.0.1:8080:8080"'; else PORTS='    ports:
      - "80:80"
      - "443:443"'; fi
    if [ -n "$AGENT_HTTPS_PORT" ]; then PORTS="$PORTS
      - \"$AGENT_HTTPS_PORT:$AGENT_HTTPS_PORT/tcp\""; fi
    NETDEF="networks:"
    # Without IPv6 on the network, v6 clients reach a published port through
    # docker-proxy and Captain sees the bridge gateway instead of them: every
    # v6 visitor then shares one address in the rate limits, the allow-list
    # and the connection log. A ULA subnet is enough; Docker NATs it.
    if ip -6 addr show scope global 2>/dev/null | grep -q inet6; then
      V6=1
      NETDEF="$NETDEF
  default:
    enable_ipv6: true
    ipam:
      config:
        - subnet: fd0c:a97a:1::/64"
    fi
    if [ "$PROXY" = 1 ] && [ -n "$PNET" ]; then
      NETS="    networks: [default, $PNET]"
      NETDEF="$NETDEF
  $PNET:
    external: true"
      UPSTREAM="http://captain:8080"
    fi
    [ "$NETDEF" = "networks:" ] && NETDEF=""
  fi
  cat > "$DIR/docker-compose.yml" <<YAML
services:
  captain:
    image: $IMAGE
    container_name: captain
    restart: unless-stopped
$PORTS
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
  if [ "$WEB_UPGRADE" = 1 ]; then prepare_worker; "$WORKER" docker-updater setup --dir "$DIR"; fi
  sleep 4
  docker compose exec -T captain captain admin create -c /etc/captain/config.yaml -email "$ADMIN_EMAIL" -password "$ADMIN_PASS" || true
  echo
  echo "Captain is running (docker). Console: https://$DOMAIN/admin/   Users: https://$DOMAIN/portal/"
  [ "$PROXY" = 1 ] || echo "The certificate is requested at startup; watch it with: docker compose logs -f"
  [ -z "$AGENT_HTTPS_PORT" ] || echo "Agent HTTPS TCP port $AGENT_HTTPS_PORT prepared. Select this port and a certificate in Captain to enable the listener; check host/cloud firewall rules separately."
  echo "Manage: cd $DIR && docker compose logs -f | docker compose pull && docker compose up -d"
  if [ "$NETWORK" = host ]; then
    echo "Network: host (Captain binds $LISTEN on the host directly; no port mapping)."
  elif [ "$V6" = 1 ]; then
    DV=$(docker version --format '{{.Server.Version}}' 2>/dev/null | cut -d. -f1)
    echo "Network: bridge with IPv6, so v6 clients keep their own address."
    if [ -n "$DV" ] && [ "$DV" -lt 27 ] 2>/dev/null; then echo "  Docker $DV is older than 27: add {\"ip6tables\": true} to /etc/docker/daemon.json (experimental there) or v6 clients still arrive as the gateway."; fi
  else
    echo "Network: bridge (no global IPv6 on this host, so none on the network)."
  fi
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
  install -m 0755 "$TMP/captain" "$DIR/captain.new"; mv "$DIR/captain.new" "$DIR/captain"
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

