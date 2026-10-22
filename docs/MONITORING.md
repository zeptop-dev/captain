# Monitoring, limits and abuse control

The probe, the alerts, and the three features that watch what users do.
Version notes name the bosun release a feature needs; Captain 1.0 ships
against bosun 0.47.

## Probe and status page

Monitoring → Collection and status page → Probe. Off by default — nothing extra runs on the nodes until it
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
- **Alerts** — through the admin Telegram chat or webhooks: node offline (after a
  grace period), sustained CPU / memory / disk over a threshold, monthly
  traffic at 80 % and 100 %.

Monthly traffic directions use the **server network-interface perspective**:
**outbound (TX)** is data sent by the selected interfaces, and **inbound (RX)**
is data received. Choose their sum, outbound only, inbound only, or the larger
of the two period totals. This includes all traffic on those interfaces and is
independent of proxy-user billing. Existing API mode values `up` and `down`
continue to mean outbound and inbound respectively.

The monthly traffic limit accepts decimals with an MB, GB or TB selector.
These use the status page's existing 1024-based units: 1 TB = 1024 GB and
1 GB = 1024 MB. For example, select TB and enter `1.1` for a 1.10 TB
allowance. Zero remains unlimited. Changing units preserves the allowance;
reopening the form or saving other settings keeps the stored byte precision.

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

## Shared node groups and ordering

