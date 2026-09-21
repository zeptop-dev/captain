# Compatibility, limits and what 1.x promises

What an operator can rely on across releases, which bosun a Captain works
with, what the database is good for, and what is deliberately not there.

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

## Captain ↔ bosun

A node is always safe to run **newer** than the panel: bosun ignores state
fields it does not know and the panel ignores report fields it does not
know. A node older than the panel keeps working; it just cannot do the
features it has never heard of, which the node page shows as an orange
"vX available" badge.

| Captain | needs bosun | for |
|---|---|---|
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
