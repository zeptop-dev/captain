# Deploying Captain

One Linux box, one binary, one SQLite file. Caddy (or nginx) in front for TLS.

## 0. Docker (shortest path)

Tags push a multi-arch image to `ghcr.io/zeptop-dev/captain` and Docker Hub
`zeptop/captain`; both are public, no login needed.

```sh
mkdir -p /opt/captain && cd /opt/captain
curl -fsSLO https://raw.githubusercontent.com/zeptop-dev/captain/master/deploy/docker-compose.yml
curl -fsSLO https://raw.githubusercontent.com/zeptop-dev/captain/master/deploy/Caddyfile
curl -fsSL -o config.yaml https://raw.githubusercontent.com/zeptop-dev/captain/master/config.example.yaml
# edit: Caddyfile domain; config.yaml listen: 0.0.0.0:8080, base_url, payments
docker compose up -d
docker compose exec captain captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
```

Data lives in the `captain-data` volume (`/var/lib/captain` inside). Upgrade with
`docker compose pull && docker compose up -d`.

## 1. Binary

Every `v*` tag attaches `captain-linux-{amd64,arm64}` plus `SHA256SUMS` to the
GitHub Release:

```sh
V=$(curl -fsSL https://api.github.com/repos/zeptop-dev/captain/releases/latest | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p'); ARCH=amd64
B="https://github.com/zeptop-dev/captain/releases/download/$V"
mkdir -p /opt/captain
curl -fsSL -o /opt/captain/captain "$B/captain-linux-$ARCH"
curl -fsSL "$B/SHA256SUMS" | grep "captain-linux-$ARCH" | sed 's# .*# /opt/captain/captain#' | sha256sum -c
chmod 0755 /opt/captain/captain
```

The binary lives in `/opt/captain`, owned by the service user, so the in-app
updater can replace it.

Or build locally with `make build` (needs Go 1.26 and pnpm) and copy `bin/captain`.

## 2. Config and service

```sh
useradd --system --home /var/lib/captain --shell /usr/sbin/nologin captain
mkdir -p /etc/captain /var/lib/captain && chown -R captain:captain /var/lib/captain /opt/captain
cp config.example.yaml /etc/captain/config.yaml   # set base_url, payments; keep listen on 127.0.0.1
chmod 0600 /etc/captain/config.yaml && chown captain /etc/captain/config.yaml
cp deploy/captain.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now captain
```

Migrations run automatically on start. Create the first admin:

```sh
sudo -u captain /opt/captain/captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
```

## Updating

Settings → "Version and updates" (and a red dot on the version badge) shows
when a newer release exists. "Update and restart" downloads the binary for
this platform, verifies it against `SHA256SUMS`, swaps `/opt/captain/captain`
(keeping the old one as `captain.backup` for "Roll back") and exits; systemd
restarts it. Docker installs show the `docker compose pull` command instead.

Nodes → an orange "vX available" badge marks bosun nodes older than the latest
bosun release; click it, or "Upgrade all", and each node installs that release
on its next report and restarts itself.

## 3. Reverse proxy

`deploy/Caddyfile` is the whole thing: Caddy fetches certificates itself. Point
`base_url` at the public URL: subscription links, EPay and Stripe callbacks use it.

Routes: `/admin/` console, `/portal/` user site (root redirects there), `/sub/{token}`
subscriptions, `/api/agent/*` for bosun, `/api/payment/*` gateway callbacks.

## 4. Nodes

Admin → Nodes → Add node gives a pair code. On the node:

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/bosun/master/scripts/install.sh | sh -s -- \
  --captain https://panel.example.com --pair ABCD-EFGH
```

Then add inbounds and entries for that node; bosun pulls the state within a minute
(immediately after a report says it changed).

## 5. Backups

Everything is in `/var/lib/captain/captain.db` plus the config file. Captain
writes a consistent snapshot to `/var/lib/captain/backups/captain-YYYY-MM-DD.db`
once a day and keeps seven; copy that directory off the machine. To restore,
stop Captain, replace `captain.db` with a snapshot, start it again.
