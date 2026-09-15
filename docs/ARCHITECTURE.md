# Captain architecture

Captain is the unified panel: users, plans, orders and payments, subscription
output, node fleet, forwarding policy, and configuration push to
[bosun](https://github.com/zeptop-dev/bosun) agents. It is a fresh project; it
does not import or emulate Xboard.

## Shape

One Go binary, one database. The admin console and the user portal are React
apps built with Vite and embedded into the binary with `embed`. Nothing else
to deploy: no PHP, no Redis, no queue worker.

```
captain/
  cmd/captain/            entry: serve | admin create | migrate
  internal/
    config/               YAML config (listen, db, base_url, payments)
    db/                   database/sql + embedded goose migrations; sqlite default, postgres optional
    domain/               plain Go types and rules (User, Plan, Order, Node, Inbound, Entry, Chain)
    store/                repositories: hand-written SQL, one file per aggregate
    service/              use cases: orders, subscriptions, node desired state, accounting
    http/                 net/http routes and middleware; JSON API under /api, session auth
      admin/              admin API
      portal/             user API
      agent/              bosun-facing API
      sub/                subscription endpoint
    subscription/         per-client renderers (clash-meta, sing-box, shadowrocket, surge, ...)
    payment/              gateways: epay, stripe
    agentproto/           wire types shared with bosun (imported from bosun/pkg)
    jobs/                 in-process scheduler: order expiry, traffic reset, health aggregation
  web/
    admin/                React + TypeScript + shadcn/ui + TanStack Query + react-router
    portal/               same stack, user-facing
```

Language and library choices:

- Go 1.26 standard `net/http` with pattern routing; no web framework.
- `database/sql` with `modernc.org/sqlite` (pure Go, keeps the single static
  binary) and `pgx` for Postgres. Migrations embedded via goose.
- Passwords argon2id; sessions are HttpOnly cookies backed by a table;
  agents authenticate with a per-node token created at pairing time.
- Frontend built by pnpm + Vite, output committed to `web/dist` only in
  releases; dev serves it with a proxy to the Go API.

## Domain model

```
User ──< Subscription (plan, expiry, quota, used) ──< Device (online tracking)
Plan  (price, quota, period, node groups)
Order (user, plan, amount, gateway, status)
Node  (bosun agent: name, addresses{public, internal, v6}, token, status, metrics)
Inbound (node, protocol, listen port, protocol settings, node groups)
Entry  (what users see: display host + port, points at an Inbound, or at a Chain)
Chain  (ordered hops: node, listen port, tcp/udp, connect-address kind; ends at an Inbound)
```

- **Inbound** is the protocol server on a node: what bosun renders.
- **Entry** is the subscription line. Several entries may point at the same
  inbound (many relays, one landing); each is its own line in the client.
  Accounting happens at the landing, so a user's traffic is counted once
  whichever entry they use.
- **Chain** expands to `forwards` rules on relay hops. The IPLC case has no
  chain at all: the entry's display host is the provider's entrance and the
  inbound listens on the box behind it.
- mieru-nobrand is not a protocol; it is an inbound with display host/port
  separate from the listen port, a traffic-pattern preset (iplc, balanced,
  stealth), and optional dedicated ports for specific users.

## Agent protocol (Captain <-> bosun)

Designed around bosun's `spec` types so there is no mapping layer.

- Pairing: admin creates a node, gets a one-time pairing code; bosun posts it
  to `POST /api/agent/pair` and receives a permanent node token.
- Desired state: `GET /api/agent/state` returns `{node: spec.Node, users:
  []spec.User, forwards: []spec.Forward, revision}` with ETag; bosun polls
  (long-poll with `wait=30s`) so changes apply within seconds.
- Reports: `POST /api/agent/report` carries `{traffic: []UserTraffic,
  online: map[user][]ip, probes: []ForwardStatus, cores: map[name]state,
  host: SystemStatus}`.
- Token in `Authorization: Bearer <node token>`; JSON on the wire.

The wire types live in bosun's public module (`pkg/spec`, `pkg/agentproto`) so
both sides compile against one definition.

## Subscription

`GET /sub/<token>` with client detection by User-Agent and an explicit
`?client=` override. Each renderer takes the user's entries and emits the
client's format. Entries carry the landing inbound's protocol settings plus
the entry's display host and port.

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

`internal/jobs.Runner` ticks every minute: cancels unpaid orders older than 30 min, expires subscriptions, resets quota when `plans.reset_days` elapses (advancing `reset_at`), purges expired sessions and stale `online_devices` rows (10 min).

Device limits: agents report per-user client IPs (`Report.Online`) for cores that know them (Xray, Hysteria). `AgentState.Build` withholds a user whose distinct IPs over the last 3 min exceed `plan.device_limit`; the user comes back automatically once old IPs age out. `limits.enforce_devices: false` turns this off. sing-box and mita inbounds never contribute IPs, so they cannot trigger the limit.

## Payments

`payment.Gateway` interface: `Create(order) (redirectURL, error)` and
`HandleNotify(http.Request) (orderID, paid, error)`. First gateways: EPay
(易支付 MD5-signed form + notify) and Stripe Checkout (webhook). Orders are
idempotent on gateway notifications.

## Monitoring

Captain keeps only what it needs to operate: node online state, per-node
totals, chain health from bosun probes. Detailed host monitoring is linked to
an external system (komari, nezha) configured per node.

## First milestone

Users, plans, orders with manual confirmation plus EPay and Stripe,
subscriptions for the main clients, nodes and inbounds including
mieru-nobrand fields, user groups to node groups, traffic accounting, bosun
driver with pairing, a minimal user portal (login, subscription, buy). No
tickets, commissions, coupons, chain UI or DNS failover yet.