Manage groups, node order and batch moves in **Nodes → Groups and order**.
These are the same monitoring groups used by the status page. Public group
names and default node order follow the saved layout; visibility is unchanged.
See [node organization](NODES.md#groups-and-display-order).

## Automatic country / region

Node details → Probe and monthly traffic → Region offers, in order:
**Do not show**, the current **automatic** country, and searchable flag/code/name
choices. Names follow the admin UI language. Manual choices are fixed; automatic
mode uses bosun's independently observed machine public IP, preferring IPv4 and
using IPv6 only when IPv4 is absent. A differing IPv6 country is shown as a hint.
A failed IPv4 lookup does not silently switch to the IPv6 country.

Captain queries [Country.is](https://country.is/) over HTTPS with the public IP
only. No node hostname, token or credentials are sent. A country lookup is an
estimate, not proof of physical location; use manual selection for broadcast IPs
or inaccurate database entries. The form shows the lookup IP and time and offers
**Detect / refresh country**. Detection previews do not change the saved choice.
Public pages receive only the effective country code, never lookup metadata or
the detection IP through this feature.

Automatic mode checks for changes every minute using recent bosun observations
(15 minutes). Successes are cached per IP for seven days; unknown/failed lookups
retry after an hour. Interactive refresh is limited to once per minute per cached
IP; all requests share a bounded rate-limited stream. An IP change immediately
invalidates the previous country's display on the next uncached page read, and
late results are discarded if the observed IP or pairing identity changed.
Offline nodes keep their last matching country but do not trigger new lookups.

Migration 77 preserves existing nonempty region codes as manual choices and
existing empty regions as Do not show. Newly added nodes default to automatic.
Automatic observations require bosun's public-address report (v0.69.0+); manually
configured public entries, NAT mappings, domain records and HTTP peer addresses
are not substitutes. No further bosun upgrade is needed. The probe update API
adds optional `RegionMode: auto|manual|off`; legacy clients can still change the
country explicitly and an unchanged effective country preserves automatic mode.

## DNS health checks

Node details → DNS health and Monitoring → DNS health show panel-side DNS
diagnostics. No bosun upgrade is required. Captain checks configured node and
public-entry names about every five minutes, independently of node resource
collection. The dashboard self-check links to unresolved DNS findings. Operators
can read and recheck; support accounts and the public status page cannot access
these diagnostics. Automatic-write history is restricted to full administrators.

- Direct names: the resolved A/AAAA address set must match configured public
  IPv4/IPv6 addresses; missing or extra addresses are mismatches. IP literals
  themselves do not need a DNS check.
- NAT/dedicated-line ingress names: compare with the provider's **public entry
  address**, never the local bind or private line IP. A domain-valued entry host
  without a fixed expected IP is checked for resolution only.
- Shared/external DNS: check resolution without comparing the CDN or load
  balancer IPs with origin node IPs. Different healthy resolver answers are
  allowed. Legacy exclusive names assigned to multiple nodes report an ownership
  conflict until renamed or explicitly marked shared.
- Both the panel resolver and Cloudflare's `1.1.1.1:53` are queried for A/AAAA.
  A/AAAA queries follow aliases, but do not inspect every CNAME or delegation.
  A timeout or unavailable resolver is **unknown**, not proof of a missing
  record. Conflicting direct-name answers are **inconsistent**: propagation,
  caching or geographic answers are possible, not a confirmed diagnosis.
  These are two resolver observations from the panel, not an authoritative
  global-DNS or proxy end-to-end check; the panel resolver may itself use
  Cloudflare. Public resolver access must be available from the panel.

Checks never modify DNS. One node has at most one check in flight, the panel
allows four at once, queries time out after four seconds and a node's query
budget is 25 seconds. Remaining targets are unknown when that budget expires.
Manual requests within 30 seconds reuse the latest result. Results older than
15 minutes or for a changed configuration are marked stale. Late results from
renamed/deleted nodes or edited entry/shared settings are discarded.

When **monitoring is enabled**, a stable DNS fault must occur in three checks
spaced at least five minutes apart and span at least ten minutes before raising
a DNS incident. Failure state survives panel restarts, but gaps over 15 minutes
restart the window. DNS incidents use the existing acknowledgment, maintenance,
silence and recovery lifecycle. Unknown or inconsistent checks cannot clear an
incident. Editing the checked configuration retires the old incident as disabled
instead of claiming recovery. Turning monitoring off suppresses DNS incidents;
read-only DNS diagnostics continue. DNS health, expected addresses and change
history are not included in the public probe APIs.

## Speed test

Admin → Speed test: TCP-connect latency from the panel to every entry's
public address — what clients actually dial — one at a time or all at once,
plus the nodes' own probe results. Add a probe task of type `download` (a
large file URL) and each node reports its download throughput every ten
minutes, which also lands in the status page history.

## Retired external monitoring integrations

Komari and DStatus registration, reporting and scrape endpoints have been removed.
Use the built-in resource/network monitoring and status page. Captain's Glassmorphism
React theme remains available and does not need a Komari server.

Legacy settings API paths retain their response shapes but return disabled settings;
enabling them is rejected. Protocol fields and old database migrations are retained
for compatibility. Captain no longer sends exporter configurations, so older managed
agents stop exporting after their next successful state pull. Upgrade standalone
agents to stop their exporters. Existing stored credentials are inert and are not
used for exporter connections; removing this feature does not delete remote monitoring accounts.

## Metrics

`GET /api/admin/metrics` serves Prometheus exposition (request counters and
latencies, node and user gauges, job outcomes). It sits behind the admin
allow-list like every other admin route, so a scraper's address has to be
on it. Each bosun node also exposes its own `/metrics`.

## Connection log (off by default)

Nodes → Node policies → Connection log makes every node report each accepted connection
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

Nodes → Node policies → Audit rules is a panel-wide list every node receives. A `block`
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

Nodes → Node policies → Dynamic speed limit throttles a user whose average rate across
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

Settings → Notifications and integrations → Mail → *Traffic thresholds* lists the used-percentages (90 by
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


## Resource detail (Captain 1.7 / bosun 0.56)

The node detail page shows per-interface byte counters and rates, local
filesystem space and inode usage, per-device disk throughput and IOPS,
logical CPU usage, and RSS/CPU for bosun and its supervised core/realm
processes. Process CPU uses 100% per logical CPU and can exceed 100%; RSS
is resident memory, not a sum of private allocations. Network filesystems
(NFS, CIFS, SMB, FUSE and automounts) are excluded to avoid blocking a
heartbeat on an unavailable mount. Disk devices/partitions can overlap;
the UI deliberately does not sum disk I/O or filesystem capacity.

Node → Monitoring → Include/exclude interfaces selects exact interface
names for host totals, rates and monthly monitoring traffic. An empty
include list excludes loopback, Docker, veth and bridge interfaces by
default. Explicit includes override defaults; explicit exclusions win.
All discovered interfaces remain visible with their inclusion status.
This setting never changes proxy-user billing. A newly included/recreated
NIC, reboot or agent restart establishes a new monthly counter baseline;
traffic before that baseline is not estimated or backfilled.

New samples carry validity flags. A missing reading or a rate without a
baseline is displayed as **—**, while measured zero remains zero. Invalid
readings do not dilute history averages or threshold alerts. Migration 54
adds per-metric sample counts; existing history remains as recorded because
older nodes did not distinguish missing readings. Replayed sample sequence
numbers within the same agent run do not enter history or monthly counters.

The resource sampler serializes consumers and caches results for one second.
Beats, minute reports, the standalone panel and external exporters share its
baselines. High-frequency beats/history and latency tasks follow the probe
collection switch; minute host reports still operate. The separate **Enable
status page** switch controls the page and public probe API while collection
and alerts can continue. New process, device and mount details are available
only through authenticated management APIs, never the public status payload.

Older bosun nodes continue to send their original summaries. Detailed
resource fields and interface selection require bosun 0.56 or later. The monitoring workspace below stores detailed history for these nodes; older
nodes still contribute their host summaries.

## Monitoring workspace (Captain 1.7)

Open **Monitoring** in the management sidebar. Filter by name, region,
provider, monitoring group and connection status; sort resource usage and
compare up to four nodes using the same metric and time range. Monitoring
labels are separate from user/access groups. Click a group in the table to
edit it; blank removes the label. Group names also appear on the status page.
Administrators and operators can use the workspace; support accounts cannot.

Each node's resource history selector includes host summaries, individual
interfaces, filesystem/inode usage, disk throughput/IOPS, logical CPUs and
managed processes. Interfaces excluded from aggregate accounting remain
available individually. A process series follows the supervisor name across
restarts, while its rate baseline still resets with process identity.

Migration 55 stores compressed buckets with per-metric valid sample count,
sum and maximum. Averages include measured zero and exclude missing values;
peaks are the largest sampled value, not an inferred sub-interval maximum.
Whole missing intervals return null points so charts break across gaps.
Buckets use Captain's receipt time in UTC, not the node's potentially skewed
clock. Sequence replay detection covers history and monthly traffic in one
transaction. History starts when this version is installed; old summaries
cannot reconstruct per-device history or peaks.

Detailed retention is independent from the existing aggregate history:

| Resolution | Retention | Workspace ranges |
| --- | --- | --- |
| Minute | 24 hours | 1h, 24h |
| Hour | 14 days | 7d, 14d |
| Day | 90 days | 30d, 90d |

Hourly cleanup prunes expired buckets. One compressed row holds a node's
bucket, with at most 512 metric series; a query catalog has at most 1,024.
A warning identifies truncation instead of silently promising all devices
on very large hosts. Historical device names stay selectable within the
requested window. Node deletion and site reset remove these records.

Latest snapshots persist independently of liveness. A task/status request
cannot refresh old resource readings. If high-frequency collection is off,
minute reports still populate the workspace, but do not create resource
history. The fleet history/comparison workspace lives in Captain; bosun
standalone retains its local resource detail and interface controls.

Management APIs:

- `GET /api/admin/monitoring`: fleet summaries, group, last seen, sampled time
  and stale flag. Detailed device/process lists are not duplicated here.
- `PUT /api/admin/nodes/{id}/monitor-group`: `{ "group": "Edge" }`.
- `GET /api/admin/nodes/{id}/resource-history?range=24h&series=host::cpu`:
  timestamp grid with average, peak and valid sample count; a bounded series
  catalog supplies opaque keys for device selection. URL-encode the full key.

**Monitoring → Collection and status page → Probe** also controls public sections and card/compact layout.
CPU, memory, disk, network, system facts, server details, traffic, latency and
history can be shown independently. The server masks hidden sections in both
snapshot and history APIs; disabling history or latency also closes the
corresponding history endpoint. Device names, mount paths and process details
are always private. Existing clients that omit `public_sections` or `layout`
preserve saved choices. The status page offers node/group/status filters,
responsive layouts and the same six languages as the management UI.

## Network quality (Captain 1.7 / bosun 0.56)

Node → Monitoring shows a **Network quality** card with the latest attempt,
classified outcome, HTTP status, completion time and stale indication. Bosun's
standalone Probe page shows the same detail. Public node detail follows the
latency/history visibility settings; private detail remains available when the
public page is disabled.

| Check | Successful attempt | Latency |
| --- | --- | --- |
| ICMP | One echo reply | Echo round-trip time |
| TCP | Connection established | TCP connection time; DNS is separate |
| Carrier/line reachability | Connection established or explicitly labelled refusal | Connection/refusal response time |
| HTTP | Status 200–399, redirects not followed | Request start through response headers |
| Download | Status 2xx with bytes received | Response headers; throughput covers the bounded body transfer |

Timeouts, connection refusal, DNS/TLS errors, permission errors, unreachable
hosts, HTTP errors, I/O errors and empty downloads have separate reason labels.
No raw error, URL or response body is included in those labels. Task targets are
administrator-configured and may deliberately address private line endpoints.
`source_ip` binds all four task types. Periodic checks never select the best of retries.

The latest result includes an exact window of up to 30 attempts: failure count,
min/max, nearest-rank P50/P95, and mean absolute difference of consecutive
successful latencies (jitter). A failure breaks adjacency. A successful refused
carrier/line result is labelled **TCP reachability**, not mistaken for service
availability. The failure percentage is an attempt ratio; only ICMP uses echo
loss, and TCP/HTTP cannot infer packet loss from these measurements.

DNS, TCP connect, TLS handshake and HTTP response-wait durations are separate.
Response wait runs from writing the request to the first response byte. Only
completed phases have a value: literal-IP DNS, plain-HTTP TLS and interrupted
phases remain unknown rather than zero. Total attempt duration also includes
failed waits and, for downloads, the body transfer. The phases need not sum to
latency because of scheduling and request/header overhead.

History offers 1h/24h/7d/14d/30d/90d ranges, failure ratio, average latency,
P50/P95, jitter and phase averages. It shares the existing latency history
retention (minute 48h, hour 60d, day 2y); the UI selects minute/hour/day resolution
for those ranges. Empty buckets break lines. Phase averages use only completed
measurements; old agents continue to supply averages/failures without fabricated
phase data. Historical percentiles use a bounded logarithmic histogram with an
upper-bin error of at most 5% or 0.1 ms; min/max are exact. Aggregation combines
histogram counts, never averages of smaller-window percentiles.

Each node holds at most 60 pending attempts per target until a successful beat.
Failed storage returns HTTP 500 so the node keeps its pending batch.
Migration 56 persists a cursor per node/task/name/run epoch, so cached reads,
beat retries and Captain restarts do not recount attempts. Gaps caused by queue
overflow appear as **unknown deliveries**, not target failures. Samples from an
older configuration are dropped on reconfiguration. History buckets use panel
receipt time adjusted by sample age against the same node's resource clock;
age outside 0–24h falls back to receipt time. This preserves delayed samples
without trusting the node's absolute clock. The completion time of a cached
result is never refreshed just because another host beat arrived.

`GET /api/admin/nodes/{id}/network-quality?range=24h` returns current results,
history points and a UTC bucket grid (`from`, `to`, `step`); admin/operator access
matches resource history. Public snapshots strip transport queues, hiding history
also masks rolling statistics, and hiding latency removes network results.
Bosun standalone has rolling results only; persistent history belongs to Captain.
Historical quality begins with this upgrade and cannot be reconstructed from old
aggregate latency rows.

## Alert lifecycle and availability (Captain 1.7)

**Monitoring → Alert events** lists ongoing, acknowledged and ended incidents
across the fleet, filterable by node. CPU, memory, disk, monthly traffic (80% and
100%) and offline conditions use the existing settings. A continuing condition
updates one incident, instead of sending another alert every cooldown period.
An operator can acknowledge it; this records the handler and time without
resolving the condition or changing notification policy. Only a valid healthy
measurement resolves a resource incident; missing measurements do not. Disabling
collection or a rule ends the incident as **disabled**, without claiming recovery.
Resource thresholds still evaluate the configured rolling average, not a promise
that every instant in the entire window exceeded the limit.

Migration 57 persists lifecycle records across restarts. An offline incident can
recover on the first new beat even when Captain's in-memory state was lost.
After a restart, new offline detection still waits one configured grace period.
New monitoring history begins with this upgrade: older cooldown timestamps cannot
reconstruct when a historical failure started or recovered.

Each incident requests one opening notification and one recovery notification
through the existing operator Telegram/webhook channels. Delivery remains best
effort, not a durable guaranteed-delivery queue; `notified_at` records an attempted
notification. Telegram batching remains 30 seconds and rechecks suppression before
sending. Disabling collection drops pending batches. Existing `node.alert` webhook
fields remain, with additive `incident_id`, `alert_kind` and `recovered` fields.
The existing offline recovery kind remains `recovered`; resource and traffic
recoveries use `cpu_recovered`, `mem_recovered`, `disk_recovered`,
`traffic80_recovered` or `traffic100_recovered`.

Full administrators can create a global or per-node **maintenance** or **silence**
window, immediately or at a future time. The form uses the browser's local time
zone; APIs store Unix seconds. A window lasts at most 30 days, can start up to a
year ahead, and cannot be backdated to erase already recorded downtime. At most
200 active/scheduled windows are allowed. Notes are private operational metadata.
The management list includes the past seven days plus future windows, displaying
up to 200 with active/scheduled windows first. Cancellation preserves the elapsed
portion of maintenance instead of physically deleting its history.

| Mode | Record incidents | Send notifications | Count time in availability |
| --- | --- | --- | --- |
| Normal | Yes | Opening and recovery | Observed time |
| Silence | Yes | Suppressed | Observed time |
| Maintenance | Yes | Suppressed | Excluded |

An incident first detected under suppression stays unnotified. If still failing
when the window ends, the next evaluation sends its opening notification. A
failure and recovery entirely within a window remain in history without a delayed
recovery notice. Overlapping windows form a union; their time is not deducted twice.
Acknowledgement is separate from both window types. Only full administrators can
create/cancel windows; operators can read them and acknowledge incidents, while
support roles cannot access monitoring management. These writes use the normal
admin operation log.

**Availability** appears in node monitoring detail and when selecting a node in
the alert workspace. It measures contact with Captain according to the configured
offline grace, not whether a proxy core or an end-to-end service worked. The metric
is online seconds divided by observed seconds outside maintenance. **Coverage**
shows the observed fraction of non-maintenance time; unknown time is reported
separately, not converted into uptime or downtime. The time bars distinguish
online, offline, maintenance and unknown periods.

Availability observations use Captain's receipt clock. Contiguous intervals are
stored and adjacent equal states merged. A new panel-process epoch, disabled
collection, or a gap of more than two minutes between observations breaks coverage.
The interval after the latest observation is also unknown until the next one.
A new node and pre-upgrade history therefore begin unknown. The public availability
endpoint requires the page, history and availability sections to be enabled and
honors node visibility. It never returns incident notes or staff identities.

Availability queries are bounded to 90 days; expired intervals are pruned hourly.
Ended incidents and canceled/finished window records are retained for 180 days;
ongoing incidents are retained until they end. Node deletion cascades its records;
site reset clears lifecycle data and observation cursors. This phase runs in Captain
and accepts existing agent beats without a new bosun protocol requirement.

New management APIs:

- `GET /api/admin/monitoring/incidents?state=open&node_id=1&before=100`:
  at most 50 events plus `next` cursor; omit filters as needed. `state` can also
  be `acknowledged` or `resolved`.
- `POST /api/admin/monitoring/incidents/{id}/ack`: acknowledge an open event.
- `GET /api/admin/monitoring/windows`: internal window list and truncation flag.
- `POST /api/admin/settings/monitor-windows`: `kind`, nullable `node_id`,
  `starts_at` (0 means now), `ends_at`, optional `note`.
- `DELETE /api/admin/settings/monitor-windows/{id}`: cancel without deleting history.
- `GET /api/admin/nodes/{id}/availability?range=24h`: summary and UTC time buckets;
  ranges are 1h/24h/7d/14d/30d/90d. Public equivalent:
  `GET /api/probe/nodes/{id}/availability?range=24h`.

## On-demand network diagnostics (Captain 1.7 / bosun 0.56)

Node monitoring details now include **Network diagnostics** for full administrators.
The check originates on the selected node, including an optional source IP for
dedicated-line tests. These checks work independently of periodic collection.

| Check | Result / limit |
|---|---|
| DNS | A/AAAA address resolution, system resolver or an explicit resolver IP/port; 5 seconds, up to 64 addresses |
| TCP | A successful connection is required; refusal is a failure, with DNS/connect timings |
| HTTP | GET response status and completed DNS/connect/TLS/response phases; 200–399 is reachable; no redirects followed |
| Download | HTTP(S) GET, nonempty 2xx body; up to 8 seconds of body transfer or 64 MiB read, plus bounded setup time |
| MTR | Numeric report from the installed `mtr`, five rounds at one-second intervals, at most 20 hops |
| Traceroute | Numeric report from the installed Linux `traceroute`, one probe per hop, at most 20 hops |

Each node permits one diagnostic at a time, with an overall 35-second deadline.
The download result describes this transfer, not the node's total link capacity;
it consumes ordinary node traffic but is not attributed to a proxy customer.
HTTP credentials, custom headers/bodies, arbitrary commands and user-supplied
flags are not accepted. Explicit administrator targets may include loopback and
private networks; proxy-core egress restrictions are unchanged. TLS certificates
are verified and response bodies are discarded, never returned to the panel.

Route checks require optional system tools. Missing tools are reported rather
than installed automatically; containers/minimal systems may also lack the
required networking capabilities. Commands use argument arrays with a previously
resolved literal destination IP, never a shell. Text output is capped at 32 KiB,
control characters are stripped and the UI renders it as escaped text. A completed
route report means the tool exited successfully, not that every hop answered;
intermediate silence alone does not establish destination packet loss. Options
follow the [MTR manual](https://github.com/traviscross/mtr/blob/master/man/mtr.8.in)
and [Linux traceroute manual](https://man7.org/linux/man-pages/man8/traceroute.8.html).

Captain requires an online, paired bosun >= v0.56.0. The existing job envelope
carries `kind: "network_diagnostic"` and parameters `type`, `target`, optional
`source_ip` and optional DNS-only `resolver`. The panel supplies a two-minute
expiry that the node also checks; offline work is not replayed after expiry.
Results use the existing report retry queue. Database errors return a retryable
failure instead of acknowledging lost results; completed results cannot be
overwritten by a replay. Job execution deduplication on the node is in memory:
a node restart before delivery may repeat an unexpired check, within its usual
limits. This is not an exactly-once execution guarantee.

- `POST /api/admin/nodes/{id}/jobs`: queue the typed diagnostic.
- `GET /api/admin/nodes/{id}/jobs/{job}`: poll the existing job shape.
- `GET /api/admin/nodes/{id}/network-diagnostics`: at most 20 checks created
  within the last 24 hours. The hourly cleanup removes older diagnostics.

Creating and reading these jobs requires a full administrator; operator/support
and public-page access are denied. Requests use existing admin audit logging.
No public status API includes targets, command output or diagnostic history.
These records never share cleanup rules with node-removal worker results.

The standalone bosun **Probe** page provides the same checks through
`POST /api/diagnostics/network`. It returns the bounded result directly and
keeps the current result in the page, without persistent history. Authentication
and cross-origin checks apply; managed nodes run diagnostics through Captain.

## Optional GPU monitoring (Captain 1.7 / bosun 0.56)

GPU collection is off by default. Enable it per node in Captain's resource
settings, or in standalone bosun's Probe settings (`probe.resources.gpu: true`).
Both management panels show utilization, used/total VRAM, temperature and power,
with a separate GPU sample time. Unsupported fields remain **—**; no device or
missing drivers produce an explicit unavailable state. This does not install
drivers or tools and does not enumerate GPU processes.

Linux collectors use the fixed, read-only query interface of
[NVIDIA nvidia-smi](https://docs.nvidia.com/deploy/nvidia-smi/index.html) and
[AMD amdgpu sysfs/hwmon](https://docs.kernel.org/gpu/amdgpu/thermal.html).
Individual fields depend on the device and driver; other operating systems and
Intel/Apple GPU telemetry are not implemented. An AMD device without a product
name uses its device ID as the display name. Device UUIDs/PCI addresses are
private management data and are excluded from every public status response.

Collection runs asynchronously at most once every 15 seconds, with one worker,
a two-second command deadline, 64 KiB NVIDIA output and 32 devices maximum.
Even a kernel sysfs read that ignores cancellation cannot stall host beats or
start accumulating workers. A previous reading remains visibly stale; a failed
completed reading has no numeric values. Disabling GPU collection clears the
visible sample and cancels pending work.

Captain's resource history includes GPU utilization, used VRAM and percentage,
temperature and power. GPU epoch/sequence cursors persist with the existing
host counters in the same transaction as history: repeated heartbeat copies of
one GPU sample count only once, including after a panel restart. Missing samples
leave gaps. Display times use receipt time minus sample age on the node's own
clock, so node clock skew and fresh host beats do not refresh old GPU data.
Standalone bosun provides current readings without persistent GPU history.

## Status-page appearance (Captain 1.7)

Monitoring → Collection and status page → Probe provides bundled **Aurora**, **Paper**, **Terminal** and **Glassmorphism** presets,
with a preview, plus **Inherit site theme** for the existing primary color,
radius and font. Choose light, dark or system mode independently, or inherit
the site's scheme. The page background, cards, controls and text now follow
that scheme; system mode follows the visitor's OS preference. Presets work with
both card and compact layouts and the six existing page languages.

The optional `appearance` object on probe settings and `/api/probe` contains
`preset` (`inherit`, `aurora`, `paper`, `terminal`, `glass`) and `scheme` (`inherit`,
`auto`, `light`, `dark`). Older settings clients that omit it preserve the
current appearance. Only bundled options are accepted; themes do not execute
remote code or custom CSS. Existing public-page visibility and section controls
remain authoritative. GPU details are never exposed by a theme choice.

**Glassmorphism** is a React port of the MIT-licensed public Vue frontend from
[Komari Glassmorphism](https://github.com/sanrokamlan-prog/komari-theme-Glassmorphism)
(v3.3.7, commit `06999d5`). Cards, list, background, globe, details and charts
reproduce the upstream components and styles; the port was verified pixel for
pixel against the Vue build. Its visitor theme button can override the
configured default light/dark mode. The two twenty-segment latency/loss bars
show weighted time buckets and open the original monitoring chart on click.
Missing intervals stay grey; complete loss does not become zero latency.
Captain supplies the data through its existing public probe API. No Komari
agent or server is needed. Attribution and adaptation notes are in
`web/probe/glass/README.md`.

Glassmorphism's overview, cards, list, comparisons and node details use
persisted current-period traffic from `traffic.used_up/used_down`. Outbound and
inbound are sent/received bytes on the server interfaces. The overview adds both
directions for each selected node's current period; reset days can differ.
Quota usage still follows each node's configured counting mode (`traffic.used`).
All these counters survive restarts and reset on the configured day or a manual
reset. Offline nodes retain recorded usage; unobserved restart gaps are not
reconstructed. Missing direction fields stay
unknown and never fall back to OS counters. Live rates remain separately gated
by the network section; period totals follow the traffic section. The existing
public `host.net_total_up/down` fields retain their OS-counter meaning. Free-form price notes cannot be converted to billing amounts, so
unsupported monetary summaries display a dash. Invalid resources stay unknown;
section and history controls apply to components and their requests.

Flags use the node's saved **Region code** under **Probe & monthly traffic**
(for example, `JP` for Japan). The grey example is a placeholder, not a saved
value. An empty region hides the flag in both cards and details; the frontend
does not infer it from the node name or IP address.

The optional globe uses bundled textures and the node's public region for an
approximate marker. It does not query external IP geolocation services. Icons
are bundled too; upstream visitor fingerprinting, external exchange-rate
requests and administrative operations are not enabled. The snapshot adds
`appearance_scheme` so inherited light/dark/system mode works on a dedicated
status hostname without exposing `/api/site`. Other presets retain the React
frontend. The selected frontend is served at the existing status URL.

With Glassmorphism selected, enabling carrier monitoring automatically uses the
[three-network variant](https://github.com/vlongx/komari-theme-Glassmorphism-three-network)
for the card/list latency section: Unicom, Telecom and Mobile each have latency
and loss bars. CT/CU/CM are separate identities despite sharing built-in task
ID zero. Missing carrier samples keep their own grey row. Custom carrier points
must retain recognised operator names to use the three-row mode; other names
remain ordinary labelled monitoring data.

With carrier monitoring off, the first enabled monitoring task assigned to the
node (ascending task ID) supplies both latency and loss. The task name appears
in the tooltip. A missing sample does not select a different task. The additive
public node field `ping_tasks` supplies configured IDs and names without targets
or private task settings, and is empty when the latency section is hidden.
The overview retains six default cards: memory, disk, remaining value, cumulative
traffic, upload rate and download rate (subject to public section controls).
Unknown remaining value displays a dash. A local static globe remains available
when WebGL cannot initialize or loses its context.

### Node cost and expiry-linked traffic reset

Node probe settings accept an amount for the complete payment cycle, a currency,
and monthly, quarterly, semiannual, annual or custom cycles. Custom cycles are
positive decimal years (up to four decimal places); amounts are stored as decimal
strings. Blank amounts hide the price, while zero means free. Existing free-text
prices remain editable until explicitly converted. Both public themes display
structured prices without interpreting custom years as a fixed number of days;
this does not enable automatic renewal, currency conversion or remaining-value estimates.

The expiry input is a calendar/date field stored as YYYY-MM-DD, without timezone
conversion or automatic extension. Monthly NIC traffic may reset on a manually
chosen day (1–31), the expiry day-of-month, or its following day. A short month
clamps the anniversary to its last day before applying the optional one-day offset.
For example, an expiry on the 31st with the next-day option resets on March 1 after
February. Payment frequency does not change monthly traffic accounting.

Existing nodes keep their manual reset days. New nodes default to following the
expiry day; before an expiry is supplied they retain the default day-1 fallback.
The editor requires an expiry for automatic mode or an explicit manual choice.
Changing the schedule or expiry preserves accumulated usage and starts using the
new reset boundary; it does not reconstruct historical usage. The current period
label may change to the newly selected boundary. Existing server-local time semantics
for the monthly boundary are unchanged.

The additive `info.billing` and `info.traffic_reset` objects live in the existing
probe-info JSON; no database migration or bosun update is needed for this feature.
Legacy clients that omit these objects preserve them when echoing the price.
An explicit change to the legacy price returns that price to free-text mode.

### Advertised server bandwidth

Node details → Probe and monthly traffic includes **Advertised bandwidth**.
Enter a numeric capacity in Mbps or Gbps (1 Gbps = 1000 Mbps). By default the
same value applies to both directions; enable separate values for asymmetric
links. Inbound/RX is received by the server; outbound/TX is sent. Empty or zero
means unknown. Each direction accepts up to 1,000,000 Mbps.

This is provider capacity metadata, not a speed test or a rate limit. It does
not change bosun, proxy shaping, monthly traffic accounting, or alert thresholds.
Public display is off by default. When enabled, both status-page themes show
the capacity; asymmetric links label RX and TX separately. The global public
Information section must also be enabled. Private bandwidth is stripped from
the public API response, rather than only hidden in the interface.

The optional `info.bandwidth` object stores `inbound_mbps`, `outbound_mbps`, and
`public`. Older API clients omitting it (or sending null) preserve the record;
an explicit empty object or two zero rates clears it. It uses the existing node
metadata storage and requires neither a migration nor a bosun upgrade.
