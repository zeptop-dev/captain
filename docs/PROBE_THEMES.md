# Komari themes and public probe data

Captain can run an original Komari theme ZIP on a dedicated server-probe
hostname. This is the server monitoring page, not the subscription service
status page. The target contract is Komari **1.6.1 public read APIs**: legacy
REST/WebSocket, JSON-RPC HTTP/WebSocket, public/common methods, and RPC health
and introspection. It does not accept Komari agents or expose Komari account,
command, terminal, plugin, or administration APIs.

## Administrator workflow

1. In **System settings → Monitoring**, enable the server probe and save a
   probe access hostname, for example `probe.example.com`. A subdomain of your
   existing domain can point to the same Captain instance as the main site;
   there is no need to purchase another domain, add a server or deploy another
   instance. Use a different hostname from the main site (changing only the
   port is not sufficient). Point DNS at the existing installation and configure
   HTTPS. If you use a reverse proxy, add this hostname and enable WebSocket
   forwarding. Entering a hostname here does not configure DNS, TLS, or the
   reverse proxy.
2. Under **Komari themes**, upload a release ZIP containing `komari-theme.json`
   and `dist/index.html`. Select the installed version and activate it. Upload
   and activation are separate operations; a failed upload does not replace
   the running version.
3. Expand the theme settings. Captain renders the package's managed settings
   and supplies visible node UUIDs and ping task IDs. Save settings separately
   from activation. Settings are public presentation data, not a place for
   passwords or API credentials.
4. Select **Captain built-in**, then click **Restore built-in theme** to
   restore the bundled probe. Installed
   versions retain their own settings for switching back. An active package
   cannot be deleted.

The same hostname must not also serve the main Captain site. The main-site
probe path continues to use the bundled frontend. Uploaded JavaScript is
third-party software: it can read the published probe data and make requests
according to its own implementation. Captain does not download or execute its
custom administration page; packages declaring raw/redirect administration
settings receive a JSON settings editor instead. Only install themes you trust.

ZIP limits are 32 MiB compressed, 96 MiB expanded, 24 MiB per file and 8,192
entries, with at most 12 installed versions and 160 MiB total. Archives are
validated and stored in SQLite without extracting executable files. Duplicate
paths, traversal, symlinks, corrupt entries and conflicting contents for the
same theme/version are rejected. Existing licensed flag and OS icons provide
compatibility paths when the package does not include its own files. Empty
region icons use a neutral globe instead of inventing a country.

## Private access and origin isolation

The existing public/users/admins visibility setting applies to both API
contracts. A private probe first opens Captain's trusted sign-in/confirmation
page. Captain exchanges that login for a host-bound, read-only probe session;
it never sends the management session, user proxy credential or API token to
the theme. The administrator-only mode requires an authenticated administrator
session. Logout, bans, expiry and privilege changes revoke reads, including
idle WebSockets. The read-only session expires after eight hours and is not
persisted across restarts.

The theme's own Komari administrator/terminal functions are unavailable. For a
public theme whose login button assumes a Komari dashboard, use
`/__captain/login` on the probe hostname to authorize read-only access.

A hostname that has served the probe is permanently reserved as a probe origin
in the installation. Removing it from settings makes it return 404; it does
not expose the management site. A site reset retains this hostname-only safety
registry because browser caches and service workers can outlive settings.
Do not repurpose a theme origin as an administrator origin, including during a
manual database rollback, restore, or replacement of the installation. Use a
clean hostname for administration.

## Captain's native model and extensions

Existing `/api/probe` routes and response shapes remain supported. The new
`/api/probe/v2` snapshot separates inventory, current observations, accepted
sample timestamps, actual billing-period traffic, public storage, location,
costs and ping observations. It uses durable UUIDs; recreating a deleted node
does not inherit its former public identity.

Native routes:

| Route | Purpose |
| --- | --- |
| `GET /api/probe/v2` | Complete public snapshot |
| `GET /api/probe/v2/nodes` | Public inventory and metadata |
| `GET /api/probe/v2/status` | Current observations, period traffic and pings |
| `GET /api/probe/v2/metrics` | Units, retention and query capabilities |
| `POST /api/probe/v2/query` | Bounded metric history query |
| `POST /api/probe/v2/ping-stats` | Attempt-weighted latency/loss summaries |

Komari responses preserve their expected field names and add a `captain`
namespace for the richer values. These fields are optional for an original
theme; its code will not automatically display every Captain extension.

- Unknown/hidden measurements remain null in the native model; unknown
  optional Komari load and ping statistics are omitted according to that wire
  contract. Measured zero is still zero. Nodes that have never reported stay
  in the directory without a fabricated latest report.
- Presence, sample receipt and freshness are separate. A resource heartbeat
  does not refresh unrelated runtime evidence or the NIC metering timestamp.
- `net_total_up/down` retain interface-lifetime semantics.
  `captain.traffic` identifies the actual billing period and its validity.
  These are server traffic figures, not user proxy billing.
- Historical capacity comes from the historical sample, not today's RAM/disk.
- New observations retain sufficient statistics for real extrema/first/last.
  Older average-only history cannot recover those values; unsupported
  aggregations fail explicitly. There is no synthetic historical backfill.
- Ping loss is attempt-weighted; quantiles use merged retained histograms and
  disclose approximation/coverage. Missing evidence is not replaced with zero.
- Hidden/unpaired nodes, disabled public sections, IPs when disabled, private
  device/process details and probe destinations are filtered in the shared
  model. Signing in does not broaden a third-party theme's projection.
- Existing observation-coverage and availability APIs continue to describe
  known versus unobserved time; no theme uptime percentage is a service SLA.

See the [wire contract](../internal/http/komari/README.md) and
[data semantics](../internal/probedata/README.md) for methods, units and limits.
Migration 97 adds identities/statistics, 98 adds theme packages/settings, and
99 adds the origin safety registry. No bosun protocol or billing change is
required. Theme releases can change their APIs independently; support is tested
against explicit package versions, not promised for all future releases.
