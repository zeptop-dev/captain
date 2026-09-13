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

- Subscriptions at `GET /sub/<token>` with client detection (`?client=` override): mihomo/Clash YAML, Stash YAML, sing-box JSON, Surge, Surfboard, Loon and Quantumult X node lists, base64 share links (v2rayN, Shadowrocket). Admin → Sub templates edits the document around the servers per format: YAML templates (clash, stash) get their `proxies` replaced and a `{{proxy_names}}` entry inside any proxy-group expands to every server name; text templates (surge, surfboard, loon, qx) replace `{{proxies}}` with the server lines and `{{proxy_names}}` with the comma-joined names. Loon and QX default to bare node lists because their remote subscriptions are node lists. Per-format protocol coverage follows each client's official reference: Loon (nsloon.app/docs/Node) gets ss, vmess, vless (+REALITY), trojan, hysteria2 and anytls over tcp/ws/http; Quantumult X (sample.conf) gets ss, vmess, vless (+REALITY) and trojan over tcp/ws; Surfboard (manual.getsurfboard.com) gets ss, vmess and trojan only; Stash (stash.wiki) uses its own keys (`sni`, hysteria2 `auth`/`up-speed`/`down-speed`, tuic `version`/`alpn`). Servers a client cannot express are left out of that client's document. `Subscription-Userinfo` header with usage and expiry. Entries decide what users see: display host and port on top of the landing inbound's settings; group-restricted inbounds only appear for that group. Rendered mihomo and sing-box documents validated with the real clients.

- Orders and payments: EPay 易支付 **v1 (MD5) and v2 (RSA)** behind one gateway (`payments.epay.version`), Stripe Checkout, 支付宝当面付 (Alipay F2F, scan-to-pay QR page), Coinbase Commerce, CoinPayments, BTCPay Server, MGate, and balance. Every callback is signature-verified before any field is read; settlement is idempotent under repeated callbacks and refused when the callback amount differs from the order.
- Portal API under `/api/portal`: register (optional), login, me (subscription, usage, subscription URL), plans, servers with per-server share links, orders, create order (returns the payment URL).

- Admin console (`web/admin`, React 19 + Mantine 8 + TanStack Query, zh-CN and en) embedded at `/admin/`: overview with traffic chart, nodes with a one-line install command (`curl .../api/agent/install.sh?pair=CODE | sh`, or a `docker run` with `BOSUN_CAPTAIN`/`BOSUN_PAIR`) that installs and pairs bosun, node detail with host metrics and inbounds (quick-setup recipes for VLESS+REALITY, Hysteria2, mieru, SS2022, Trojan+WS), entries, users with an edit drawer (grant plan, balance, rotate subscription URL), plans, orders, settings.

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

The script asks for the domain, an email for the certificate, an optional
Cloudflare API token (DNS-01: one wildcard certificate covers the domain, www
and subscription hosts; without it HTTP-01 on port 80 is used), and the admin
login, then installs with Docker when it is present (`docker compose` in
`/opt/captain`) or as a systemd service otherwise (`--mode binary` to force).
It does not install Docker for you: put it on first
(`curl -fsSL https://get.docker.com | sh`) if that is the way you want to run it.
Every answer can be given as a flag, see `install.sh --help`. Piped through `sh`
the script never lands on disk; `... | sh -s -- uninstall` takes the whole
installation away again (`--keep-data` keeps the database).

Already running nginx, OpenResty (1Panel), Caddy or anything else on 80/443?
The script notices, asks, and installs Captain behind it (`--behind-proxy` to
skip the question): Captain serves plain HTTP on 127.0.0.1:8080 (joining the
proxy container's Docker network when the proxy is containerised) and the
script prints the exact proxy snippet to paste; the proxy holds the
certificate. `--reconfigure` rewrites config.yaml when switching modes.

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

## Plans, coupons, invites

A plan has a base period and price plus any number of extra periods (quarter,
year …) with their own prices; buyers pick one at checkout. Renewing the same
plan before it expires extends the time and refills the quota; buying a
different plan replaces the current one. Quota reset: never, every N days from
purchase, on the 1st of each month, or on January 1st.

Coupons (Admin → Coupons) take a percentage or a fixed amount off, optionally
limited to plans, total uses, uses per user and a date range; the checkout
shows the discounted price as the code is typed. Invites: every user has a
referral link `/?ref=CODE`; a visitor who arrives through it is recorded as
invited when the account is created (by password or OIDC), and each paid order
credits a configurable share to the inviter's balance (Settings → Referral
rewards). Settings → Announcement puts a notice on the portal home page.

Nodes learn about changes within seconds: bosun keeps a long-poll request open
on the state endpoint, no persistent connection needed.

## Payment gateways

Enable any subset under `payments:` in config.yaml (see `config.example.yaml`);
each appears in the portal's "pay with" chooser. Callback URLs to register at
the provider are `<base_url>/api/payment/<name>/notify`.

