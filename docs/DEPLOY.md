# Deploying Captain

One Linux box, one binary, one SQLite file. Caddy (or nginx) in front for TLS.

## 0. The installer (shortest path)

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh
```

Asks for the domain, an email for the certificate, an optional Cloudflare API
token (DNS-01: one wildcard certificate covers the domain, `www` and the
subscription hosts; without it HTTP-01 on port 80 is used) and the admin
login, then installs with Docker when it is present (`docker compose` in
`/opt/captain`) or as a systemd service otherwise (`--mode binary` forces
it). It does not install Docker for you — put it on first
(`curl -fsSL https://get.docker.com | sh`) if that is how you want to run it.
Every answer can be a flag; see `install.sh --help`. Piped through `sh` the
script never lands on disk. Everything below is what it does by hand.

**Already running nginx, OpenResty (1Panel), Caddy or anything else on
80/443?** The script notices, asks, and installs Captain behind it
(`--behind-proxy` skips the question): Captain serves plain HTTP on
127.0.0.1:8080 — joining the proxy container's Docker network when the proxy
is containerised — and the script prints the exact proxy snippet to paste;
the proxy holds the certificate. `--reconfigure` rewrites config.yaml when
switching modes.

**Docker network (`--network bridge|host`).** The default is Compose's own
bridge network (`captain_default`): only 80/443 — or 127.0.0.1:8080 behind
a proxy — are published, and Captain can join a containerised proxy's
network so the proxy reaches it as `http://captain:8080`, which a host-mode
container cannot offer. When the host has a global IPv6 address the script
enables IPv6 on that network (a ULA subnet Docker NATs): without it, v6
clients reach a published port through docker-proxy and Captain sees the
bridge gateway instead of them, so every v6 visitor shares one address in
the login rate limit, the admin allow-list and the connection log. Docker
older than 27 also needs `{"ip6tables": true}` in `/etc/docker/daemon.json`
for that to take effect. `--network host` skips all of it: Captain binds
the host's interfaces directly (`0.0.0.0:443`, or behind a proxy
`127.0.0.1:8080` — the proxy network's gateway address when the proxy is a
bridge container, since that is the one host address it can reach). No
NAT, no port mapping, but nothing else may hold those ports.

**Removing it again:**

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh -s -- uninstall
#   ... | sh -s -- uninstall --keep-data      keeps the database
```

**Day to day, with Docker:**

```sh
cd /opt/captain
docker compose logs -f                        # logs
docker compose pull && docker compose up -d   # upgrade (the console shows a red dot when a release is out)
docker run --rm -v captain_captain-data:/d -v $PWD:/out alpine sh -c 'cp /d/backups/*.db /out/'   # copy the daily snapshots out
```

The manual layouts the script writes are `deploy/docker-compose.yml`,
`deploy/docker-compose.proxy.yml` and `deploy/captain.service`; use them
directly if you prefer.

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
unit carries `CAP_NET_BIND_SERVICE` for that. Set `tls.cloudflare_token` (a
token with Zone:DNS:Edit on the zone) to switch to DNS-01: Captain then obtains
`example.com` **and** `*.example.com` up front (the registrable domain of the
panel host), so www, the bare domain and every subscription host under it are
covered by one certificate, port 80 is no longer required and the Cloudflare
proxy can stay on (SSL mode Full (strict)). Hosts outside that zone still get
their own certificate on first request. With a `www.example.com` panel the
bare domain redirects to www. To terminate TLS elsewhere set
`tls.auto: false`, keep `listen: 127.0.0.1:8080` and use `deploy/Caddyfile` (or
an nginx equivalent). `base_url` must be the public URL either way: subscription
links, EPay and Stripe callbacks use it.

Routes: `/admin/` console, `/portal/` user site (root redirects there), `/sub/{token}`
subscriptions, `/api/agent/*` for bosun, `/api/payment/*` gateway callbacks.

## 3a. Subscription domain

Settings → "Subscription URLs" lists the addresses put into users' subscription
links (one per line, picked at random; blank means `base_url`). `[1-9]` and
`[uuid]` placeholders combine with a wildcard DNS record so every user gets a
different hostname. Requests arriving on those hosts reach only `/sub/…`;
the login pages answer 404 there, so the address users pass around exposes
nothing else. With `tls.auto` Captain obtains certificates for these hosts on
first use as well (point their DNS at the same server; Let's Encrypt limits
about 50 certificates per registered domain per week, so keep placeholder
ranges small).

## 3b. Node certificates

Settings → "Automatic certificates": enter the Let's Encrypt email (and a
Cloudflare API token for DNS challenges). Inbounds whose TLS settings carry
`"auto_cert": true` then get their certificate obtained and renewed on the node
itself; the node page lists each certificate with its expiry, and the node list
shows a red TLS badge when one failed or expires within two weeks.

## 3c. Domain layout

One domain is enough: `/` landing page, `/portal/` user site, `/admin/`
console, `/sub/…` subscriptions. Put subscriptions on their own host with
Settings → Subscription URLs when you want the address users share to serve
nothing else. `base_url` is the public URL of the site; payment callbacks and
OIDC redirect URIs derive from it.

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
