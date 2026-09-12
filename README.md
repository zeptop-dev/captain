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

## Docker Compose (recommended install)

One Linux server with Docker (Ubuntu/Debian: `curl -fsSL https://get.docker.com | sh`),
a domain whose A record points at it, and ports 80/443 free.

```sh
mkdir -p /opt/captain && cd /opt/captain
B=https://raw.githubusercontent.com/zeptop-dev/captain/master/deploy
curl -fsSLO $B/docker-compose.yml
curl -fsSLO $B/Caddyfile
curl -fsSL -o config.yaml $B/config.docker.yaml
```

Edit two files:

- `Caddyfile`: replace `panel.example.com` with your domain (leave `captain:8080`).
- `config.yaml`: set `base_url` to `https://<your domain>` (subscription links and
  payment callbacks use it); add `payments` when you have gateway credentials.

Then:

```sh
docker compose up -d
docker compose exec captain captain admin create -c /etc/captain/config.yaml -email you@example.com -password 'choose-a-long-one'
```

Open `https://<your domain>/admin/` and sign in. Caddy fetches the certificate
on the first request, which can take a few seconds.

Day-to-day:

```sh
docker compose logs -f captain            # logs
docker compose pull && docker compose up -d   # upgrade (the console shows a red dot when a release is out)
docker run --rm -v captain_captain-data:/d -v $PWD:/out alpine sh -c 'cp /d/backups/*.db /out/'   # copy the daily snapshots to the host
```

Captain snapshots its database every day into `backups/` inside the data volume
and keeps the last seven, so a restore is a copy of one file. Logins lock an
address for 15 minutes after five failures; session cookies are HTTPS-only
whenever `base_url` is https.

No domain yet? `deploy/docker-compose.plain.yml` runs Captain alone on
`http://<server>:8080` with `base_url: http://<server>:8080`; switch to the
Caddy variant later and keep the same `captain-data` volume.

## Run

```sh
make build            # builds web/admin with pnpm, then the Go binary with it embedded
cp config.example.yaml /etc/captain/config.yaml       # set base_url
bin/captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
bin/captain serve -c /etc/captain/config.yaml
```

## License

MIT, see `LICENSE`.
