# Compatibility, limits and what 1.x promises

What an operator can rely on across releases, which bosun a Captain works
with, what the database is good for, and what is deliberately not there.

Docker web upgrades add optional `host_update` metadata to existing update
responses; API paths and existing fields remain intact. Docker-managed node
upgrades require the new bosun host-updater support (v0.65.0+) and one-time
registration on that node. Older Docker nodes continue to reject in-container
binary replacement. No agent protocol or database migration is required.

## Versioning

Captain follows semantic versioning from 1.0.0 on:

- **patch** (1.0.x): fixes only. No schema change that an older binary
  cannot read back, no endpoint removed, no setting renamed.
- **minor** (1.x.0): new features, new endpoints, new settings, new
  migrations. Existing endpoints, payloads and settings keep working.
- **major**: reserved for a change that breaks one of the contracts below.
  It will come with a written migration path.

Every release lists what changed in [CHANGELOG.md](../CHANGELOG.md), and
the release notes name the bosun version it was tested against.

## How a release is cut

Three rules, each learned the hard way:

1. **The release workflow runs the same gates as CI** — i18n parity,
   oxlint, `gofmt`, `go vet`, `govulncheck`, `go test -race` — and the image
   job waits for the binaries. A red commit cannot become a release. A change
   to the workflow itself is tried first with a manual run (Actions → release
   → Run workflow), which builds the binaries and images and publishes nothing.
2. **A published tag is never moved.** The self-updater compares versions
   only, so a node or panel that already fetched the first build of a tag
   would stay on it for ever. Something wrong in a release is fixed by the
   next patch version.
3. **`make e2e` before a release that touches the node protocol,
   subscription output or the money paths**, against a real panel with real
   nodes and a real client (`scripts/e2e/README.md`). A release that only
   touches the console or the docs does not need it. The run is noted in
   the release notes.

## What is stable in 1.x

