# Operating Captain

What to know once Captain is installed: how the database is run, how to back
it up and put it back, how to upgrade and roll back, and what to watch.
Installation itself is in [DEPLOY.md](DEPLOY.md); publishing the panel without
a public IP in [CLOUDFLARE_TUNNEL.md](CLOUDFLARE_TUNNEL.md); driving it from an
AI agent in [MCP.md](MCP.md). Paths below are the binary install
(`/etc/captain/config.yaml`, `/var/lib/captain`); with Docker the data
directory is the `captain-data` volume mounted at `/var/lib/captain`.

## One process, one SQLite file

`internal/db/db.go` opens the database with `journal_mode=WAL`,
`foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL` and
`SetMaxOpenConns(1)`: a single connection, so every query runs behind the
one writer. Consequences:

- Run exactly one `captain serve` per database file. There is no
  active-active or hot-standby mode: a second process on the same file would
  queue behind the first (up to the 5 s busy timeout, then fail), and the
  in-memory state (node state cache, login lockout, allow-list cache, update
  check cache) is per process. Scale by giving the process a faster disk,
  not by adding processes.
- `database.driver` takes `sqlite`; anything else is refused at start.
  There is no Postgres or MySQL backend, and no Redis: every cache and
  counter lives in the one process. [COMPATIBILITY.md](COMPATIBILITY.md)
  has the measured write times and the size this is good for.
- Migrations (`migrations/*.sql`, goose) are applied on every start of
  `captain serve` and by `captain migrate`. Only the `Up` direction is ever
  run.

Files under the data directory (`data_dir`, default `/var/lib/captain`):

| path | what | in the backup? |
|---|---|---|
| `captain.db` (+ `captain.db-wal`, `captain.db-shm` while running) | everything: users, orders, nodes, settings, panel-issued certificates, probe history | yes |
| `backups/` | daily snapshots `captain-YYYY-MM-DD.db` | is the backup |
| `certs/` | certmagic store for the panel's own HTTPS (`tls.auto`) | no, re-obtained |
| `certs-issued/` | ACME account for certificates issued from Domains & certs | no, re-created |
| `site/` | your own landing page, if you dropped one in | no |

Secrets outside the database: `/etc/captain/config.yaml` (payment keys,
Cloudflare token). Keep a copy of it with the snapshots.

Logs go to stderr as `log/slog` text lines (`journalctl -u captain` or
`docker compose logs -f captain`). `log_level: info` (default) logs every
request that ends in a 4xx/5xx or takes longer than a second with a request
id (also echoed as `X-Request-ID`); `debug` logs every request.

## What makes the file grow

Most tables grow with the number of users. Three grow with *traffic*, and
they are the ones to watch:

| table | written by | bounded by |
|---|---|---|
| `conn_log` | every accepted connection, when the connection log is on | retention days (7) **and** rows per user (1000), both in Settings → Connection log |
| `audit_log` | every audit-rule hit | 90 days, one hit per user and rule per minute on the node |
| `sub_requests` | every subscription fetch | 30 days and the newest 200 rows per user |
| `hwid_devices` | every device that fetches with `x-hwid` | 64 per user, and devices unseen for 90 days are forgotten |

The connection log is off by default for that reason: with it on, a busy
node writes a row per connection, which on a few thousand users is millions
of rows a day. Both of its limits are enforced hourly, in batches of 20 000
rows so the single database connection is never held for long.

SQLite does not return freed pages to the filesystem, so the file does not
shrink after a cleanup: it is reused for new rows. A real shrink needs
`VACUUM`, which rewrites the whole file and blocks the panel while it runs
— take the downtime deliberately, or restore a snapshot (the daily backup
is a `VACUUM INTO` copy and is already compact).

## Backups

Captain snapshots its database once a day with SQLite's `VACUUM INTO`, so
the copy is consistent while the panel keeps serving and is a self-contained
file without a WAL. `internal/backup.Manager` runs from the job tick:

- the day's snapshot is taken once the configured hour has passed, as
  `<data_dir>/backups/captain-YYYY-MM-DD.db` (a second run on the same day
  gets a `-HHMMSS` suffix); the newest *keep* files (default 7) are kept;
- Settings → Database backups sets the hour and retention, adds a remote
  (WebDAV with basic auth, or an S3-compatible bucket: endpoint, region,
  bucket, prefix, keys, path-style for MinIO and some R2/B2 setups) that
  receives each snapshot gzipped as `captain-<date>.db.gz` with its own
  retention, tests the remote, runs a backup now and downloads local
  copies (`POST /api/admin/settings/backup/run`,
  `GET /api/admin/settings/backup/files/{name}`);
