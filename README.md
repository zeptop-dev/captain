# Captain

Unified management panel for [bosun](https://github.com/zeptop-dev/bosun)
nodes: users, plans, orders and payments, subscription output, node fleet,
forwarding policy and configuration push. One Go binary with the admin console
and user portal embedded. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md);
running it in production (backup, restore, upgrade, monitoring):
[docs/OPERATIONS.md](docs/OPERATIONS.md); changes per release: [CHANGELOG.md](CHANGELOG.md).
Publishing the panel through Cloudflare Tunnel (no public IP, no open ports): [docs/CLOUDFLARE_TUNNEL.md](docs/CLOUDFLARE_TUNNEL.md).

## Status

One binary, verified by end-to-end API tests (`internal/http/e2e_test.go`)
and, before every release, by `make e2e` against a real panel with real nodes
and a headless mihomo (`scripts/e2e/README.md`)
and live runs with real bosun nodes (Captain driver) serving the official
clients, with the traffic landing in the user's subscription:

- SQLite database with embedded goose migrations (`migrations/`): users,
  sessions, plans, subscriptions, orders, nodes, inbounds, entries, traffic,
  online devices, forward status, settings, and what later releases added
  (coupons, commissions, tickets, gift codes, articles, external nodes, probe
  stats, sub links, API tokens, domains, ingresses, certificates, node jobs).
  One Captain process per database file (see `docs/OPERATIONS.md`).
- Admin API: login with argon2id + session cookie, create nodes (one-time
  pairing code), inbounds, user groups, users, plans, grant a plan, user detail.
- Per-inbound access: an inbound bound to a user group only provisions that
  group's users (`spec.Inbound.ScopedUsers`); ungrouped inbounds get every
  user with a usable subscription.
- Agent API implementing `bosun/pkg/agentproto`: pair, state with ETag,
  report (traffic per user *and inbound*, charged to the subscription whose
  plan group matches that inbound — an ungrouped inbound charges the
  soonest-expiring plan — online devices,
  forward status, node liveness). A user whose subscription expires or runs
  out of quota disappears from the node's desired state.