| Contract | Promise |
|---|---|
| `POST /api/agent/*` (pair, state, report, beat) | Fields are only **added**. An older agent keeps working: it simply does not send the new ones, and the panel treats them as absent (that is how `traffic_seq`, `inbound` per traffic entry and the doctor report arrived). |
| `/sub/{token}`, `/s/{code}` | The URL shape, the `Subscription-Userinfo` header and the per-client document formats stay. A format may gain fields its client understands. |
| `/api/admin/*` | Existing routes keep their method, path and response shape. New fields may appear; a removed field waits for a major. Read the [role matrix](../README.md#staff-roles-theme-webhooks) for who may call what. |
| Webhook payloads | `X-Captain-Event`, the HMAC-SHA256 `X-Captain-Signature` over the body, and each event's fields are stable. New events and new fields may be added. Current events: `user.registered`, `order.paid`, `ticket.created`, `ticket.replied`, `withdrawal.requested`, `subscription.expiring`, `subscription.traffic`, `user.first_connected`, `user.not_connected`, `user.audit_hit`, `user.audit_banned`, `user.throttled`, `node.alert`. |
| `config.yaml` | Keys are not renamed or removed inside 1.x. Unknown keys are refused at start (`KnownFields`), so a typo never becomes a silent default. |
| Migrations | Only forward. `captain migrate` and every `captain serve` run `Up`; there is no `Down`. Rolling the binary back across a release that added migrations needs the snapshot taken before the upgrade — see [OPERATIONS.md](OPERATIONS.md#rollback). |
| Database file | One Captain process per file. The daily `VACUUM INTO` snapshot is a self-contained SQLite database you can copy, gzip and restore. |

Not covered by any promise: the admin console's internal HTTP calls beyond
the routes above, log line wording, the MCP tool list (it follows the
features), and anything documented as experimental.

## Managed VLESS Reverse (migration 67)

The new `/api/admin/nodes/{id}/reverse-connections` GET/PUT endpoints are
restricted to full administrators. PUT replaces one exit's connection set in
one database transaction and requires the current ID/version map. Existing
node/inbound/entry API shapes stay unchanged; owned resources reject direct
mutation with 409 and must be changed through the wizard.

The additive wire fields are `Inbound.reverse`, `Node.reverse_clients`,
`CoreCapabilities.vless_reverse` and `CoreStatus.reverse` (link ID → nullable
boolean). Capability gating withholds managed inbounds from old agents. A
reverse-only exit starts Xray without requiring a public inbound. The transport
uses the new VLESS reverse fields supported by the pinned Xray 26.3.27; no core
upgrade is required. Publish the bosun capability before deploying Captain
connections. Database migration 67 owns and cascades the dedicated resources;
no existing inbound is converted automatically.

## REALITY target screening (Captain 1.12.1 / bosun 0.61.0)

The reverse wizard uses the existing `reality_scan` job on each transit.
There are no new Captain endpoints, migrations or configuration keys.
The result gains optional `tls_record_bytes`, `tls_record_limit`,
`tls_record_ok` and `reason_code` fields. Older nodes keep scanning with the
previous checks; missing record measurements are shown as **Not checked**.
Older Captain versions can ignore the new fields and still respect the
existing `feasible` result. Neither the reverse wire format nor the pinned
Xray core changes. See [target screening limitations](NODES.md#inbounds).

## NAT / IPLC port mappings (migration 66)

Ingress APIs retain their existing routes and continuous range/offset fields.
Explicit mappings and the node-wide requirement to select an ingress are
additive, and existing records keep the requirement disabled. Omitted/null
new fields on ingress updates retain their saved values. Inbounds and forwards
share validation, and edits that invalidate existing listeners or delete a
used ingress are rejected. Captain resolves forward ingress references to
ordinary listen addresses before sending state, so managed nodes do not need
an agent upgrade. The standalone editor requires the corresponding bosun
update. See [NODES.md](NODES.md#ingresses-and-port-mappings-nat--iplc).

## Account separation (migration 52)

Console and customer accounts now have independent ID namespaces in the same
SQLite file. The migration preserves existing IDs, customer subscription credentials and management
API response shapes stay unchanged. New installations start customer IDs at
1. Staff sign-in to the customer portal is refused; using a subscription
requires a separate customer account. This fixes subscriptions that the old
portal allowed staff to purchase but never provisioned to a node. Existing
staff customer history is preserved as disabled customer records; see
[ADMIN.md](ADMIN.md#staff-roles). No newer bosun is required.

Migration 53 adds an immutable internal node identity, initially equal to
each customer's existing ID. Administrators may explicitly change customer
and staff IDs through the new ID endpoints; scripts addressing those accounts
must then use the new ID. Subscription URLs, proxy credentials and the agent
protocol stay unchanged. Older bosun nodes continue to report the same wire
IDs; Captain maps them to the current customer IDs before billing.

## Captain ↔ bosun

A node is always safe to run **newer** than the panel: bosun ignores state
fields it does not know and the panel ignores report fields it does not
know. A node older than the panel keeps working; it just cannot do the
features it has never heard of, which the node page shows as an orange
"vX available" badge.

| Captain | needs bosun | for |
|---|---|---|
| 1.15.0 | ≥ 0.64.0 | Reviewed core package inventory, download and activation jobs; sing-box Extended and SSH TCP proxy inbounds. Existing inbounds and older agents remain supported. |
| 1.14.1 | ≥ 0.63.0 | Per-inbound private destination access on supported Linux nft sing-box/Xray nodes; runtime capabilities gate both save and state delivery, including individual managed reverse exits. Ordinary inbounds remain compatible with older nodes. |
| 1.12.1 | ≥ 0.61.0 | TLS wire record size screening in REALITY target scans; older agents retain the previous checks and show record size as not checked |
| 1.13.0 | ≥ 0.62.0 | IPv6 nft forwarding, transport-aware forwarding health and node upstream exceptions. Older TCP reports remain readable; old UDP/mixed reports display unknown health. Existing routes, protocols and counters are retained. |
| 1.12 | ≥ 0.60.0 | managed VLESS Reverse, reverse-only exits and per-link status (Xray enabled on both nodes); NAT/IPLC mapping restrictions are resolved by Captain and remain usable with older managed nodes |
| 1.8.0 | >= 0.57.0 | exit diagnostics and immutable durable traffic batches; epoch receipts are additive, legacy reports remain supported. Presets and administration features do not require a node upgrade. |
| 1.7.0 | ≥ 0.56.0 | resource detail, optional GPU, NIC selection, missing-data flags, network-quality attempt batches/timings and on-demand diagnostics; legacy summaries remain supported |
| 1.6.0 | ≥ 0.55.0 | remote node removal: preserve as standalone or uninstall; record-only DELETE remains compatible |
| 1.6.0 | ≥ 0.55.0 | core availability, automatic-selection preview and confirmed running assignments; older nodes retain protocol filtering with unknown availability |
| 1.4 | ≥ 0.53.0 | DStatus active mode (nodes report to the panel under a per-node SID instead of being scraped) |
| 1.3 | ≥ 0.52.0 | the DStatus endpoint (nodes answer a DStatus panel's scrapes as a neko-status agent) |
| 1.2 | ≥ 0.49.0 | port forwards with several targets (failover, weighted round-robin) and per-target health |
| 1.1 | ≥ 0.48.0 | ShadowTLS v3 on Shadowsocks inbounds |
| 1.0 | ≥ 0.47.0 | the cores' own control APIs reachable only by root on the node |
| | ≥ 0.46.1 | traffic batch numbering (exactly-once charging across a report retry or a node restart) |
| | ≥ 0.46 | reports say what window their deltas cover, so the dynamic speed limit cannot mistake a backlog for a burst |
| | ≥ 0.45 | private/loopback destinations rejected in the cores, route and audit rules validated before they reach a config |
| | ≥ 0.44 | audit rules, dynamic speed limit, egress follows ingress |
| | ≥ 0.42 | connection log, core isolation (`cores.user`) |
| | ≥ 0.36 | per-inbound traffic accounting |
| | ≥ 0.30 | baseline: pairing, state pull, reports, forwards, certificates |

`min_version` in both `config.yaml` files is the floor a self-update or
rollback may not go below; set it to the version you have tested.

## Database: SQLite only, and what that is good for

There is **no** Postgres, MySQL or Redis backend, and none is planned for
1.x. `database.driver` takes `sqlite`; anything else is refused at start.
Every cache, rate limiter and lockout counter lives in the one process.
The reasons are measured, not aesthetic.

`internal/store/scale_test.go` builds the real schema and times the hot
paths. On a 2026 laptop SSD, single connection, WAL:

| | 5 000 users, 10 nodes | 50 000 users, 50 nodes |
|---|---|---|
| node report, traffic charged | 100 rows: **6 ms** (p99 9 ms) | 1 000 rows: **85 ms** (p99 112 ms) |
| online devices, one IP each | 2 ms | 30 ms |
| connection log insert | 3 000 rows: 27 ms | 20 000 rows: 325 ms |
| subscription fetch record | < 1 ms | < 1 ms |
| dashboard, under continuous writes | 56 ms | 724 ms |
| user list page, under continuous writes | 12 ms | 160 ms |
| hourly connection-log trim | 49 ms | 4.0 s |
| daily backup (`VACUUM INTO`) | 31 ms | 692 ms |

Reproduce with:

```sh
CAPTAIN_SCALE=1 SCALE_USERS=50000 SCALE_NODES=50 SCALE_ACTIVE_PCT=100 \
  go test ./internal/store/ -run TestScale -v -timeout 40m
```

What that means for the single writer. Each node reports once per
`agent.push_seconds` (60 s by default), so the write time per minute is
roughly *active users × 85 µs*:

- 50 000 active users across 50 nodes ⇒ ~4.3 s of writing per minute, a
  **7 % duty cycle**;
- with the connection log **on** at 20 000 connections per node per
  report, add ~16 s per minute ⇒ ~34 %. That is why it is off by default;
- the writes only queue behind each other, never behind a read, and the
  dashboard figures above were taken with a writer saturating the
  connection — the realistic number is a fraction of that.

Extrapolated, the traffic path alone saturates the connection somewhere
around a few hundred thousand active users. Long before that you would
feel it as console latency, and the answer then is a faster disk or
turning the connection log off, not a second process: **a second Captain
on the same file is not supported** and would queue behind the first.

Size is bounded by design, not by hope. Every table that grows with
traffic has a cap (see [OPERATIONS.md](OPERATIONS.md#what-makes-the-file-grow)).
The connection log — the only one that can grow fast — keeps at most 1 000
rows per user and 7 days; measured at ~91 bytes per row, that is ~450 MB
at 5 000 users and ~4.5 GB at 50 000, worst case, with the feature on.

### Why not Postgres or MySQL

The schema is deliberately portable (no `strftime`, `julianday` or
`IFNULL`), but the code is not: 394 SQL statements with 984 `?`
placeholders (Postgres wants `$1`), 46 migration files in one dialect,
`VACUUM INTO` for backups, and `SetMaxOpenConns(1)` assumed by every
transaction. Porting is a week of work plus a second full test matrix,
and it buys exactly one thing: more than one Captain process. Nobody has
needed that yet. It stays possible — that is why the schema avoids
SQLite-only constructs — and it will be a 2.0 conversation with a real
multi-instance requirement behind it.

### Why not Redis

Sessions, the login lockout, the subscription rate limiter and the
settings caches are all in-process maps. In one process they are faster
than a network round trip and cannot fall out of step. Redis would only
matter with several Captain processes, which is the same conversation as
above, and it would add a daemon to install, secure and back up. The one
thing Redis usually buys — surviving a restart — is not worth it here: a
restart clears a login lockout and a rate-limit window, both of which
refill in minutes.

### Decision: the connection log stays in the main database

It was tempting to give `conn_log` its own SQLite file to keep its writes
off the panel's lock. Measured, it is not worth the cost: the table is
already bounded per user and by retention, the feature is off by default,
and at 50 000 users with it on the writes take ~34 % of one minute — the
console stays usable. A second database file would have to be added to the
backup, the restore procedure and the migration runner, which is exactly
the kind of operational surface you do not widen on the day you cut 1.0.

Revisit if any of these becomes true: the connection log is on for a fleet
where reports plus log writes exceed ~50 % of the report interval; the
hourly trim takes longer than a minute; or the file grows past ~20 GB. The
scale test above is how to check.


### Pending monitoring extension

Captain 1.7.0 accepts existing bosun summaries unchanged.
Per-NIC selection, detailed resources and explicit missing-data flags require
bosun 0.56.0. The additions are `SystemStatus.valid/resources`
and `Probe.resources`; old peers ignore these fields. Migration 54 adds
valid sample counts and per-node monitoring configuration/counter baselines.
Migration 55 adds private resource history and monitoring groups; migration 56
adds network-quality aggregates and persistent attempt cursors. Additive
`PingResult.quality` carries timestamped, sequenced attempts, classified outcomes,
rolling statistics and optional phase timings. Optional
`PingTask.tcp_reachability` preserves automatic line checks; newer nodes also
recognize older Captains’ negative ingress task IDs for that policy. New agents retain the legacy
ping fields. New Captain accepts old nodes without inventing missing quality
measurements; an old Captain ignores the added detail and retains its original
cached-result history semantics. Both peers must be upgraded for attempt-based
history. The new admin read endpoint is
`GET /api/admin/nodes/{id}/network-quality?range=24h`.


Migration 57 adds alert lifecycle records, maintenance/silence windows and
availability intervals on Captain only; it consumes existing beats without new
agent fields. The existing settings/API fields remain compatible, with the
additional public `availability` section. Acknowledgement and window APIs are new;
existing `node.alert` payloads keep their fields and gain incident metadata.

On-demand diagnostics add the `network_diagnostic` job kind using the existing
state/report job envelopes. Captain only queues it for online bosun >= v0.56.0;
older nodes retain all previous functionality. Both ends validate the same typed
request and honor expiry. A retryable report error now covers failed job-result
storage too. Download measurements gain optional `bytes`; legacy readers ignore
it. The new standalone endpoint is `POST /api/diagnostics/network`; it is refused
in managed mode. No existing job kind or management endpoint changes shape.

Optional GPU collection adds `ResourceOptions.gpu` (default false) and
`Resources.gpu` (state, sample epoch/sequence/time and nullable device metrics),
requiring bosun 0.56.0. Older nodes continue without GPU data. The GPU cursor
uses the existing JSON counter column; no additional migration is required.
Device identities and detailed metrics stay private to management APIs.
Probe settings and public snapshots gain an optional `appearance` object for
bundled presets and color scheme. Omitted settings preserve existing values;
this page-only feature has no bosun version requirement.

## Administration additions in 1.8

Migrations 58–64 add traffic receipts, API scopes, bulk result storage and
metadata, staff Passkeys, named subscription profiles/templates, typed preset
libraries and the independent infrastructure-cost ledger. Existing URLs,
response fields and account/traffic identities remain valid. Scopes omitted
from token creation retain the legacy behavior; an explicit empty array denies
all access. Profiles inherit global behavior when no assignment/default exists.

Passkey handles survive staff ID changes; bulk operations use immutable
customer identities to reject reused IDs. Infrastructure history follows staff
renumbering and survives node removal. Site reset clears every new table.
See [administration workflows](ADMIN_WORKFLOWS.md) before rolling back or
changing the configured Passkey origin.

## Per-inbound private access (Captain 1.14 / bosun 0.63)

This adds `Inbound.private_access`, `ReverseClient.private_access` and the
runtime core capability `private_access`; existing fields and paths stay intact.
Use Captain v1.14.1 and bosun v0.63.0 or newer; v1.14.1 fixes replacing
a custom grant when switching modes or clearing optional rule restrictions. Upgrade
bosun first. Ordinary configurations remain usable
with old nodes. Enabled private policies require a recent supporting node
report when saving and are withheld at state delivery if support disappears.
Existing JSON storage carries the field without a schema migration. Omitted
or null policy updates preserve permissions; explicit `mode: off` revokes them.
