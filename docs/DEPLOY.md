# Deploying Captain

One Linux box, one binary, one SQLite file. Caddy (or nginx) in front for TLS.

## 1. Binary

Tagged pipelines publish `captain-linux-{amd64,arm64}` plus `SHA256SUMS` to the
project's generic package registry. The project is private, so downloads need a
Deploy Token with `read_package_registry` (Settings → Repository → Deploy tokens):

```sh
V=v0.1.0; ARCH=amd64
B="https://gitlab.com/api/v4/projects/boyang-hu%2Fcaptain/packages/generic/captain/$V"
curl -fsSL -H "Deploy-Token: $TOKEN" -o /usr/local/bin/captain "$B/captain-linux-$ARCH"
curl -fsSL -H "Deploy-Token: $TOKEN" "$B/SHA256SUMS" | grep "captain-linux-$ARCH" | sed 's# .*# /usr/local/bin/captain#' | sha256sum -c
chmod 0755 /usr/local/bin/captain
```

Or build locally with `make build` (needs Go 1.26 and pnpm) and copy `bin/captain`.

## 2. Config and service

```sh
useradd --system --home /var/lib/captain --shell /usr/sbin/nologin captain
mkdir -p /etc/captain /var/lib/captain && chown captain:captain /var/lib/captain
cp config.example.yaml /etc/captain/config.yaml   # set base_url, payments; keep listen on 127.0.0.1
chmod 0600 /etc/captain/config.yaml && chown captain /etc/captain/config.yaml
cp deploy/captain.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now captain
```

Migrations run automatically on start. Create the first admin:

```sh
sudo -u captain captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
```

## 3. Reverse proxy

`deploy/Caddyfile` is the whole thing: Caddy fetches certificates itself. Point
`base_url` at the public URL: subscription links, EPay and Stripe callbacks use it.

Routes: `/admin/` console, `/portal/` user site (root redirects there), `/sub/{token}`
subscriptions, `/api/agent/*` for bosun, `/api/payment/*` gateway callbacks.

## 4. Nodes

Admin → Nodes → Add node gives a pair code. On the node:

```sh
curl -fsSL https://gitlab.com/boyang-hu/bosun/-/raw/master/scripts/install.sh | sh -s -- \
  --captain https://panel.example.com --pair ABCD-EFGH
```

Then add inbounds and entries for that node; bosun pulls the state within a minute
(immediately after a report says it changed).

## 5. Backups

Everything is in `/var/lib/captain/captain.db` plus the config file. `sqlite3
captain.db ".backup /path/captain-$(date +%F).db"` is a consistent copy.