- Subscriptions at `GET /sub/<token>` with client detection (`?client=` override): mihomo/Clash YAML, Stash YAML, sing-box JSON, Egern YAML, Surge, Surfboard, Loon and Quantumult X node lists, base64 share links (v2rayN, Shadowrocket), WireGuard `.conf`. The renderers are bosun's `pkg/subscription`, shared with the standalone panel. Admin → Sub templates edits the document around the servers per format: YAML templates (clash, stash) get their `proxies` replaced and a `{{proxy_names}}` entry inside any proxy-group expands to every server name; text templates (surge, surfboard, loon, qx; egern is YAML with `{{proxy_names}}` in its policy groups) replace `{{proxies}}` with the server lines and `{{proxy_names}}` with the comma-joined names. Loon and QX default to bare node lists because their remote subscriptions are node lists. Per-format protocol coverage follows each client's official reference: Loon (nsloon.app/docs/Node) gets ss, vmess, vless (+REALITY), trojan, hysteria2 and anytls over tcp/ws/http; Quantumult X (sample.conf) gets ss, vmess, vless (+REALITY) and trojan over tcp/ws; Surfboard (manual.getsurfboard.com) gets ss, vmess and trojan only; Stash (stash.wiki) uses its own keys (`sni`, hysteria2 `auth`/`up-speed`/`down-speed`, tuic `version`/`alpn`). Servers a client cannot express are left out of that client's document. `Subscription-Userinfo` header with usage and expiry. Entries decide what users see: display host and port on top of the landing inbound's settings; group-restricted inbounds only appear for that group. Rendered mihomo and sing-box documents validated with the real clients. The same page has a visual designer (bosun's `pkg/subdesign`): proxy groups whose members are all servers, servers with an entry tag (`{{proxy_names:tag=hk}}`), servers whose name matches a region pattern (`{{proxy_names:match=HK|香港}}`) or other groups, plus an ordered rule list drawn from the ACL4SSR catalogue with presets (basic, ACL4SSR standard, standard + region groups); "Generate & apply" writes every format's template at once. Each entry can carry client extra fields (a JSON object merged into its mihomo/Stash/sing-box proxy: tfo, smux, dialer-proxy, ip-version…). External nodes are TCP-probed from the panel every 10 minutes; a source with "hide unreachable nodes" drops the ones whose last probe failed from subscriptions. A node option "mita native quotas" also writes each user's allowance into mita's own quotas (window = the plan's reset cycle) so the core keeps enforcing it when the panel is unreachable.

- Orders and payments: EPay 易支付 **v1 (MD5) and v2 (RSA)** behind one gateway (`payments.epay.version`), Stripe Checkout, 支付宝当面付 (Alipay F2F, scan-to-pay QR page), Coinbase Commerce, CoinPayments, BTCPay Server, MGate, and balance. Every callback is signature-verified before any field is read; settlement is idempotent under repeated callbacks and refused when the callback amount differs from the order.
- Portal API under `/api/portal`: register (optional), login, me (subscription, usage, subscription URL), plans, servers with per-server share links, orders, create order (returns the payment URL).

- Admin console (`web/admin`, React 19 + Mantine 8 + TanStack Query, zh-CN and en) embedded at `/admin/`: overview with traffic chart, nodes with a one-line install command (`curl .../api/agent/install.sh?pair=CODE | sh`, or a `docker run` with `BOSUN_CAPTAIN`/`BOSUN_PAIR`) that installs and pairs bosun, node detail with host metrics and inbounds (quick-setup recipes for VLESS+REALITY, Hysteria2, mieru, SS2022, Trojan+WS), entries, users with an edit drawer (grant plan, balance, rotate subscription URL), plans, orders, settings.

- User portal (`web/portal`, light theme, phone friendly) embedded at `/portal/` (the site root redirects there): sign up / sign in, home with usage, expiry, balance, subscription link with copy, QR code and one-tap import links (Clash, sing-box, Shadowrocket, Surge), plans paid with balance or any enabled gateway (see Payment gateways), orders, servers with per-server share links.

Housekeeping runs in-process (`internal/jobs`, one tick a minute: stale
order cancellation, subscription expiry, quota resets, queued-plan starts,
session and online-device purges, hourly reminders and external node sync,
daily backups, certificate renewal); nodes report online client IPs
(`Report.Online`) for device limits; mail (`internal/mail`) covers
registration codes, password reset and reminders. The sections below
describe each.

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
lock an address for 15 minutes after five failures; `trusted_proxies` in
config.yaml names the reverse proxies whose forwarding headers decide that
address (empty: any loopback or private peer); session cookies are
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
plan before it expires extends the time and keeps what was used: a plan with a
reset cycle keeps its counter and next reset, a plan without one gets the new
period's allowance added. Buying a different plan stacks next to the current
one (or replaces it in single-plan mode). Quota reset: never, every N days from
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
  Renewing the same plan stacks time and keeps the usage counter (see above).
- **Referral levels and payouts** — Settings → Invites: level-1 percentage,
  optional multi-level (levels 2 and 3), and where rewards go: straight to the
  balance, or a commission account the user can move to balance or withdraw
  (minimum amount, allowed methods). Admin → Withdrawals marks requests paid or
  rejected; rejecting refunds the commission.
- **Subscription adjustments** — in a user's drawer: extend by N days
  (+30/+60/+90), override the quota (kept across renewals of the same plan),
  set a personal monthly reset day, reset usage. Users → Renewals lists
  everyone by expiry with one-click extensions for batch renewals.
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
- **Two-factor sign-in** — Settings → Two-factor authentication: each staff
  account can add a TOTP authenticator; the console then asks for the code
  after the password. API tokens are unaffected.
- **Entry tags, regions and drag ordering** — Admin → Entries: drag the
  handle to set the order clients see, add free-form tags (shown to users in
  the portal and usable as a filter) and pick a region; Settings →
  Subscription → *Auto flags* prefixes names that lack a flag emoji with
  their region flag (explicit region, else detected from the name: "Tokyo",
  "HK-02", "香港", "jp1.example.com"...).
- **Short and temporary subscription links** — Settings → Subscription: turn
  on *Use short links* and users get `https://sub.example.com/s/<8 chars>`
  instead of the long token URL (old links keep working; rotating a user's
  token also rotates the code). A user's drawer can also issue temporary
  links limited by uses and/or hours, for trials or support, revocable at
  any time.

## External nodes, outbounds and relays

- **External nodes** (Admin → External nodes): paste share links (vless,
  vmess, trojan, ss, hysteria2, tuic, anytls) or add an airport subscription
  (base64 / URI-list form, re-synced hourly, per-source User-Agent). They are
  offered in your subscriptions like your own entries, optionally restricted to
  a user group. Nothing runs behind them, so their traffic is not charged.
- **Outbounds & landing** (node page): add an exit from a share link (bosun
  renders it in the serving core's dialect: sing-box takes every protocol,
  Xray vless/vmess/trojan/ss/socks/http), chain exits (`via`), then pick a
  default exit for the whole node or add rules such as `inbound:tag`,
  `domain:`, `ip:`, `protocol:`, `port:` → outbound / direct / block. Needs
  bosun >= 0.12.

## Domains and certificates

Admin → Domains & certs registers the domains you own (each with
Cloudflare as the DNS provider, using the global token from Settings → ACME
or its own token when the zone lives in another account, or "manual"), shows
what uses each one (node host names, inbound TLS names, subscription hosts,
the panel itself) and manages certificates:

- **Issue** — Let's Encrypt through DNS-01 on the panel, one certificate for
  any set of names (`example.com` + `*.example.com` together is fine), stored
  in the database and renewed by the panel 30 days before expiry; the first
  failed renewal notifies the admin. The Cloudflare token stays on the panel;
  nodes need neither a token nor port 80.
- **Upload** a PEM pair, or let a certificate manager (Certimate, an acme.sh
  deploy hook) POST renewals to the per-panel webhook shown on the page
  (lenient JSON keys: `domain`/`domains`, `certificate`, `privateKey`/`private_key`).
- **Deploy by coverage** — every node whose standard-TLS inbounds use a
  covered name (exact or wildcard) receives the pair in its state; bosun
  (>= 0.13) uses it ahead of ACME and lists it as method `custom`. Deleting
  a certificate lets the nodes fall back to node-side ACME.

Nodes take an optional host name (Node → Domain, e.g. `jp1.example.com`):
new inbound recipes use it as the TLS name and entries advertise it instead
of the IP, so a certificate for it reaches the node without further setup.

**Automatic DNS records.** A registered Cloudflare domain with *Auto DNS
records* on (the default) gets A/AAAA records created or updated whenever a
node with a host name under it is saved (node domain → public / IPv6
address) or a line ingress with an *entry domain* is saved (entry domain →
the provider's entry IP). Records are never deleted, never proxied, and the
outcome is shown in a toast; the token needs DNS edit permission on the
zone, which the DNS-01 token already has.

## Komari reporting

Settings → Komari reporting attaches every managed node to a Komari
monitor as an agent: give the Komari URL and its auto-discovery key, and
each node (bosun >= 0.17) registers under its node name, reports metrics
every few seconds and answers Komari's ping tasks. Only the ping capability
is offered. This runs alongside Captain's own probe; turn the probe page off
if you prefer Komari's.

## Backups

Captain snapshots its SQLite database once a day (`VACUUM INTO`, so the
copy is consistent while the panel keeps running) into `<data_dir>/backups`
and keeps the newest seven. Settings → Database backups sets the hour and
retention, adds a remote (WebDAV with basic auth, or any S3-compatible
bucket: AWS, Cloudflare R2, Backblaze B2, MinIO with path-style) that
receives each gzipped snapshot with its own retention, tests the remote,
runs a backup on demand and downloads local copies. Restore by stopping
Captain, replacing `captain.db` with a snapshot and starting it again
(step by step, including the `-wal`/`-shm` caveat, in
[docs/OPERATIONS.md](docs/OPERATIONS.md)).

## Line ingresses (IPLC)

A node behind an IPLC or dedicated line has more than one way in. Node page
→ Line ingresses registers each line with the addresses the provider gives
you: the local NIC address on the VPS (inbounds bind to it so replies go
back through the line), the line's far-end address (what a relay must
forward to; not reachable from the public internet), the provider's public
entry if the service includes one (e.g. a China Mobile entry IP), the
usable port range and an optional port offset. Inbounds pick an ingress
(direct is the default, or the line on nodes without a public address);
any protocol may ride a line, and a recipe applied while an ingress is
selected takes the first free, non-reserved port of the range. Whether a
protocol passes is up to the provider's entry (nobrand's carrier entry, for
one, only passes non-TLS protocols such as mieru). Entries then advertise
the public entry on the mapped port. A line
without a public entry is served through a relay node: add a port forward
there whose target is the far-end address (the picker fills it in) and use
the relay's address in the entry. Direct inbounds on the same node (hy2,
REALITY) keep using the node's public address or domain.