- remote copies can be **encrypted** with [age](https://age-encryption.org)
  before they leave the host (Settings → Database backups → Encrypt remote
  copies), because the database holds password hashes, TOTP secrets,
  payment gateway keys, node secrets and every subscription token. With a
  *public key* (the console generates a pair and shows the private key
  once; the panel keeps only the public half) Captain can seal its remote
  copies but not open them. A *passphrase* is simpler and is stored on the
  panel, so it protects the bucket, not the host. Sealed copies are named
  `captain-<date>.db.gz.age`; local snapshots stay plain, like the live
  database next to them;
- the outcome (time, file, error, remote name) is stored under the
  `backup_status` setting and shown on the card; a failure logs
  `backup failed` with `component=backup`.

Copy the snapshots off the machine (Docker: `docker run --rm -v
captain_captain-data:/d -v $PWD:/out alpine sh -c 'cp /d/backups/*.db
/out/'`), or let the remote do it. Do not copy `captain.db` itself while the
panel runs: the WAL may hold pages the main file does not, and a copy taken
mid-write can be inconsistent. The snapshots exist for that.

## Restore

1. Stop Captain: `systemctl stop captain`, or `docker compose stop captain`.
2. In the data directory move `captain.db`, `captain.db-wal` and
   `captain.db-shm` aside. Never leave an old `-wal`/`-shm` pair next to a
   restored file, and never bring the `-wal`/`-shm` files of a live copy
   along: SQLite would replay that WAL into the restored database.
3. Put the snapshot in place as `captain.db`. A copy from the remote is
   gzipped, and sealed when encryption is on; `captain backup open` does
   both steps and refuses to overwrite an existing file:

   ```sh
   captain backup open -identity captain-backup-key.txt -o captain.db captain-2026-09-19.db.gz.age
   CAPTAIN_BACKUP_PASS='…' captain backup open -passphrase-env CAPTAIN_BACKUP_PASS -o captain.db FILE
   ```

   Without the binary: `age -d -i captain-backup-key.txt FILE | gunzip >
   captain.db` (plain copies: `gunzip`). A snapshot from `VACUUM INTO` has
   no WAL; Captain creates fresh `-wal`/`-shm` files on start.
4. Make sure the service user owns it (`chown captain:captain` for the
   binary install; uid 1000 inside the container).
5. Start Captain. A snapshot older than the binary is fine: the missing
   migrations are applied on start. A snapshot from a *newer* Captain than
   the binary is not (see rollback below): upgrade the binary first.
6. Check `GET /api/health`, log in, and open Nodes: node tokens live in the
   database, so paired nodes reconnect on their own and pull the restored
   state on their next poll (`last_seen_at` moves within a minute).

Anything that happened between the snapshot and the restore (orders,
sign-ups, traffic counters) is gone; the nodes' own counters are already
zeroed after each report, so that traffic is not re-charged.

**This procedure is exercised, not assumed.** Last drill 2026-09-18: the
daily snapshot taken before the 1.0 upgrade (schema version 42) was
restored into an empty data directory and started with the current binary.
Migrations 43–48 applied on start, the accounts, nodes, plans and orders
came back, and the restored panel served the test user's subscription to
mihomo, sing-box and v2rayN with the correct `Subscription-Userinfo`. Two
things that drill is worth repeating for: an *older* snapshot on a *newer*
binary is the normal case and it works, and make sure nothing else is
already listening on the port you start the restored panel on — otherwise
you will be testing the wrong process.

## Upgrade

Settings → Version and updates (`GET /api/admin/system/update`; the release
check is cached 20 minutes, `?force=1` re-checks; a red dot on the version
badge means a newer release exists).

- **In place** (binary install): `POST /api/admin/system/update/apply` with
  `{"version": ""}` for the latest or a tag newer than the running one
  (drafts, pre-releases and older tags are refused; going back is what
  rollback is for). Captain downloads
  `captain-linux-<arch>` from the GitHub Release, verifies it against
  `SHA256SUMS`, swaps the binary atomically keeping the old one as
  `captain.backup` next to it, answers `{"restarting": true}` and exits
  half a second later; systemd (`Restart=always`, `RestartSec=3`) starts the
  new version, which applies any new migrations on start. The binary's
  directory must be writable by the service user (`/opt/captain` owned by
  `captain` in DEPLOY.md) and the build must be a release build: development
  builds refuse (HTTP 502 with the reason).
- **Docker**: the same call is refused with HTTP 409 (`running in a
  container: pull the new image instead`); do
  `docker compose pull && docker compose up -d`.
- **By hand**: replace the binary as in DEPLOY.md step 1 and
  `systemctl restart captain` (or `POST /api/admin/system/restart`, which
  exits the process for systemd to restart).

Take a backup right before upgrading (run-now on the Backups card): it is
the only way back across a release that added migrations. Nodes are separate:
an orange "vX available" badge on Nodes, `POST /api/admin/nodes/{id}/upgrade`
or `POST /api/admin/nodes/upgrade-all` sets `upgrade_to`, which the node gets
in the answer to its next report and applies itself.

## Rollback

`POST /api/admin/system/update/rollback` (Settings → "Roll back") puts
`captain.backup` back and restarts, again not inside a container. That
restores the binary only. Captain never runs goose `Down` migrations
(`db.Migrate` calls `goose.UpContext`; `captain migrate` does the same), so
the database stays at the newer schema. An older binary started on a
database with newer migrations sees tables and columns it does not know
about and may fail or misbehave. Therefore, when the release you are leaving
added files under `migrations/`:

1. stop Captain;
2. restore the snapshot taken before the upgrade (procedure above);
3. put the old binary back (rollback endpoint before stopping, or copy
   `captain.backup` over the binary, or download the old tag);
4. start.

Everything written between the upgrade and the rollback is lost, which is
why the pre-upgrade snapshot matters. If the release added no migrations
(compare `ls migrations/` between the two tags), rolling the binary back
is enough.

### Rolling a node back

Nodes → the ↶ icon next to a node's version queues a `rollback` job: bosun
puts its previous binary (`bosun.backup`) back, reports the version it landed
on and restarts; the node list shows the old version within a minute. Only
one step back is kept, so a second rollback does nothing. Bring the node
forward again with "upgrade".

## What to monitor

- **Liveness**: `GET /api/health` answers `{"ok":true,"time":<unix>}` without
  auth. It says the process is up, nothing more.
- **Self-check** (dashboard, admins only; `GET /api/admin/system/selfcheck`):
  the panel lists its own gaps — no snapshot in 26 hours, backups that
  never leave the host or leave it unencrypted, the heartbeat off or
  failing, no mail provider, staff accounts without two-factor login, a
  newer release, under 1 GiB free where the database lives, nodes silent
  for five minutes, the panel's public certificate within 14 days of
  expiry — each with a link to where it is fixed.
- **Metrics**: `GET /api/admin/metrics` serves Prometheus text. It sits
  behind the admin auth, so scrape it with a personal API token
  (Settings → API tokens & MCP; `Authorization: Bearer cap_...`) from an
  address on the admin allow-list. Gauges: `captain_nodes`,
  `captain_nodes_online` (reported within three push intervals),
  `captain_node_last_seen_seconds{node}`, `captain_users_active`,
  `captain_subscriptions_usable`, `captain_subscriptions_expiring_7d`,
  `captain_orders_pending`, `captain_backup_last_success_timestamp_seconds`,
  `captain_backup_last_error`. Counters:
  `captain_payment_callback_errors_total`,
  `captain_payment_settle_errors_total`, `captain_job_errors_total{job}`,
  `captain_node_reports_total`, `captain_node_state_builds_total`.
  Alert on `captain_node_last_seen_seconds` growing past a few push
  intervals (`agent.push_seconds`, default 60), on the backup timestamp
  falling more than a day behind, and on any increase of the error counters.
- **Nodes** without Prometheus: `GET /api/admin/nodes` lists each node with
  `last_seen_at`, `online` (true when it reported within the last three
  minutes), `version`, core states and certificate warnings. With the probe
  page on (Settings → Probe), offline nodes (after the grace period),
  sustained CPU/memory/disk and monthly traffic thresholds raise notices
  through Telegram, mail and webhooks by themselves.
- **Log lines worth an alert** (exact message text):
  - `payment callback rejected` (warn, `gateway`, `err`): a gateway
    notification failed signature or amount verification;
  - `settle failed` (error, `order`): a verified payment that could not be
    applied to the order;
  - `<job> failed` with `component=jobs`, e.g. `expired subscriptions
    failed`, `reset quotas failed`: a housekeeping step erroring every
    minute;
  - `backup failed` (`component=backup`) and `certificate renewal failed`
    (`component=certs`, `domain`);
  - `traffic samples for users this node does not serve were dropped`
    (warn, `node`): a node reported accounting for users it is not
    provisioned with;
  - `webhook failed`, `build state`, `panic`.
- **Certificates**: the node list shows a red TLS badge when a node-side
  certificate failed or expires within two weeks; panel-issued certificates
  are renewed 30 days ahead and the first failed renewal notifies the admin.

## Admin access, allow-list and client addresses

- Sessions last 30 days; the admin console needs the admin login (password,
  plus a TOTP code once enabled), and only sessions minted there reach
  `/api/admin`. Staff API tokens (`cap_...`) carry their owner's role,
  narrowed by the token's scope (read-only answers GET only) and its
  optional expiry; no token can manage staff accounts, and `/mcp` is behind
  the same allow-list as `/api/admin`.
- Five failed logins from one address within 15 minutes lock that address
  for 15 minutes (`internal/http/ratelimit`). The counter lives in memory:
  a restart clears it.
- Settings → Security → *Admin allow-list* (`admin_allow_cidrs`: bare
  addresses or CIDRs) restricts the admin login and every `/api/admin`
  route (and `/mcp`), API tokens included, so a metrics scraper must be on
  it too.
  Saving a list that would exclude your own address is refused; the list is
  cached for 30 s. The portal, subscriptions, node and payment endpoints are
  not affected.
- **Which address is "yours"**: `ratelimit.ClientIP` uses the TCP peer
  address unless the peer is a trusted proxy. `trusted_proxies` in
  config.yaml lists those proxies (addresses or CIDRs); when it is empty,
  any loopback or private peer (RFC 1918, IPv6 ULA) counts as one, which
  covers Caddy, nginx, 1Panel or cloudflared on the same host or compose
  network. Behind a trusted proxy the address comes from
  `X-Forwarded-For`, read from the right: every proxy appends the peer it
  saw, so the rightmost entry that is not itself a trusted proxy is the
  client and everything left of it is whatever the client sent.
  `X-Real-IP` is only used when no `X-Forwarded-For` entry is usable,
  because a plain `reverse_proxy` in Caddy (and cloudflared) passes a
  client-supplied `X-Real-IP` through untouched while it does rewrite
  `X-Forwarded-For`. Consequences:
  - the proxy has to append to `X-Forwarded-For` (all of the bundled
    layouts do), otherwise everything appears to come from the proxy's
    address and one locked address locks everyone out (and an allow-list
    with only your public address blocks the console);
  - with two proxy layers (Cloudflare in front of nginx, say) name both in
    `trusted_proxies` — the rightmost untrusted entry is then the visitor,
    not the outer proxy;
  - with `trusted_proxies` empty, any client that reaches Captain from a
    loopback or private address (a VPN peer, another container, a LAN
    host) is treated as a proxy and can claim any address in the headers
    it sends. Keep `listen` on `127.0.0.1:8080` behind the proxy, or name
    the proxy in `trusted_proxies` so nothing else is believed. Public
    peers are never trusted, so a client on the internet cannot spoof its
    way past the allow-list.
  Behind Cloudflare Tunnel the connection comes from cloudflared on
  localhost and it forwards the visitor's address, so the real address is
  used; details in [CLOUDFLARE_TUNNEL.md](CLOUDFLARE_TUNNEL.md).
- Node pairing (`POST /api/agent/pair`) is throttled per address the same
  way as logins, so a leaked panel URL cannot be used to guess pair codes
  quickly.

## Related documents

- [DEPLOY.md](DEPLOY.md): installer, Docker and binary layouts, HTTPS
  (`tls.auto`, DNS-01 wildcard), subscription hosts, node certificates.
- [CLOUDFLARE_TUNNEL.md](CLOUDFLARE_TUNNEL.md): running the panel (and a
  bosun panel) behind cloudflared with no open ports; the 100 s request
  limit and the node long-poll (`/api/agent/state?wait=`, capped at 50 s).
- [MCP.md](MCP.md): the Model Context Protocol server at `POST /mcp`, its
  tools and the `confirm: true` guard on writes.
- [ARCHITECTURE.md](ARCHITECTURE.md): packages, domain model, agent
  protocol, jobs.
- [COMPATIBILITY.md](COMPATIBILITY.md): what a release promises, the
  Captain ↔ bosun version matrix, and the measured database ceiling
  (why SQLite, and no Postgres, MySQL or Redis).