| name | provider | notes |
|---|---|---|
| `epay` | 易支付 v1/v2 | page jump; callback signed MD5 or RSA |
| `stripe` | Stripe Checkout | webhook signed with the endpoint secret |
| `alipay` | 支付宝当面付 | `alipay.trade.precreate`; Captain serves a QR page at `/api/payment/alipay/page` (RSA2 both ways) |
| `coinbase` | Coinbase Commerce | hosted charge; webhook HMAC-SHA256 (`X-CC-Webhook-Signature`) |
| `coinpayments` | CoinPayments | `create_transaction` (API key pair); IPN HMAC-SHA512 with the IPN secret, merchant id checked |
| `btcpay` | BTCPay Server | Greenfield invoice; webhook HMAC-SHA256 (`BTCPay-Sig`), store id checked |
| `mgate` | MGate | Xboard-compatible: md5(sorted query + app_secret) both ways |

Crypto gateways take a `currency` for the fiat price (default CNY, USD for
CoinPayments); the buyer picks the coin on the provider's page.

## Registration limits

Settings → Registration limits (all optional, applied to password sign-up;
the email whitelist and invite-only rule also govern accounts auto-created by
external login):

- **Email domain whitelist** — only listed domains (and their subdomains) may register.
- **Sign-ups per IP** — at most N accounts per address within the window (default 24 h).
- **Invite only** — a valid invite code or `/?ref=` link is required.
- **Captcha** — Cloudflare Turnstile, Google reCAPTCHA v2 or hCaptcha. Enter the
  site key and secret; the widget appears on the sign-up form automatically and
  the token is verified server-side.

## Support, gift codes, knowledge base, Telegram

- **Tickets** — users open tickets in the portal (priority low/normal/high) and
  reply in a thread; Admin → Tickets answers them. A reply reaches the user on
  Telegram when linked, otherwise by email; new tickets can ping the admin chat.
- **Gift / redeem codes** — Admin → Gift codes generates single-use codes in
  batches: balance top-up, a plan for N days, extra traffic or extra days on the
  active plan, with optional expiry. Users redeem them on the portal home page.
- **Knowledge base and downloads** — Admin → Knowledge base holds Markdown
  guides grouped by category (`{{sub_url}}`, `{{email}}`, `{{site_name}}` are
  substituted per reader); Settings → Client downloads lists the apps. Both
  appear under Help in the portal.
- **Telegram bot** — Settings → Telegram: paste a BotFather token (and your chat
  id for order/ticket notices). Users link their chat from the portal with a
  one-time `/bind CODE`; the bot answers `/sub`, `/status`, `/unbind` and
  delivers expiry, traffic and ticket notices. Plain Bot API long polling, no
  webhook or public URL needed.
- **Plan change credit** — Settings → Plan change credit: switching to a
  different plan credits the unused remainder of the current one (by remaining
  time, or remaining traffic for plans without expiry) against the new order.
  Renewing the same plan still stacks time and refills quota.
- **Referral levels and payouts** — Settings → Invites: level-1 percentage,
  optional multi-level (levels 2 and 3), and where rewards go: straight to the
  balance, or a commission account the user can move to balance or withdraw
  (minimum amount, allowed methods). Admin → Withdrawals marks requests paid or
  rejected; rejecting refunds the commission.
- **Trial plan** — Settings → Trial plan hands every new account (password or
  external login) a plan once, for the configured number of days.

## Staff roles, theme, webhooks

- **Staff roles** — Admin → Staff creates console accounts with a role: admin
  (everything), operator (everything except settings, system, staff and the
  landing page) or support (tickets, plus read-only users, orders, plans and
  the dashboard). The last admin cannot be demoted, disabled or deleted.
- **Theme and page injection** — Admin → Landing page: primary colour, radius,
  light/dark/system scheme for the portal and the landing page, portal title,
  font, and raw HTML injected before `</head>` / `</body>` on every portal and
  landing page (analytics, chat widgets). The console keeps its own look.
- **Event webhooks** — Settings → Event webhooks: Captain POSTs JSON for
  `user.registered`, `order.paid`, `ticket.created`, `ticket.replied`,
  `withdrawal.requested` and `subscription.expiring` to your URLs with
  `X-Captain-Event` and an HMAC-SHA256 `X-Captain-Signature` over the body;
  failed deliveries retry three times. This is the integration point for
  n8n, scripts or a CRM in place of an in-process plugin system, which a
  single static binary cannot load.
- **Device limits on nodes** — plans' device limits reach the nodes; bosun
  counts client IPs per user on Xray, Hysteria and sing-box (log-based) and
  Captain locks out users over their limit. mieru inbounds cannot report IPs.
- **Languages** — the console and portal ship in 简体中文, 繁體中文, English,
  日本語, Русский and 한국어.

## Mail

Admin → Settings → Mail: SMTP (any provider; port 587 STARTTLS, 465 TLS or 25
plain) or the Resend HTTP API for hosts that block mail ports, plus a "send
test email" button. With mail configured you can require an emailed code to
register, users can reset their password themselves, and the hourly job sends
reminders three days before a subscription expires and when 90% of the quota is
used (each once). Templates are plain, mail-client-safe HTML in Chinese.

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