When the probe is on, every ingress with a local NIC address and a far-end
address gets an automatic RTT task (bosun >= 0.15 binds the TCP connect to
the NIC; a refused port still measures the line) shown under the ingress
name on the status and speed-test pages. Lines are usually private, so
carrier latency is not measured through them.

## Snell, mieru knobs, doctor

- **Snell** (bosun >= 0.18): inbound protocol `snell` on the `snell` core
  (Surge's snell-server v5, or v4). One shared PSK for everyone, so there is
  no per-user accounting or limit on such inbounds; Surge, Stash and mihomo
  subscriptions carry it, sing-box and URI lists leave it out.
- **mieru knobs**: MTU, multiplexing level and handshake mode per inbound
  reach the mierus:// links and mihomo/Stash lines; transport `BOTH` serves
  TCP on the port and UDP on port + 1 (links list both, mihomo takes TCP).
- **Doctor**: bosun runs a self-check every 10 minutes (cores, listeners,
  line bindings, forwards, certificates, port clashes, firewall, disk,
  memory, panel link, clock) and sends it with its report when the verdicts
  change; the node page shows it and the node list flags failures.

## Port forwards (relay tunnels)

Node page → Port forwards: listen on a port of this node and relay raw
TCP, UDP or both to a landing server. Clients connect to the relay while
the landing inbound keeps doing auth and per-user accounting. Pick another
managed node's inbound as the target and one click creates an entry that
advertises this relay's address; the node reports each rule's reachability,
RTT, connections and bytes. A rule's backend is the built-in userspace
relay or, with bosun >= 0.18, nftables kernel DNAT (`nft` on the node,
IPv4 target, optional source preservation when the target routes replies
back through the node). Ports are checked against the node's own
inbounds. (Xray-style domain/IP splitting inside a tunnel is not offered:
use the routing rules on the landing node instead.)

Relays hide the client's address from the landing node, so by default every
connection arriving from one of the panel's own nodes counts as *one* online
device for the limit (and is marked "via relay" in the user drawer). For the
real per-client picture, tick "Expect PROXY protocol" on a landing inbound
that is reached only through this panel's forwards (xray only): forwards
targeting it send a PROXY protocol v2 header automatically (built-in relay
or realm backend), the landing node sees the real client and counts devices
exactly. Direct connections to such an inbound fail, by design.

**Device identification (HWID).** Happ, FlClashX, V2Box, Streisand and other
clients that follow the Remnawave/Happ convention send `x-hwid`,
`x-device-os`, `x-ver-os` and `x-device-model` when they fetch the
subscription. Settings → Subscription → *Device identification* turns this
on: each device is recorded, the plan's device limit (or a per-user override
in the user drawer, or the fallback limit) is enforced per device at fetch
time, and a device over the limit gets an empty document with
`x-hwid-max-devices-reached: true`, `x-hwid-limit` and an optional
`announce` text the client shows. Clients that send no `x-hwid` keep the
online-IP counting unless *Require x-hwid* is on (then they get 404). Users
see and remove their devices in the portal; admins see them, the per-user
limit and the recent fetch history (IP, client, device, what was served) in
the user drawer. History is kept 30 days.

