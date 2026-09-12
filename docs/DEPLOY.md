# Deploying Captain

One Linux box, one binary, one SQLite file. Caddy (or nginx) in front for TLS.

## 0. The installer (shortest path)

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh
```

Asks for domain, certificate email and admin login; installs with Docker if
present, otherwise as a systemd service. Everything below is what it does by
hand.

## 0b. Docker by hand

```sh
mkdir -p /opt/captain && cd /opt/captain
B=https://raw.githubusercontent.com/zeptop-dev/captain/master/deploy
curl -fsSLO $B/docker-compose.yml
curl -fsSL -o config.yaml $B/config.docker.yaml    # set base_url and tls.email
docker compose up -d
docker compose exec captain captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
```

The container publishes 80 and 443 and obtains its certificate on the first
request. Data (database, daily backups, certificates) lives in the
`captain-data` volume. Upgrade with `docker compose pull && docker compose up -d`.
With an existing reverse proxy use `docker-compose.proxy.yml` and set
`tls.auto: false`, `listen: 0.0.0.0:8080` in config.yaml.

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

## 3. HTTPS

`tls.auto: true` in config.yaml makes Captain listen on :443 with a Let's
Encrypt certificate for the `base_url` host (renewed automatically, cached in
`<data_dir>/certs`); :80 answers the ACME challenge and redirects. The systemd
unit carries `CAP_NET_BIND_SERVICE` for that. To terminate TLS elsewhere set
`tls.auto: false`, keep `listen: 127.0.0.1:8080` and use `deploy/Caddyfile` (or
an nginx equivalent). `base_url` must be the public URL either way: subscription
links, EPay and Stripe callbacks use it.

Routes: `/admin/` console, `/portal/` user site (root redirects there), `/sub/{token}`
subscriptions, `/api/agent/*` for bosun, `/api/payment/*` gateway callbacks.

## 3b. Node certificates

Settings → "Automatic certificates": enter the Let's Encrypt email (and a
Cloudflare API token for DNS challenges). Inbounds whose TLS settings carry
`"auto_cert": true` then get their certificate obtained and renewed on the node
itself; the node page lists each certificate with its expiry, and the node list
shows a red TLS badge when one failed or expires within two weeks.

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
