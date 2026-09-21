# Monitoring, limits and abuse control

The probe, the alerts, and the three features that watch what users do.
Version notes name the bosun release a feature needs; Captain 1.0 ships
against bosun 0.47.

## Probe and status page

Settings → Probe. Off by default — nothing extra runs on the nodes until it
is on. Once enabled, bosun (≥ 0.11) sends a light host beat every few
seconds — CPU, memory, swap, disk, load, network rate and totals, TCP/UDP
and process counts, uptime, IPv4/IPv6 reachability, host facts — plus
latency: TCP-connect checks against the carrier probe points and your own
icmp/tcp/http tasks.

Captain folds the beats into minute, hour and day buckets (48 h / 60 d /
2 y), keeps a short in-memory ring for sparklines, and serves a status
page:

- **Address** — a path on the main domain (`/status` by default) and/or
  dedicated hostnames (`status.example.com`) that serve only the page; both
  are covered by the built-in certificates.
- **Visibility** — public, signed-in users, or staff only; per-node "hide";
  node addresses hidden unless allowed; title and logo so the page can be
  de-branded.
- **Per node** — region flag, provider, price, expiry, and a monthly NIC
  traffic allowance (limit, reset day, counting mode) with reset-aware
  counters shown as a bar.
- **Alerts** — through Telegram, mail or webhooks: node offline (after a
  grace period), sustained CPU / memory / disk over a threshold, monthly
  traffic at 80 % and 100 %.

The carrier latency targets default to the CT/CU/CM probe points; Settings
→ Probe → *Latency targets* replaces the set with your own `name host:port`
lines, and every node managed by Captain follows the panel. A node running
without Captain keeps its own `probe:` section in bosun's config.yaml.

**After a panel restart** every node is given one grace period to beat
again before anything counts as offline, so a restart that took longer than
the grace period does not alert on the whole fleet.

**Alert batching** — node alerts raised within 30 s go out as one Telegram
message (cut to Telegram's 4096-character limit), so a panel-side blip that
takes every node offline at once does not page once per node. Webhook
events are still emitted per alert.

## Speed test

Admin → Speed test: TCP-connect latency from the panel to every entry's
public address — what clients actually dial — one at a time or all at once,
plus the nodes' own probe results. Add a probe task of type `download` (a
large file URL) and each node reports its download throughput every ten
minutes, which also lands in the status page history.

## Komari reporting

Settings → Komari reporting attaches every managed node to a Komari monitor
as an agent: give the Komari URL and its auto-discovery key and each node
(bosun ≥ 0.17) registers under its node name, reports metrics every few
seconds and answers Komari's ping tasks. Only the ping capability is
offered. This runs alongside Captain's own probe; turn the probe page off
if you prefer Komari's.

## DStatus