## API tokens and MCP

Settings → API tokens & MCP issues personal bearer tokens (`cap_...`) for the
admin API and for AI agents: Captain serves the Model Context Protocol at
`POST /mcp` with tools for nodes, users, plans, orders, tickets, the probe
and TCPing; write tools require `confirm: true`. See `docs/MCP.md`.

## Speed test

Admin → Speed test: TCP-connect latency from the panel to every entry's
public address (what clients dial), one click or all at once, plus the
nodes' own probe results. Add a probe task of type `download` (a large file
URL) and each node reports its download throughput every 10 minutes, which
also lands in the status page history.

## Probe / status page

Settings → Probe. Off by default; nothing extra runs on nodes until it is on.
When enabled, bosun (>= 0.11) sends a light host beat every few seconds
(CPU, memory, swap, disk, load, network rate and totals, TCP/UDP/process
counts, uptime, IPv4/IPv6 reachability, host facts) plus latency: TCP-connect
checks against the CT/CU/CM carrier probe points and your own icmp/tcp/http
tasks. Captain folds beats into minute/hour/day buckets (48 h / 60 d / 2 y),
keeps a short in-memory ring for sparklines, and serves a status page:

- address: a path on the main domain (default `/status`) and/or dedicated
  hostnames (`status.example.com`) that serve only the page; both covered by
  the built-in certificates;
- visibility: public, signed-in users, or staff only; per-node "hide";
  node IPs hidden unless allowed; title/logo so the page can be de-branded;
- per node: region flag, provider, price, expiry, and a monthly NIC traffic
  allowance (limit, reset day, counting mode) with reset-aware counters,
  shown as a bar on the page;
- alerts through Telegram / mail / webhooks: node offline (grace period),
  sustained CPU/memory/disk over a threshold, monthly traffic at 80% and 100%.

The carrier latency targets default to the CT/CU/CM probe points
(cloudcpp); Settings → Probe → *Latency targets* replaces the set with
your own `name host:port` lines, and every node managed by Captain follows
the panel. A node running without Captain keeps its own `probe:` section
in bosun's config.yaml instead.


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
make build            # builds web/admin, web/portal, web/site and web/probe with pnpm, then the Go binary with them embedded
cp config.example.yaml /etc/captain/config.yaml       # set base_url
bin/captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
bin/captain serve -c /etc/captain/config.yaml
```

## License

MIT, see `LICENSE`.
