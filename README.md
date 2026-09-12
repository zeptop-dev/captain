# Captain

Unified management panel for [bosun](https://github.com/zeptop-dev/bosun)
nodes: users, plans, orders and payments, subscription output, node fleet,
forwarding policy and configuration push. One Go binary with the admin console
and user portal embedded. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Status

Backend skeleton, verified by an end-to-end API test and a live run with a
real bosun (Captain driver) serving mieru to the official client, with the
traffic landing in the user's subscription:

- SQLite database with embedded migrations (users, sessions, plans,
  subscriptions, orders, nodes, inbounds, entries, chains, traffic, online
  devices, forward status, settings).
- Admin API: login with argon2id + session cookie, create nodes (one-time
  pairing code), inbounds, user groups, users, plans, grant a plan, user detail.
- Per-inbound access: an inbound bound to a user group only provisions that
  group's users (`spec.Inbound.ScopedUsers`); ungrouped inbounds get every
  user with a usable subscription.
- Agent API implementing `bosun/pkg/agentproto`: pair, state with ETag,
  report (traffic charged to the active subscription, online devices,
  forward status, node liveness). A user whose subscription expires or runs
  out of quota disappears from the node's desired state.

- Subscriptions at `GET /sub/<token>` with client detection (`?client=` override): mihomo/Clash YAML, sing-box JSON, base64 share links (v2rayN, Shadowrocket), Surge. `Subscription-Userinfo` header with usage and expiry. Entries decide what users see: display host and port on top of the landing inbound's settings; group-restricted inbounds only appear for that group. Rendered mihomo and sing-box documents validated with the real clients.

- Orders and payments: EPay 易支付 **v1 (MD5) and v2 (RSA)** behind one gateway (`payments.epay.version`), Stripe Checkout with webhook verification, and balance. Settlement is idempotent under repeated callbacks; EPay callbacks are also checked against the order amount.
- Portal API under `/api/portal`: register (optional), login, me (subscription, usage, subscription URL), plans, servers with per-server share links, orders, create order (returns the payment URL).

- Admin console (`web/admin`, React 19 + Mantine 8 + TanStack Query, zh-CN and en) embedded at `/admin/`: overview with traffic chart, nodes with pairing codes and a bosun config snippet, node detail with host metrics and inbounds (quick-setup recipes for VLESS+REALITY, Hysteria2, mieru, SS2022, Trojan+WS), entries, users with an edit drawer (grant plan, balance, rotate subscription URL), plans, orders, settings.

- User portal (`web/portal`, light theme, phone friendly) embedded at `/portal/` (the site root redirects there): sign up / sign in, home with usage, expiry, balance, subscription link with copy, QR code and one-tap import links (Clash, sing-box, Shadowrocket, Surge), plans with balance / EPay / Stripe checkout, orders, servers with per-server share links.

Not yet: jobs (stale order cancellation, quota resets), online device
collection, email (password reset, notifications).

## Install

One Linux server, a domain whose A record points at it, ports 80 and 443 free.
Captain terminates HTTPS itself with a Let's Encrypt certificate; no reverse
proxy needed.

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh
```

The script asks for the domain, an email for the certificate, and the admin
login, then installs with Docker when it is present (`docker compose` in
`/opt/captain`) or as a systemd service otherwise (`--mode binary` to force).
It does not install Docker for you: put it on first
(`curl -fsSL https://get.docker.com | sh`) if that is the way you want to run it.
Every answer can be given as a flag, see `install.sh --help`. Piped through `sh`
the script never lands on disk; `... | sh -s -- uninstall` takes the whole
installation away again (`--keep-data` keeps the database).

Already running Caddy or nginx on that host? Add `--behind-proxy`: Captain then
serves plain HTTP on 127.0.0.1:8080 and `deploy/Caddyfile` shows the proxy
block.

Day-to-day (Docker):

```sh
cd /opt/captain
docker compose logs -f                        # logs
docker compose pull && docker compose up -d   # upgrade (the console shows a red dot when a release is out)
docker run --rm -v captain_captain-data:/d -v $PWD:/out alpine sh -c 'cp /d/backups/*.db /out/'   # copy the daily snapshots to the host
```

Captain snapshots its database every day into `backups/` inside the data
directory and keeps the last seven, so a restore is a copy of one file. Logins
lock an address for 15 minutes after five failures; session cookies are
HTTPS-only whenever `base_url` is https. Certificates live in `certs/` in the
same directory and renew themselves.

Manual layouts (`deploy/docker-compose.yml`, `deploy/docker-compose.proxy.yml`,
`deploy/captain.service`) are what the script writes; use them directly if you
prefer.

## Landing page

`/` is a public landing page: hero with a rotating globe showing your node
locations and arcs from a hub, feature cards, the plan list and an FAQ, all
edited under Admin → Landing page (no rebuild, saves apply at once). Want your
own design? Drop an `index.html` (plus assets) into `<data_dir>/site/` and
Captain serves that directory instead.

## External login (OIDC)

Admin → Settings → "External login" takes any OpenID Connect provider
(Casdoor, Authentik, Keycloak, Zitadel, Google …): id, display name, issuer
URL, client id and secret. Register `https://<your domain>/api/oauth/<id>/callback`
as the redirect URI at the provider. The portal then shows "Continue with …";
accounts are matched by the provider's subject, linked to an existing account
with the same verified email, or created when registration is open (or
`auto_register` is set for that provider). Users can link and unlink logins
from the portal home page, and password login can be switched off entirely.

Casdoor example: issuer `https://door.example.com`, scopes default
(`openid profile email`), `trust_email: true` since you run it yourself.

## Run

```sh
make build            # builds web/admin with pnpm, then the Go binary with it embedded
cp config.example.yaml /etc/captain/config.yaml       # set base_url
bin/captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
bin/captain serve -c /etc/captain/config.yaml
```

## License

MIT, see `LICENSE`.
