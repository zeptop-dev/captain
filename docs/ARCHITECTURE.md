# Captain architecture

Captain is the unified panel: users, plans, orders and payments, subscription
output, node fleet, forwarding policy, and configuration push to
[bosun](https://github.com/zeptop-dev/bosun) agents. It is a fresh project; it
does not import or emulate Xboard.

## Shape

One Go binary, one database. The admin console, the user portal, the landing
page and the status page are React apps built with Vite and embedded into the
binary with `embed` (`web/embed.go`). Nothing else to deploy: no PHP, no
Redis, no queue worker.

```
captain/
  cmd/captain/            entry: serve | migrate | admin create | version
  migrations/             goose SQL files, embedded (Up only, applied on start)
  internal/
    config/               YAML config (listen, base_url, data_dir, tls, database, agent, payments, portal, limits, trusted_proxies)
    db/                   database/sql + goose; sqlite only (driver "postgres" is refused by db.Open)
    domain/               plain Go types and rules (User, Plan, Subscription, Order, Node, Inbound, Entry, Coupon, Ticket, GiftCode, Article)
    store/                repositories: hand-written SQL, one file per aggregate
    service/              use cases: orders, subscriptions, node desired state (agentstate), certs, dns, external nodes, probe, sub links
    jobs/                 in-process scheduler, one tick a minute
    http/                 net/http routes and middleware; JSON API under /api, session auth
      admin/              admin API (/api/admin): objects, settings, self-update, backups, domains, certificates, ingresses, forwards, node jobs, speed test, sub templates + designer
      portal/             user API (/api/portal)
      agent/              bosun-facing API (/api/agent: pair, state, report, beat, install.sh)
      sub/                subscription endpoint (/sub/{token}, short links /s/{code})
      payment/            gateway callbacks and pages under /api/payment/
      oauth/              OpenID Connect login (/api/oauth)
      site/               landing page data (/api/site)
      probe/              status page data and router (/api/probe; page under a path or on dedicated hosts)
      mcp/                Model Context Protocol server (/mcp)
      ratelimit/          login and pairing lockout, client IP resolution behind trusted proxies
    auth/                 argon2id passwords, random tokens, TOTP
    payment/              gateways: epay, stripe, alipay, coinbase, coinpayments, btcpay, mgate
    certs/                panel HTTPS via certmagic (HTTP-01 or Cloudflare DNS-01) and the DNS-01 issuer for panel-managed certificates
    dns/                  Cloudflare A/AAAA records for node host names and line entry names
    backup/               daily VACUUM INTO snapshots, WebDAV / S3 upload
    mail/                 SMTP / Resend transactional mail, settings in the database
    telegram/             Bot API client (sendMessage, getMe, getUpdates) and long-polling command handler
    webhook/              signed event delivery to operator URLs
    notify/               fan-out of user and admin notices to Telegram, mail and webhooks
    captcha/              Turnstile / reCAPTCHA / hCaptcha verification
  web/
    admin/                React 19 + TypeScript + Mantine 8 + TanStack Query + react-router
    portal/               same stack, user-facing
    site/                 landing page (globe, plans, FAQ)
    probe/                public status page
```

Language and library choices:

- Go 1.26 standard `net/http` with pattern routing; no web framework.
- `database/sql` with `modernc.org/sqlite` (pure Go, keeps the single static
  binary), opened with WAL, foreign keys, `busy_timeout=5000` and a single
  connection (`SetMaxOpenConns(1)`): one Captain process per database file.
  The config accepts `database.driver: postgres` but `db.Open` rejects
  anything except `sqlite`. Migrations embedded via goose; only `Up` runs.
- Passwords argon2id; sessions are HttpOnly cookies (`captain_session`,
  30 days) backed by a table, flagged `admin` only when minted by the admin
  login (password + TOTP), so portal, OIDC and password-reset sessions never
  reach `/api/admin`. Staff can also use personal API tokens (`cap_…`, same
  role as the owner). Agents authenticate with a per-node token created at
  pairing time; the store keeps its SHA-256.
- Frontends built by pnpm + Vite (`make web` before `go build`); dev serves
  them with a proxy to the Go API (`/api`, `/sub` → 127.0.0.1:8080).
- Logging is `log/slog` text on stderr; `log_level` in the config.

## Domain model

```
User ──< Subscription (plan, starts/expires, quota, used up/down, reset_at, status active|queued|expired|cancelled)
User ──< Order (plan, period, amount, coupon, surplus credit, gateway, status, activation)
User ──< Ticket ──< TicketMessage;  Coupon;  GiftCode;  Article;  Group (user_groups)
Plan  (base period + extra PlanPrices, quota, device and speed limits, reset mode never|days|monthly|yearly, group)
Node  (bosun agent: addresses{public, internal, v6}, domain, token hash, version/platform/hostname,
       last_seen_at, applied_revision, upgrade_to; JSON columns for forwards, outbounds, routes,
       default outbound, DNS servers, WARP account, per-core overrides; decoy site, node speed limit, mita quotas)
Ingress (per node: bind IP, far-end IP, public entry host, port range and offset, reserved ports)
Inbound (node, tag, protocol, listen, port, core, spec.Inbound settings, group, ingress)
Entry  (what users see: display host + port on top of an Inbound, tags, region, client extra fields, sort)
Domain / Certificate (registered zones with a Cloudflare token; PEM pairs issued, uploaded or received by webhook)
ExternalSource ──< ExternalNode (imported share links and airport subscriptions)
NodeJob (one-off task carried in the state, answered in the report)
```

- **Inbound** is the protocol server on a node: what bosun renders. Its
  settings are bosun's `spec.Inbound`, so there is no mapping layer.
- **Entry** is the subscription line. Several entries may point at the same
  inbound (many relays, one landing); each is its own line in the client.
  Accounting happens at the landing, so a user's traffic is counted once
  whichever entry they use. Group-restricted inbounds only appear for their
  group; `user_entry_blocks` hides single entries from single users.
- **Relays** are per-node port forwards (`nodes.forwards_json`, one
  `spec.Forward` per rule) rather than chain objects: a rule on the relay
  node targets a landing inbound and an entry advertises the relay's
  address. The `chains` table and `entries.chain_id` from the first schema
  are not used by the store.
- **Line ingresses** cover the IPLC case without a relay: the inbound binds
  to the line's local address and the entry advertises the provider's
  entrance on the mapped port.
- mieru is an inbound on the `mita` core with strategy presets (IPLC /
  public / stealth / custom) and per-inbound MTU, multiplexing and handshake
  knobs; the display host and port live on the entry, separate from the
  listen port.

## Agent protocol (Captain <-> bosun)

Designed around bosun's `spec` types so there is no mapping layer.

- Pairing: admin creates a node, gets a one-time pairing code; bosun posts it
  to `POST /api/agent/pair` (throttled per address) and receives a
  permanent node token.
  `GET /api/agent/install.sh?pair=CODE` serves the install one-liner while
  the code is still redeemable.
- Desired state: `GET /api/agent/state` returns `agentproto.State`
  (`revision`, `node: spec.Node`, `users: []spec.User`, `forwards:
  []spec.Forward`, `pull_seconds`, `push_seconds`, optional `probe`,
  `komari` and `jobs`) with an ETag. With `?wait=` (capped at 50 s) and a
  matching `If-None-Match` the request is held, re-checked every two
  seconds, so edits reach nodes within seconds without a persistent
  connection. `service.AgentState` builds the state and caches it per node
  (10 s TTL; admin writes, reports and job ticks invalidate it).
- Reports: `POST /api/agent/report` carries `agentproto.Report` (`version`,
  `revision`, `traffic`, `online` user → client IPs, `forwards`, `cores`,
  `certs`, `host`, `doctor`, `jobs` results, per-`inbounds` and
  per-`outbounds` traffic). Captain updates `last_seen_at`, stores doctor
  and job results, charges traffic only for users the node actually serves
  (other samples are dropped with a warning), records inbound/outbound
  totals, online IPs and forward status, then answers
  `{state_changed, upgrade_to}` so the node pulls at once or upgrades.
- Beats: `POST /api/agent/beat` is a light host sample sent every few
  seconds while the probe page is on.
- Node jobs: `POST /api/admin/nodes/{id}/jobs` queues a task
  (`reality_scan`, `warp_register`) that rides in the next state and comes
  back in the report.
- Token in `Authorization: Bearer <node token>`; JSON on the wire.

The wire types live in bosun's public module (`pkg/spec`, `pkg/agentproto`)
so both sides compile against one definition; Captain also imports bosun's
`pkg/subscription`, `pkg/subdesign`, `pkg/selfupdate` and `pkg/wg`.

## Subscription

`GET /sub/<token>` with client detection by User-Agent and an explicit
`?client=` override; `GET /s/<code>` serves short links, and temporary links
limited by uses or hours live in `sub_links`. `service.Subscription`
assembles the user's lines (the landing inbound's protocol settings plus the
entry's display host, port and client extra fields, plus external nodes the
user's groups may see) and bosun's `pkg/subscription` renders the client's
format. Per-format templates (settings key `sub_templates`) and the visual
designer (`sub_design`, bosun's `pkg/subdesign`) shape the document around
the servers. Hosts listed under Settings → Subscription URLs answer nothing
but `/sub/`.

### Several plans per user

A user may hold several subscriptions at once (`subscriptions.status`
`active`), plus `queued` ones bought "after the current plan". Rules:

- Buying the plan the user already holds renews it in place (time extends,
  quota refills). A different plan stacks by default; with the admin switch
  *one plan at a time* (`subscription.single_plan`) it replaces every active
  one and the surplus credit applies, as before multi-plan.
- Access is the union: `Store.AccessGroups` = groups of every usable
  subscription's plan plus `users.group_id`. `EntriesForUser`,
  `ExternalNodesForGroup` and `UsersWithAccess` all take that set.
- Traffic lands on one subscription (`chargeableTx`): among usable ones,
  those whose plan group owns the inbound, then the soonest expiring; with
  nothing usable the most recent active row keeps counting.
- Device and speed limits take the widest value across usable plans; a
  plan with 0 (unlimited) wins.
- `Subscription-Userinfo` sums usage and quota (unlimited if any plan is)
  and reports the latest expiry (never if any plan never expires).
- A queued subscription keeps `period_days`; `Store.PromoteQueued` (every
  job tick) starts the oldest one for a user with no usable active plan.
  Buying "after the current plan" while nothing is usable starts it now.
- `ActiveSubscription` still returns one row (the usable one lasting
  longest) for callers that only show a summary.

## Background jobs and limits

`internal/jobs.Runner` ticks every minute (first tick at start): cancels
unpaid orders older than 30 min, expires subscriptions, starts queued ones,
resets quota when a plan's reset point passes (advancing `reset_at`), purges
expired sessions and stale `online_devices` rows (10 min), then invalidates
the cached node state. On top of that: external subscriptions re-sync every
hour and external nodes are TCP-probed every 10 minutes; probe offline checks
run every tick and probe stats are pruned hourly; expiry (3 days ahead) and
90 %-traffic reminders go out hourly via Telegram, mail and the
`subscription.expiring` webhook, once per period; panel-issued certificates
are renewed 30 days before expiry; the daily traffic history is pruned to 400
days once a day; and the backup manager takes the day's snapshot once the
configured hour has passed. Every job logs `<name> failed` under
`component=jobs` when it errors.

Device limits: agents report per-user client IPs (`Report.Online`) for cores
that know them (Xray from its stats API, Hysteria from auth callbacks,
sing-box from its connection log). `AgentState.Build` withholds a user whose
distinct IPs over the last 3 min exceed the plan's device limit and keeps
them withheld for 5 min so nodes do not flap; the user comes back once old
IPs age out. `limits.enforce_devices: false` turns this off. mita (mieru)
inbounds never contribute IPs, so they cannot trigger the limit. Plan speed
limits and a node-level default reach the nodes in the state, where bosun
enforces them.

## Payments

`payment.Gateway` interface: `Name()`, `Create(ctx, order, plan, user,
clientIP) (*Checkout, error)` returning the URL to open, and
`Notify(*http.Request) (*Notification, error)`, which must verify the
signature before reading any field. A gateway that serves its own page (the
Alipay QR page) also implements `Pager`, mounted at
`GET /api/payment/<name>/page`. Gateways: `epay` (v1 MD5 / v2 RSA), `stripe`
(Checkout + webhook), `alipay` (当面付), `coinbase`, `coinpayments`, `btcpay`,
`mgate`, plus the built-in balance; each is enabled by its block under
`payments:` in the config and reports at `/api/payment/<name>/notify`.
`service.Orders` settles: the callback amount must match the order,
repeated callbacks are idempotent, a late callback revives a cancelled
order, and `OnPaid` invalidates node state and emits `order.paid`.

## Monitoring

- **Probe / status page**: nodes send beats while Settings → Probe is on;
  `service.Probe` keeps the latest sample and a short ring per node, folds
  beats into minute/hour/day buckets (`node_stats`, `node_ping_stats`),
  raises offline / threshold / monthly-traffic alerts through `notify`, and
  `http/probe` serves the page under a path of the main site or on dedicated
  hosts with its visibility rules.
- **Komari**: the Komari settings ride in `state.komari`, so every managed
  node registers itself as a Komari agent.
- **Metrics**: `GET /api/admin/metrics` (admin session or API token) is a
  Prometheus text exposition: node, user, subscription and order gauges,
  per-node `captain_node_last_seen_seconds`, backup outcome, and counters
  for rejected payment callbacks, settle failures, job errors, node reports
  and state builds.
- Each node may still carry an external monitor link (`monitor_url`).

Operational detail (deployment, backup and restore, upgrades, what to watch)
is in [OPERATIONS.md](OPERATIONS.md).

## Where the rest lives

- Domains and certificates: `store/domains.go`, `store/certificates.go`,
  `service/certs.go` (issue via `certs.ACMEIssuer`, renew in the job tick),
  `internal/dns` (automatic A/AAAA records), `http/admin/domains.go`,
  `http/admin/certificates.go`, and `POST /api/hooks/certificate` for
  certificate managers. Certificates reach nodes in `node.certificates`.
- Node-side features pushed through the state: line ingresses
  (`store/ingresses.go`), port forwards (`store/forwards.go`), outbounds,
  routes, DNS and WARP (`nodes` JSON columns), per-core config overrides
  (`nodes.overrides_json`, checked against bosun's `spec.CheckOverride`
  denylist), decoy site, mita native quotas, Komari and probe settings.
- External nodes: `service/external.go` parses share links and syncs
  airport subscriptions; `store/external.go` keeps them per group.
- Money: coupons, commissions, withdrawals, gift codes, surplus credit and
  trial plans in `store/coupons.go`, `store/giftcodes.go`, `store/orders.go`
  and the settings keys `invite`, `surplus`, `trial`.
- Support and content: tickets, articles (knowledge base), client
  downloads, announcements; Telegram bot binding and notices.
- Access control: staff roles (`admin`, `operator`, `support`; the path and
  method matrix in `http/admin`), TOTP for staff, personal API tokens, the
  admin IP allow-list (`security` setting), login lockout after five
  failures, registration limits and captcha for the portal.
- Integrations: event webhooks (`internal/webhook`), OIDC login
  (`http/oauth`, `identities` table), MCP (`http/mcp`, see
  [MCP.md](MCP.md)).
- Self-update: bosun's `pkg/selfupdate` (`GET /api/admin/system/update`,
  `POST …/update/apply`, `POST …/update/rollback`, `POST …/system/restart`)
  plus node upgrades (`upgrade_to` answered in the report).