Settings → DStatus endpoint lets a [DStatus](https://github.com/fev125/dstatus)
panel scrape the nodes **without installing its neko-status agent**: every
node (bosun ≥ 0.52) serves `GET /stat` on the port you give, answers only
when the request carries the key in a `key` header, and returns the host
sample in neko-status' shape. In DStatus, add each server with that port
and key.

Do not use DStatus' own SSH-based agent installer for nodes Captain
manages. It would leave the monitoring panel holding root credentials for
every node, which is exactly the blast radius Captain's pairing tokens
exist to avoid.

This is a pull, unlike Komari's push, so the port has to be reachable from
the DStatus panel — bosun's firewall auto-open opens it while the setting
is on, and the node's doctor warns when the endpoint is up but has never
been scraped (usually a firewall or the wrong address in DStatus) or when
scrapes are being refused for a wrong key.

Two figures will not match DStatus': it counts whole-interface traffic,
Captain counts the per-user proxy traffic it bills, so the interface number
runs higher. Per-core CPU and per-interface counters come back empty
because bosun does not measure them; everything DStatus renders from
`cpu.multi`, `mem`, `disk` and `net` is real.

## Metrics

`GET /api/admin/metrics` serves Prometheus exposition (request counters and
latencies, node and user gauges, job outcomes). It sits behind the admin
allow-list like every other admin route, so a scraper's address has to be
on it. Each bosun node also exposes its own `/metrics`.

## Connection log (off by default)

Settings → Connection log makes every node report each accepted connection
— user, inbound, client address, destination host and port, TCP or UDP —
taken from the cores' own logs (sing-box, xray, hysteria; mieru has none).
Rows are listed per user in the user drawer ("Connections") for abuse
reports and support.

This is personal data about what your users visit. Keep it off unless you
need it, keep the retention short, and say so in your privacy notice;
switching it off deletes what was collected. Needs bosun ≥ 0.42.

Two caps bound the table: the retention in days (7 by default) and the rows
kept per user (1000 by default). Both run hourly, in batches, so the
cleanup never holds the database. What that costs at scale is measured in
[COMPATIBILITY.md](COMPATIBILITY.md#database-sqlite-only-and-what-that-is-good-for).

## Audit rules

Settings → Audit rules is a panel-wide list every node receives. A `block`
rule becomes a route rule on sing-box and xray — the connection is rejected
— and every hit, block or `log`, comes back with the next report as user,
client address and destination.

The match syntax is the routing one: `domain:`, `full:`, `keyword:`,
`regexp:`, `ip:`, `port:`, `inbound:`, `geosite:`, `geoip:`,
`protocol:bittorrent`. A match a core would refuse is dropped on the node
and named by its doctor ("Panel rules") instead of breaking the config.

- **hysteria cannot route**, so it does not block: it reports the hits it
  sees in its own request log, and the panel marks them log-only and keeps
  them out of the auto-ban count. mieru inbounds can neither block nor
  report.
- Hits are listed in the settings card and per user in the user drawer,
  kept 90 days, and optionally sent to the admin chat.
- **Auto-ban after N hits in M hours** bans the user — never staff, and
  only counting `block` hits of a rule that still exists.
- Attribution comes from the cores' logs, not from an API: bosun drops a
  log line that names a user the node does not serve on that inbound, and
  the panel ignores a hit for a rule it never issued, but treat it as
  advisory rather than proof.

Needs bosun ≥ 0.43 (0.45 for the validation and the log checks).

## Dynamic speed limit

Settings → Dynamic speed limit throttles a user whose average rate across
all nodes stays above the trigger for the trigger window (100 Mbps over
60 s by default) down to a lower speed for a while (30 Mbps for 10 min),
optionally only during given hours and never for whitelisted users.

The panel computes the rate from the node reports, spread over the window
each report says it covers (bosun ≥ 0.46 states it; older agents are
assumed to cover the push interval), so a backlog of reports after a panel
restart cannot look like one enormous burst.

The throttle reaches the nodes as a temporary user speed limit (the minimum
of it and the plan's) and lapses on its own. It is applied with `tc`, so it
takes effect without restarting a core — **except** for users who have no
speed limit at all, which needs the core to be reloaded: those are left
alone unless *also throttle users with no speed limit* is on. The user
drawer shows an active throttle, can lift it, and can set one by hand.

## Device limits

Plans' device limits reach the nodes, where bosun counts client addresses
per user from the cores' logs (xray, hysteria, sing-box) and Captain locks
out users over their limit. mieru inbounds cannot report addresses.
Connections arriving from one of the panel's own nodes count as one device
together, because a relay hides the client — see
[NODES.md](NODES.md#port-forwards-relay-tunnels) for the PROXY protocol
option that gives the exact count. Counting per device instead of per
address is what [HWID](SUBSCRIPTIONS.md#device-identification-hwid) does.

## Traffic thresholds and connection events

Settings → Mail → *Traffic thresholds* lists the used-percentages (90 by
default) at which a user is told, once per quota period, by Telegram or
mail; each crossing also emits a `subscription.traffic` webhook.

The panel stamps the first traffic it ever sees for a user (shown in the
user drawer) and emits `user.first_connected`. A user who has held a usable
plan for a day without ever connecting emits `user.not_connected` once, for
onboarding follow-up.

## Disk checks

Self-update refuses to download when the binary's filesystem lacks twice
the asset size plus headroom, and the backup job refuses a snapshot when
the backup directory lacks twice the newest backup plus headroom — instead
of filling the disk halfway.
