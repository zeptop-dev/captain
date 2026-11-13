# Compatibility, limits and what 1.x promises

What an operator can rely on across releases, which bosun a Captain works
with, what the database is good for, and what is deliberately not there.

For the v1.48 features, use v1.48.1 or later: the unchanged v1.48.0 tag has no
official binaries or images because its release tests reached the previous
time limit. The v1.48 application compatibility is unchanged; the current rollback floor is v1.63.0 to preserve ingress address sources, subscription bindings and strict subscription-node IP policies and the earlier server-plan/interface-binding protections.

## Operations insights (unreleased, migration 121)

Additive private route events, opt-in anomaly policies, exact-type silences,
infrastructure term snapshots and additional-expense history preserve existing
node, payment and subscription data. New storage, route-watch, network-alerts
and infrastructure analytics capabilities have explicit narrow-token scopes.
The line guide uses the existing preview/save APIs. These insights introduce
no installed-agent protocol requirement or additional rollback-floor change.
The integrated shared bosun pin and next-release rollback floor follow the DNS
following modes section below (v1.66.0); existing installed-agent behavior is
unchanged by the insights themselves.
See [operator behavior and limits](OPERATIONS_INSIGHTS.md).

## Migration notice dismissal (Captain v1.65.0)

An additive administrator POST endpoint saves a site-wide dismissal for the exact
plan-access conversion receipt. The optional conversion.dismissed field reports
visibility; history and permission revisions remain unchanged. No schema or agent
change is needed; schema120, shared bosun0.95.0 and rollback floor1.63.0 remain.
An older panel may show the notice again but cannot lose grants through dismissal.
See [conversion notice](PLAN_ACCESS.md#dismissing-the-conversion-notice).

## Plan names (Captain v1.64.0, migration 120)

New or renamed plans require unique names, including disabled plans; existing
historical duplicates can still receive unrelated edits. Independent owned groups
use plan names without random suffixes and rename transactionally with their plan.
Migration120 normalizes only unambiguous names; conflicting shared names, all IDs,
grants, subscriptions and migration receipts remain intact. Existing API paths
and payloads remain stable; name conflicts return409. No agent change is needed.
See [name handling](PLAN_ACCESS.md#names-captain-v1640-migration-120).

## Plan node selection (Captain v1.63.0, migration 119)

Node editors show read-only authorization. Plans select individual inbounds,
snapshot a category or explicitly follow its future membership. Migration 119
atomically expands old all-users grants to existing plans, including disabled
items, with an immutable conversion receipt. Existing subscriptions, account
extra grants, explicit restrictions and external-node provenance remain intact.
The built-in rollback floor is v1.63.0. New admin UI creation opts into unassigned
inbounds while legacy APIs retain their 1.x semantics. No agent protocol change
is needed. See [plan access](PLAN_ACCESS.md) for scope and shared-inbound boundaries.

## Plan-first access (Captain v1.62.0)

Migration 118 adds plan-owned independent permissions while retaining legacy groups
as explicit reusable schemes. Existing plan, subscription and account grants are not
rewritten. Plan POST/PATCH has optional Access drafts with node-access authorization
and an atomic revision check. The rollback floor becomes v1.62.0 to
protect ownership and retained external-node provenance; no agent changes are
needed. See [plan access](PLAN_ACCESS.md).

## Account presentation and mail tools (Captain v1.61.0)

Optional account information adds isolated select/REJECT groups only to supported
Clash/Mihomo and Stash documents. Existing legacy info lines and other formats are
preserved. Mail templates use a separate settings document; older mail-setting
writes cannot remove them. Plan prices retain the cents API, while city shortcuts
only populate explicit display drafts. No schema, agent protocol, proxy core or
installed-agent change is required. Schema stays 117, formal bosun stays v0.93.0,
and the built-in Captain rollback floor stays v1.60.0.

## NAT address inheritance (Captain v1.60.0, migration 117)

Ingress `entry_address_source` and Entry `FollowIngress` are additive. Existing
rows remain custom/fixed; server address inheritance and subscription binding
are separate explicit choices. Old clients preserve unchanged bindings and
explicit endpoint overrides detach them. Ingress DNS retry state is persisted
but excluded from topology preview revisions. See
[NAT address inheritance](INGRESS_ADDRESS_SOURCE.md) for API contracts and
transaction boundaries. The built-in rollback floor is v1.60.0, since older
panels ignore the new bindings.
No new agent fields or bosun version gate are needed.

## Per-entry connection IP policy (Captain v1.59.0 / shared bosun v0.93.0)

Migration 116 adds an optional `Entry.IPVersion`. Existing rows and omitted
create fields retain the empty legacy value; update omission preserves the
saved policy. No agent protocol or listener changes are required. Unsupported
client formats do not receive forced-family hostname entries. See
[connection IP version](ENTRY_IP_VERSION.md) for template conflicts, client
chains, atomic variant creation, and downgrade restrictions. Captain pins the
formal bosun v0.93.0 library; installed agents require no new state fields or
runtime behavior. Captain's built-in rollback floor is v1.59.0 because older
panels silently ignore this connection policy.

## Standard sing-box security packages (Captain v1.57.1 / bosun v0.91.3)

The shared catalog includes a compatible `1.14.1-r3` rebuild using Go 1.26.10
and reviewed dependency security updates. It is the tested default; affected
`1.14.1-r1` and `1.14.1-r2` keep their immutable identities and are marked broken.
Previously selected exact packages remain selected until an administrator
changes them. Installing a new bosun version alone does not prove that its
running proxy cores have changed.

The newer `1.14.4-r1` package is caution and requires explicit activation.
Its upstream parser rejects repeated members in outbound groups that older
versions accepted. Captain keeps the existing routing API contract, including
unrelated edits to these historical configurations. Correct incompatible
groups and use core management's candidate configuration preflight before
switching; a downloaded package is not an active package. A failed candidate
preflight preserves the running core.

Captain v1.57.1 uses the formal bosun v0.91.3 module to serve these package
identities through Captain delivery. Existing agent protocol fields, schema
113 and rollback floors are unchanged. Older panels do not gain the new
download catalog until upgraded. When nodes use Captain delivery, upgrade the
panel first so an unpinned node can fetch the new compatible default when it
restarts. Nodes using direct GitHub downloads do not depend on the panel's
catalog for that fetch.

## Native network dependencies (Captain v1.57.0 / bosun v0.91.2)

This release also recognizes the reviewed Extended r4 core package and GeoCheck
0.3.0-r2 diagnostic identity. During mixed-version upgrades using Captain
delivery, either an old panel or an old agent may request a different tool
identity. The optional GeoCheck download can remain unavailable until both
components are upgraded; normal reporting and proxy service remain independent
of that tool. The affected r1 helper is not restored as a compatibility fallback.

Captain v1.57.0 uses the formal bosun v0.91.2 shared module and exposes a
separate OS-package choice for full native server installation. Existing
Captain/GitHub defaults and unchanged legacy install commands remain valid.
Explicit choices require the fetched installer's exact
`system-packages-v1` feature marker; unsupported choices stop before the old
installer executes. This is an installation option, not a new agent protocol
field or persisted configuration key. Docker, monitor-only and OpenWrt setup
retain their existing behavior.

bosun v0.91.2 adds bounded runtime recovery after fresh authorized control pulls,
preserves requested user limits, and distinguishes missing tools, unavailable
kernel enforcement and partial application in Doctor. Installing packages does
not supply capabilities withheld by an LXC host or prove measured throughput.
Older bosun agents remain compatible but do not gain the new recovery behavior.
No migration is added (schema 113). Existing rollback floors remain unchanged.

## Per-server carrier probe plans (Captain v1.56.0)

Migration 113 stores independent per-server carrier plans and node-bound edit
revisions. Existing and newly created servers inherit the global default;
custom and disabled modes preserve the independent draft. Global monitoring
remains the master collection switch, while the global carrier toggle is only
the inherited default. Resource collection and separately assigned ping tasks
keep their existing controls. Older metadata editors preserve the new plan.

Agent delivery uses the existing carrier-probe configuration. Older compatible
agents can execute the selected plan without a new version requirement. The
additive bosun v0.90.0 `window_started_at` report field lets Captain establish whether all
samples in a rolling window follow the current plan. After a plan change, agents
without that field retain their fresh latest latency while the unverified
rolling statistics remain unknown. No historical samples are relabeled as a
new protocol, family, or target. Historical samples keep their original times;
selecting the same exact target again retains its earlier history without
reviving old current-state or rolling-window evidence.

Older Captain binaries ignore server overrides and return to their global
selection, including re-enabling probes disabled for one server. Captain
v1.56.0 therefore raises the built-in rollback floor to v1.56.0. Higher
configured floors remain effective. Manual downgrade requires restoring a
compatible backup and explicitly checking the resulting probe configuration.

## Public IP fallback (Captain v1.56.0 / bosun v0.90.0)

### Explicit fallback following (Captain v1.58.0 / bosun v0.92.0)

Captain v1.58.0 adds per-family `fallback_auto_ipv4` and
`fallback_auto_ipv6` settings to the existing node address API, defaulting to
false. Omitted fields preserve the stored choice. Migration 114 stores policy
boundaries; fallback candidates remain separate from the original dual-source
address fields. Existing address and configuration keys retain their meanings.

bosun v0.92.0 reports add `check_status` and `run_id` to the existing optional
fallback object. Only fresh `no_conflict` observations in the same uninterrupted
run can qualify. Old agents without these fields still display their fallback
observations but cannot use the new automatic-follow option. Old Captain versions
ignore the evidence and do not promote a single-source result into dual-source
confirmation; downgrading stops fallback following. Normal dual-source following
and proxy traffic continue using the established protocol.

The following describes the originally released observation-only behavior:

bosun v0.90.0 can anonymously query the configured Captain HTTPS origin,
including its port, when the normal independent public-IP sources fail. A
Cloudflare trace result is separate single-source advisory evidence. It never
replaces the authoritative address, changes an entry, or updates DNS. No new
option is required; direct origins without that endpoint remain unknown.
Older panels ignore these additive fields and older agents keep their existing
address detection. No proxy-core version or bosun rollback-floor change is
required.

## Bilingual display groups and categories (Captain v1.56.0)

Migration 112 adds optional Chinese/English names to monitoring groups and
subscription-node categories. Existing keys, IDs and legacy name fields remain
intact. `NodeLayout.groups` and node membership still use stable keys; the
optional `group_names` map carries `{zh,en}` display names. Older clients that
omit it preserve translations for unchanged keys. Localized category fields
are additive; updates that omit them preserve existing translations.
Explicit bilingual category edits refresh the compatibility `Name` from the
Chinese name, or English when Chinese is absent, retaining its existing
case-insensitive uniqueness rule. Category IDs never change with display names.

Captain's interfaces choose the Chinese name for Chinese locales and English
for other locales, falling back to the other name and then the legacy label.
Translation changes never modify access, billing, subscription output, ordering
or agent configuration. Public metadata is attached only to already-visible
objects. No bosun upgrade is required. Older Captain binaries do not understand
the extra names and may discard group translations when rewriting layouts;
keep a compatible backup before manually downgrading. No new built-in rollback
floor is introduced for this optional presentation metadata.

## Interface-bound probes (Captain v1.54.0 / bosun v0.89.0)

Migration 111 adds optional `source_interface` to custom ICMP/TCP tasks.
Existing tasks keep system routing. Bound tasks require explicit server selection
and literal IPv4/IPv6 targets; TCP requires an IP and numeric port. The device
name identifies a Linux interface, not an OpenWrt logical network. Binding does
not bypass firewall, policy-routing or transparent-proxy rules.

Only agents reporting `probe_interface_version >= 1` receive these tasks, in the
new `interface_tasks` list. Older agents ignore that entire list, including after
a downgrade with a stale capability report, instead of probing an unbound path.
Measurements carry a definition digest, so changing interface, protocol or target
does not relabel previous samples. Public views do not disclose the interface
name or target address.

The built-in Captain rollback floor becomes v1.54.0: older panels ignore the new
database column and would emit the task in the legacy unbound list.
Manual rollback to an older panel requires a compatible backup; the
updater cannot protect manual executable replacement. Higher configured floors
remain effective.

## Complete-path transports and backends (Captain v1.53.0 / bosun v0.88.0)

Complete routes derive TCP, UDP or same-port both from their final inbound.
Optional `backend` fields on direct template steps and `entry_backend` select
relay, realm or nft. Fixed references preserve their selected versions; old
omitted fields retain native relay configuration identity. Omitted/null edits
preserve stored backend intent. No agent protocol field or database migration
is required. New UDP/both or external-backend routes require bosun v0.88.0 on
every forwarding server; existing native TCP routes retain their earlier gates.

Multiple candidates require native relay; UDP/both uses new-session round-robin
without UDP health failover. Existing handshake health is only valid for
all-native TCP paths. realm uses one target per path hop, and changes may restart
all realm listeners on a server. nft preflight failure preserves the old whole
table, but a successful receipt does not confirm conntrack expiry. Native relay
changes do not drain old sessions. See [release boundaries](LINE_RELEASES.md).

New path release snapshots use `path-forward-v2`; legacy `path-relay-v1` plans
remain accessible under their original permissions. Captain's rollback floor
is v1.53.0, since older panels cannot preserve these intents. bosun's floor stays
v0.85.0; installing an older agent does not grant it the new path eligibility.

## Captain-managed Agent HTTPS entry (Captain v1.55.0)

An opt-in second HTTPS listener reuses existing Agent routes and database
certificates. There is no new bosun protocol requirement, config.yaml key or
database schema migration. Captain downloads still require bosun v0.89.0.
Settings are disabled by default; existing installation origins and running
agents remain unchanged. A supporting Captain build is required while agents
depend on this listener: older binaries ignore its settings and cannot serve
its port. The existing update floor is unchanged; do not manually downgrade
a deployment that still relies on this entry. See [deployment and certificate
lifecycle](AGENT_DELIVERY.md#captain-managed-agent-https-entry).

The Captain v1.56.0 access diagnostic and host firewall helper add no Agent protocol
or schema requirement. Existing nodes keep working. The new installer preflight
is a script change: Captain must embed the supporting bosun bootstrap dependency
when released; it is not provided by the v0.89.0 module. Compatibility fallback
supports older main listeners. No rollback floor is raised by these diagnostics.

## Agent connection and Captain downloads (Captain v1.54.0 / bosun v0.89.0)

Migration 110 adds optional per-server installation origins and download sources.
Existing agents keep their stored endpoint; an admin edit does not switch an
online connection. Captain delivery requires bosun v0.89.0 or later and a panel
with the new authenticated download routes. Native installation, upgrades and
catalog-managed prebuilt cores use the same endpoint without a direct-download
fallback. System packages and Docker images are outside this delivery path.
See [Agent connection and downloads](AGENT_DELIVERY.md) for proxy setup, checksum
provenance, cache limits and downgrade constraints.

## Activated configuration evidence and operation history (Captain v1.52.0 / bosun v0.87.0)

Supporting agents add optional `rendered_config` alongside independently numbered
configuration receipts. Older agents remain compatible and show unavailable
artifact evidence. Invalid optional evidence cannot reject valid accounting.
Migration 108 adds bounded delivery-linked observations; administrator tokens
need both `rendered-config:read` and node reading permission. This does not change
configuration identity, deployment behavior or the existing rollback floor.
Upgrade request history starts at migration 109; existing slots are not reconstructed.
The independent `operations:read` scope still requires each source object’s original
read permissions. See [scope and privacy](RENDERED_CONFIG.md) and [operation history](OPERATION_HISTORY.md).

## Live diagnostics and traffic provenance (Captain v1.51.0 / bosun v0.86.0)

Live diagnostics require a freshly challenged supporting node and an explicit
administrator viewing lease. The additive State control is temporary transport
metadata: it does not enter configuration revisions, receipts or offline
snapshots. Older nodes keep serving but cannot provide live samples. Normal
monitoring and public probe permissions remain separate.

Optional traffic-source metadata is frozen with each original report batch.
Malformed metadata becomes an unknown observation without rejecting otherwise
valid customer accounting. Migration 107 stores bounded private history;
permanent receipt cursors, existing UTC accounting days and subscription
selection are unchanged. Legacy batches remain distinguishable. API tokens need
the independent `node-diagnostics` or `traffic-history` scope and node-read
permission; only full administrators may use either feature.

See [live diagnostics](NODE_DIAGNOSTICS.md) and [traffic history](TRAFFIC_HISTORY.md)
for freshness and retention limits. Built-in rollback floors remain Captain
v1.50.0 and bosun v0.85.0; this batch adds observations, not execution policy.

## Public probe model and Komari themes (Captain v1.48.0)

The original `/api/probe` API remains available. Additive `/api/probe/v2`
reads and Komari 1.6.1 public REST/WS/JSON-RPC reads share a typed public
projection. The compatibility API is only mounted on a dedicated probe host;
Komari agents, administration, terminals and account APIs are not enabled.
Unknown measurements, stale sampling evidence, actual NIC billing periods and
historical aggregation quality remain explicit in Captain extensions.

Migrations 97–99 add stable public identities, bounded metric history, validated
theme ZIPs/settings and an append-only probe hostname safety registry. The
registry survives site reset: a previously untrusted theme origin must never
fall back to the management site. It contains hostnames only. Restoring an older
database must not repurpose a theme hostname as an administration hostname.

No bosun protocol or customer billing change is required. Original themes may
ignore richer Captain fields, so a theme's lifetime traffic display must not
be interpreted as Captain's billing-period allowance. Older average-only
history cannot supply invented extrema. See [theme operation and data
semantics](PROBE_THEMES.md) for the target contract and installation limits.

## Server traffic controls (Captain v1.48.0 / bosun v0.83.0)

Server traffic policies are opt-in and independent of customer allowances,
inbound enablement and subscription publication. Existing servers keep the
alert-only behavior. A new private, foreign-keyed record stores automatic and
manual controls separately; existing probe/profile updates preserve it.
The administrator-only `node-service-control` API token scope is separate
from existing node and monitoring permissions. Writes require an optimistic
revision so a stale form cannot overwrite a later manual pause or quota hold.

The optional `Node.service_control` and `Report.service_control` protocol
objects are additive. Enabling restrictions requires a fresh capability report
from a supporting bosun; upgrading Captain alone does not add enforcement to
older agents. Saving desired state is not an application acknowledgement.
The management page distinguishes pending, applied, failed and unknown state.
Configuration, billing and private control details are not published on the
server probe or customer status pages. Customer status may show a confirmed
service pause without revealing its operator, quota or automatic policy.

Automatic decisions use accepted server-network counter deltas and the configured
cycle/direction, not user proxy billing. Monitoring must remain enabled for an
active automatic policy. Delayed, missing or duplicate samples cannot establish
a new healthy period. A manual hold survives cycle rollover, and restoring a
server that is still over quota requires an explicit exemption for that cycle
or a policy/allowance change. Traffic samples and configuration delivery are
asynchronous: this is not a byte-exact supplier-side spending cap.

Captain v1.49.0 / bosun v0.84.0 adds an `offline_enabled` management
setting which defaults to false and is
preserved when an older editor omits it. Enabling it requires the independent
fresh `offline_supported` capability; v0.83.0 alone does not support local
threshold evaluation. Its full policy and meter anchor travel in the optional
`service_control.offline` object; local execution evidence travels in the
matching report object. Existing SQLite state JSON stores the setting without
a new schema migration. Disable this option before manually downgrading to a
release without offline support. Subscriber receipts and billing are unchanged.

The first releases containing this feature are Captain v1.48.0 and
bosun v0.83.0; their built-in rollback floors retain those versions. Older
panels or agents can ignore controls or stop maintaining them. Manual downgrade
requires restoring compatible backups and explicitly arranging replacement
traffic protection. No existing release tag is changed.

## Server suppliers and primary infrastructure bills (Captain v1.46.0)

Migration 94 stores explicit server-to-supplier and primary-asset associations
separately from public probe information. Existing provider text and loose
`infra_assets.node_id` references are preserved; no assets are automatically
selected. The new administrator-only `/api/admin/infrastructure` API has its own
`infrastructure` token scope. Shared financial writes through existing node and
asset endpoints also require that permission, while unrelated legacy edits keep
their existing permissions.

The node probe API adds an optimistic infrastructure revision and an optional
supplier ID. Omission preserves associations; changing linked values requires a
matching revision. Payments, expiry changes and supplier renames invalidate old
drafts. Public probes retain the existing provider, fee and expiry projection,
without ledger IDs, contact details or payment history. No customer billing,
subscription rendering, agent protocol or bosun upgrade changes are involved.

Linking preserves the online expiry-extension setting. Accepted fresh resource
reports can advance both shared dates atomically without recording a payment.
The server-maintained additive `auto_renewal.anchor_day` preserves month-end
days separately from the real cycle month; callers cannot supply an arbitrary
anchor. Unlinking keeps the current values and date-maintenance setting.

Migration 95 records the payment's date action. Payment input adds optional
`date_action` (`record_only` or `set_due_date`) and `next_due` for an explicit
target. Recording only a payment changes no expiry or active status, and keeps
the existing ledger permission. Changing shared dates additionally requires
`infrastructure` permission. Explicit targets cannot precede the current expiry;
past payment snapshots are preserved even after a manual date correction.
Asset responses add read-only
`auto_renewal_enabled` for form defaults; changing that setting invalidates open
payment drafts. Payment history leaves date fields empty for bookkeeping-only
records, rather than inventing a coverage period.

Omitted actions retain the old period-advance/one-time behavior for existing
independent assets and manually maintained primary bills. A newly linked primary
bill with automatic date extension defaults to bookkeeping only. Optional
fields are omitted from legacy request hashes so successful pre-upgrade payment
retries remain idempotent. Old history remains distinguishable as legacy
period-advance records. No report or association manufactures a payment.

Captain v1.46.0 raises the built-in rollback floor to v1.46.0. Older panels
do not maintain these associations or payment actions and ignore a disabled
public homepage. Before manually downgrading, restore the matching pre-upgrade
database backup rather than editing shared values with an older binary.

## Curated client downloads

Migration 93 stores the curated client catalog independently of the existing
custom-client setting. New catalog and cached-download APIs are additive; the
legacy administrator and anonymous portal client-list APIs keep their existing
shapes and behavior. No agent protocol, subscription output or bosun version
requirement changes.

Release discovery, cached files and published download snapshots are independent.
Clients start hidden, and update checks do not change an existing publication.
Database backups preserve metadata but not installer bytes; missing cached files
are omitted from customer downloads until restored or downloaded again. Older
Captain versions ignore the curated catalog and cannot serve its cached routes.
See [client downloads](CLIENT_DOWNLOADS.md) for publication, access and storage.

## Subscription-node access groups

Migration 92 adds explicit inbound access policies (`all`, `groups`, `none`) and
multiple allowed groups. Existing inbounds retain their legacy `GroupID` behavior
until explicitly edited. Entries sharing an inbound share authentication and
billing eligibility; display categories, tags and server monitoring groups remain
independent. Plans keep one group and users retain the union of their usable
subscriptions and existing account-group grants.

The administrator-only `/api/admin/node-access` API uses a catalogue revision and
shared-impact confirmation. Entry writes may optionally include `Access`; changing
it requires both entry-write and node-access-write permission and commits with the
Entry in one transaction. Omitted/null `Access` preserves permissions. Legacy
inbound or reverse-connection edits preserve explicit policies; attempts to change
the old single-group field after adopting a policy are rejected. Omitting
`group_id` in managed reverse connection edits preserves its old value.

No new bosun protocol fields are needed: Captain supplies each inbound's existing
scoped-user list. Migration is forward-only. Old Captain releases cannot enforce
these policies, especially `none`, so the updater's rollback floor is raised to
v1.45.0, the first release shipping migration 92. Manual downgrade requires restoration
of a compatible pre-feature database backup; leaving the new database in place
can expose restricted nodes.

## City locations and map publication

Location lookup policy is stored separately from published server addresses and
public probe metadata. The additive administrator probe fields
`probe.location_ip_settings` and optional update input `LocationIP` select
`auto`, `manual` or `entry`; omitted input preserves the saved policy. The policy
defaults to automatic observations without entry fallback. Normal observations
outrank explicitly permitted confirmed fallback observations across both address
families; IPv4 is preferred within a source tier. Manual region/coordinates and
hidden display modes remain independent. Public location projections exclude
the selected IP, policy and evidence metadata. No agent protocol, published entry,
DNS or billing change is implied by choosing a location input.

Captain v1.59.0 adds the location policy in migration 115. Older panels ignore
this display-only preference, so their automatic location may differ. The
location feature alone does not require a higher rollback floor; the entry IP
policy shipped alongside it raises Captain's built-in floor to v1.59.0. Confirmed fallback selection uses the existing
bosun v0.92.0 evidence, while manual/entry lookup needs no agent upgrade.

Migration 91 adds independent subscription-node map metadata and a foreign-key
homepage publication list with an optimistic revision. No existing Entry columns,
proxy configuration, subscription region, agent protocol or billing fields change.
Server map preferences extend the existing private probe-info JSON. The effective
public `location` is additive: null means hidden or unknown; a missing field from
an older API permits the legacy country-marker fallback. Only public-safe location
fields are projected, without lookup IPs, timestamps or manual-setting drafts.

Old Entry updates preserve map metadata. Old website updates that omit/null the
new selection preserve it; explicit changes require a matching revision. Deleting
an entry removes its publication reference and invalidates older selection drafts,
so a reused numeric ID cannot inherit public visibility. Service-status visibility
continues to use its own publication list and user authorization.

No bosun upgrade is required beyond the existing public-address observations used
by automatic country detection. Older Captain releases ignore these display-only
options, and old probe-info writers may discard the new private preferences when
resaving them. Keep a database backup for downgrade; migrations remain forward-only.

## Carrier protocol and address-family selection (Captain 1.43 / bosun 0.82)

The new `carrier_probe` object is additive and retains separate TCP/ICMP,
IPv4/IPv6 target lists. Both panel and agent changes are required: the agent
must report `carrier_probe_version >= 1`. Older agents continue serving proxies
and custom monitoring tasks, while built-in carrier readings show an upgrade
requirement. The panel does not enable the legacy TCP flag alongside the new
object, because that would mislabel old-agent data as the selected protocol or
family. Existing explicit targets are retained; this release introduced
NextTrace defaults for empty lists. The later provincial TCP presets are
described below.

Migration 89 adds separate carrier-series history and cursor tables. Existing
history tables and conflict keys are unchanged, so older panels can still
write their original history format. Protocol, family, name and target identify
a new series; public charts never reinterpret old samples as the new target.
Settings omission/null preserves the new object, including hidden target drafts.
See [monitoring](MONITORING.md) for legacy hostname handling and TCP/ICMP limits.

Captain 1.43 uses the formal bosun v0.82.0 shared module without a local
`replace`. Managed nodes need bosun v0.82.0 or newer for the selected built-in
carrier protocol and address family. Upgrading the panel alone cannot supply
this capability. Existing proxy routing, subscriptions, customer accounting
and selected proxy-core versions are unchanged.

### Provincial TCP presets

`carrier_probe.tcp_province` is an additive province code, defaulting to `bj`
when absent. Empty TCP lists now use the selected Zstatic provincial targets;
ICMP defaults remain NextTrace Beijing. Existing explicit lists are not
rewritten. Omitted/null/empty province values in settings updates preserve the
saved choice; an explicit `bj` resets it. Old clients can still edit the other
fields without losing the province.

Captain expands empty lists into explicit addresses in agent state, without
modifying saved settings. Existing bosun v0.82.0+ nodes therefore use exactly
the same target identities as the panel even if they ignore the new province
field. Older agents still require the carrier-probe capability. No migration
is needed; old history remains associated with its original target. Public
task metadata adds `province` only for active TCP presets, respecting latency
visibility. Proxy routing, customer accounting and core versions are unchanged.

## Customer service status (Captain 1.41 / bosun 0.81)

Migration 88 adds private coherent runtime-report snapshots and service-status
publication/maintenance settings. Existing rows start without observations and
remain unknown until an accepted full agent report arrives; resource-only beats
cannot populate or refresh these observations. The companion bosun change adds optional `Report.applied_revision` to distinguish
a successfully applied state from the existing `revision` (last received state).
The old field keeps its meaning. Nodes without this confirmation continue proxying
normally but have unknown service status; upgrade bosun to obtain confirmed runtime
status. Missing capabilities remain unknown rather than being interpreted as a
failure. Captain 1.41 uses the formal bosun v0.81.0 shared module. Managed nodes
need bosun v0.81.0 or newer for this confirmation; upgrading the panel alone does
not make old agents' runtime evidence complete. No default core version changes.

Migration 87 adds optional subscription-node display categories. These do not
change access groups, billing, tags, regions or subscription output order.

New admin, portal and public service-status APIs are additive. Existing probe
routes, visibility, server monitoring, subscription outputs and customer billing
remain unchanged. Public publication is explicitly opt-in; maintenance is metadata,
not a proxy operation. Older Captain versions ignore this new page and its private
report columns; schema changes remain forward-only.

## Subscription presets (Captain 1.36 / shared bosun package 0.78)

Captain uses bosun's shared subscription compiler for administrator
ACL4SSR imports and optional enhancements. Existing managed nodes do not need
an agent upgrade for this feature; no agent fields or database migrations are
added. Basic and regional designs retain the seven existing output formats.
Claude/WebRTC/UDP-443 enhancements and representable imported rules use native
adapters for all seven formats. DNS protection supports Mihomo, Stash, Surge
and Egern with explicit client-specific constraints; unsupported rule/group
semantics remain blocked instead of being silently discarded. Inspection adds
optional warning fields, and Claude publication metadata adds `applied_formats`
while retaining the original Mihomo `applied` response. The existing apply API keeps
its all-format behavior when `formats` is omitted; an explicit list updates
only those supported global templates. Existing drafts, named profile templates,
response-rule templates and independent WireGuard subscriptions keep their
separate scopes. See [subscription templates](SUBSCRIPTIONS.md#templates-and-the-designer).

Captain 1.40 bundles shared bosun package 0.80 for these seven-format adapters
and regional automatic/manual selectors. This is a panel library requirement,
not a minimum managed agent version. Existing published templates remain in use
until the administrator reapplies the design. Custom groups and legacy name
filters retain their behavior; registered exit regions take precedence only for
the new region selectors.

Docker web upgrades add optional `host_update` metadata to existing update
responses; API paths and existing fields remain intact. Docker-managed node
upgrades require the new bosun host-updater support (v0.65.0+) and one-time
registration on that node. Older Docker nodes continue to reject in-container
binary replacement. No agent protocol or database migration is required.

Monitor-only node mode (Captain v1.18.0) requires bosun v0.66.0+ and migration 72.
The `Node.mode/mode_revision`, pairing capability and report mode/revision fields
are additive. Existing rows default to full mode. Nodes without mode reports
cannot be switched; ordinary old full-mode nodes retain their existing behavior.
A configured monitor node cannot be paired to a mode-unaware agent. After using
mode switching, missing support or a pending acknowledgement blocks new proxy
configuration until the matching full-mode report arrives.

## CPU topology on status cards (Captain 1.32 / bosun 0.76)

CPU topology on status cards adds optional `HostInfo.cpu_physical_cores`,
`cpu_sockets` and `virtualization_role` fields. `cpu_cores` retains its logical
CPU meaning. No migration or existing API change is required; old reports
fall back to a logical CPU count and do not invent physical topology. Full
topology requires bosun v0.76.0 or newer and complete Linux topology with a
confirmed non-guest virtualization role. Detail-page load metrics are unchanged.

## Storage on status cards (Captain 1.32 / bosun 0.76)

Migration 83 adds private per-node storage display settings. Existing probe API
routes and the root-only `disk_total`/`disk_used` meanings remain unchanged;
the public snapshot adds a separate `storage` array. Old admin clients that omit
or send null storage preserve the saved selection. Old agents fall back to their
existing filesystem report or root-only summary. The additive filesystem fields
`id`, `sampled_at`, `pool` and `member_mounts`, asynchronous capacity sampling
and mergerfs collection are available with bosun v0.76.0 or newer. Mount paths,
devices and pool membership stay private. Public root summaries and capacity
history follow the storage selection, including when root is hidden.
This change introduced a v1.32.0 update/rollback floor: older panels ignore this selection
and can expose the root filesystem again. Manual downgrade requires restoring
the matching pre-upgrade database backup.

## Server and proxy traffic (Captain 1.33 / bosun 0.76.1)

Server list and detail responses add optional `host_traffic` with separate
`used_up` / `used_down`, `valid`, and nullable Unix-second `cycle_start`,
`cycle_end` and `updated_at` fields. Existing `traffic_today_bytes` keeps its
UTC-day customer proxy meaning, attributed to the authenticating inbound's
server. Reverse exits do not repeat the front server's customer bill.

No migration, agent protocol or configuration-key change is required. NIC
observation metadata is stored in existing monitoring JSON; older panels may
discard that metadata without changing customer usage. Old positive monitoring
usage remains available with an unknown update time; missing baselines are not
reported as measured zero. Host traffic is independent of the allowance's
counting mode and retains its actual saved period while the server is offline.

Automatic exclusion of managed reverse-WireGuard and shaping interfaces requires
bosun v0.76.1 or newer on the monitored server. Explicit NIC selections remain
unchanged, and historical monitoring totals are not rewritten. No new rollback
floor is introduced by this change.

## Exit attribution and WireGuard devices (Captain 1.35 / bosun 0.77)

Migration 84 adds immutable server identities and daily collector-to-exit
traffic records. Management server list/detail responses add `proxy_traffic`:
`today_bytes`, `collected_today_bytes`, `forwarded_today_bytes`,
`received_today_bytes`, `unattributed_today_bytes`, `day_start`, `day_end`,
`history_complete` and `attribution_started_at`. Times are UTC Unix seconds.
The new display total is collected minus forwarded plus received. The original
`traffic_today_bytes` field retains its collector-based meaning; old clients
and the customer's quota ledger are not reinterpreted. Attribution is committed
with report receipts and billing, then retained independently of later topology
changes. Attribution follows topology at report acceptance, not a reconstructed packet-time route. Unknown inbound reports stay on their collector. Migration backfill is
conservative, and coverage metadata does not promise complete old history or
zero loss from core/host failures.

Migration 85 stores independently revocable WireGuard devices. The new
`/api/portal/me/wireguard-devices` and
`/api/admin/users/{id}/wireguard-devices` APIs support list/create, and their
`/{device}` paths support rename/delete. The list contains server-evaluated
availability, its reason, the WG limit, `remaining`, optional `shared_limit` /
`shared_used`, device records and eligible entry choices. When HWID registration
is enabled, WG devices share its capacity; `shared_limit: 0` means unlimited
shared capacity, while the WG-specific maximum still applies. Admin and operator
accounts can manage credentials; support can read names and timestamps without
subscription URLs. Cookie authentication and origin checks remain in force.

Each device URL is a WG-only credential, separate from the normal subscription
token. Its formats are `wireguard`, `clash`, `stash`, `singbox` and `egern`;
native WG configuration requires a single entry. Client-side dialer chains,
custom subscription templates and entry extra fields are not used in this
restricted output. Existing normal subscriptions preserve their previous
format and credential behavior. Resetting a permanent subscription token also
revokes its device links immediately and withdraws their peers at the next
successful server synchronization. Ordinary downloaded credentials and existing
temporary links retain their previous behavior.

Native device protocol support starts at bosun v0.77.0. Deploy bosun v0.77.1
or later with the supported Xray `26.7.28-r3` package selected explicitly,
and a current capability report for
creation. Updating Captain or bosun does not automatically replace the default
core. The additive `User.wireguard_peers`, `User.wireguard_legacy_disabled` and
core `wireguard_devices` capability do not change legacy users. Devices have
independent keys and addresses but share their parent's user identity for
accounting and limits. Older agents/cores continue to serve compatible existing
services; they are not silently given device credentials they cannot handle.
Changing WG peers may restart their shared Xray instance and interrupt its
other connections.

The original Xray `26.7.28-r2` package must be replaced with `-r3`: the newer
Go 1.27.3 TLS layout is incompatible with that upstream version's Vision path.
The replacement uses the supported Go 1.26.10 toolchain and the same security
updates to dependencies. Bosun v0.77.1 also explicitly preserves the previous
REALITY client-version acceptance, so enabling WG devices does not reject
existing Mihomo clients. This retains compatibility; it does not upgrade a
client's TLS fingerprint or change its privacy properties. The default Xray
package remains unchanged. No Captain protocol or database change is needed
for this bosun compatibility patch.

Captain v1.35.0 raised the built-in update/rollback floor to v1.35.0;
v1.45.0 raised it further for inbound access policies, and v1.46.0 retains
those protections while preserving shared infrastructure dates and payments.
Older panels can omit registered device peers and lose the new
attribution semantics. Manual downgrade requires restoring the corresponding
pre-upgrade database backup, including its older credential state. See
[device subscriptions](SUBSCRIPTIONS.md#independent-wireguard-devices) and
[rollback procedures](OPERATIONS.md#rollback).

## Pinned Claude routing rules

The designer optionally stores `enhancements.claude_rules` containing the
selected domain rules and their source revision. This is additive; designs
without it retain the original built-in rules. The cached library, saved draft
and published global Mihomo template are independent settings. Applying the
design saves its publication metadata and rendered template atomically.
No migration or agent-protocol change is required, and the compiler runs in
Captain rather than on the servers. Captain v1.39.0 uses the bosun v0.79.0
library for this feature; existing servers do not need an upgrade for it.

An older Captain can continue serving the already-rendered text templates, but
its designer does not understand the snapshot: saving or applying from that
older designer can discard the selected version and return to its built-in
list. Keep a backup before downgrading and avoid editing such designs on older
versions. The existing global rollback floor is unchanged.

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
| 1.65.0 | Shared bosun0.95.0 retained; no agent upgrade | Persistent administrator migration-notice dismissal; schema120/floor1.63.0 unchanged. |
| 1.64.0 | Shared bosun 0.95.0 retained; no agent upgrade required | Unique new/renamed plan names and synchronized owned-group names, migration120. Historical duplicates and permission IDs preserved. Rollback floor1.63.0 retained. |
| 1.63.0 | Formal shared bosun 0.95.0; plan/category permissions do not require an agent upgrade | Migration119 converts legacy global grants; explicit category-follow rules and unassigned console creation. Matching agents add non-fatal core preparation and native candidate preflight. Rollback floor1.63.0. |
| 1.62.0 | Formal shared bosun 0.94.0; no installed-agent upgrade required for plan permissions | Independent/shared plan access, Surge VLESS and Go 1.26.11 security maintenance; migration118, rollback floor1.62.0. |
| 1.61.0 | Formal shared library 0.93.0; no installed-agent upgrade | Subscription account presentation, mail templates/previews, yuan price editor and city corrections; schema 117 and rollback floor v1.60.0 unchanged. |
| 1.60.0 | Formal shared library 0.93.0; no new installed-agent requirement | Managed entry-domain composition, explicit NAT address inheritance and subscription endpoint binding; migration 117. Captain rollback floor becomes v1.60.0. |
| 1.59.0 | Formal shared library 0.93.0; no installed-agent upgrade needed for entry IP policy or location settings | Per-entry connection-family policies and independent location IP selection; migrations 115–116. Existing observation capability gates remain, and Captain rollback floor becomes v1.59.0. |
| 1.58.0 | ≥ 0.92.0 for optional fallback following; older agents remain compatible and observation-only | Per-family opt-in with conflict/continuity evidence and sustained confirmation. Migration 114; existing rollback floors remain unchanged. |
| 1.57.0 | 0.91.2 for new native dependency recovery/Doctor behavior; older agents remain compatible | Formal v0.91.2 shared module and separate install-time OS-package choice; no schema or rollback-floor change. |
| 1.56.0 | ≥ 0.90.0 for high-port IP observations and complete rolling-window evidence; existing compatible agents can run independent plans | Default/per-server carrier plans and advisory IP fallback, bilingual groups/categories, Agent access/firewall diagnostics and preceding-day traffic resets. Migrations 112–113; Captain rollback floor v1.56.0 protects per-server probe intent. |
| 1.54.0 | ≥ 0.89.0 for Captain delivery and interface-bound probes | Optional agent origins and native package delivery, OpenWrt monitor setup and path-isolated ICMP/TCP tasks; migrations 110–111. Older nodes retain existing services. |
| 1.51.0 | ≥ 0.86.0 for live diagnostics and detailed traffic provenance | Temporary administrator leases and bounded private batch history; older nodes retain configuration and accounting compatibility. Migration 107. |
| 1.53.0 | ≥ 0.88.0 for UDP/both or non-relay complete paths | Derived transport, per-hop backend intent and staged path snapshots; existing native TCP paths keep their earlier gates. No migration. |
| 1.50.0 | ≥ 0.85.0 and fresh capability for path handshake health; ≥ 0.84.0 receipts for staged routes | Complete-route staged plans, atomic inbound/line/Entry changesets and optional TCP relay path health; migrations 104–106. Existing agents and immediate delivery remain compatible. |
| 1.49.0 | >= 0.84.0 for receipts, staged release prerequisites and offline enforcement | Immutable line-owned snapshots, opt-in TCP relay release plans, fixed-version complete paths, bounded cleanup evidence; migrations 100-103. Existing immediate delivery remains compatible. |
| 1.48.0 | ≥ 0.83.0 for server traffic enforcement | Opt-in server pause/aggregate rate protection with independent application evidence; original agents remain compatible. Komari public themes and panel-side DNS controls do not require a node upgrade. Mihomo dual-stack design generation uses the formal shared module. |
| 1.46.0 | Unchanged; shared module v0.82.0 | Server supplier/primary-bill associations, independent payment date actions, settings navigation and an optional homepage; migrations 94–95. Existing managed nodes do not need an upgrade. Captain rollback floor is v1.46.0 to retain shared dates and payment semantics. |
| 1.45.0 | Unchanged; shared module v0.82.0 | City locations and explicit map publication, multiple inbound access groups and curated client downloads; migrations 91–93. Existing managed nodes do not need an upgrade. Captain rollback floor is v1.45.0 to retain access policies. |
| 1.35.0 | ≥ 0.77.0 and explicitly activated Xray `26.7.28-r3` for native WG devices | Independent WG-only device subscriptions and parent-account limits; migration 85. Managed reverse exit display uses existing traffic reports and migration 84 without requiring a fleet upgrade. Existing fields and ordinary subscriptions remain compatible; Captain rollback floor becomes 1.35.0. |
| 1.34.0 | Unchanged | Additive public storage history and per-volume detail charts. Absolute filesystem history starts with this panel version; older data-volume percentages are not reconstructed as capacity. No migration or agent protocol change. |
| 1.32.0 | ≥ 0.76.0 for full CPU topology, asynchronous filesystem sampling and mergerfs | Additive CPU/filesystem fields and private storage selection (migration 83). Old agents retain logical CPU and existing filesystem/root fallbacks; traffic units and card layout need no agent update. |
| 1.28.0 | ≥ 0.75.0 for core-only split routing, WG prerequisites and managed forwarding | Additive capabilities; migration 78 preserves legacy WG options. Existing agents remain usable for previous features; new combinations require fresh support. |
| 1.27.0 | ≥ 0.74.0 for automatic private access and prerequisite checks | Scoped policy features are independent of nft runtime status; core-only authorization needs administrator acknowledgement and supported direct sing-box/Extended. Existing policies/older agents retain prior behavior. No migration. |
| 1.26.0 | ≥ 0.73.0 for live hostname updates | Older agents retain their last paired hostname. Node cost and reset settings need no agent update. |
| 1.24.0 | ≥ 0.72.0 for detailed firewall status | Automatic listener ownership, references and synchronization diagnostics. Older host-management snapshots remain supported. No migration. |
| 1.23.0 | ≥ 0.71.0 for host management | Typed host tasks, actual-state UI and independent recovery. Older nodes remain usable without host actions. No migration. |
| 1.22.0 | ≥ 0.70.0 for route-tool management | Explicit NextTrace installation and tool/ICMP checks; route collection remains available on 0.69.0. No migration. |
| 1.21.0 | ≥ 0.69.0 for return routes and public address observations | Optional private route history/schedules and address following (migrations 74–75); existing fixed entries and older nodes remain supported. |
| 1.20.0 | ≥ 0.68.0 for reverse WireGuard | Managed kernel tunnels, explicit LAN grants, shared user shaping and migration 73; old nodes retain ordinary services. |
| 1.19.0 | (no change) | Glassmorphism status page served by the React port; no agent, API or database change. |
| 1.18.0 | ≥ 0.66.0 | Monitor-only installation and confirmed node mode switching; existing full nodes remain compatible. |
| 1.17.0 | ≥ 0.65.0 | Docker web upgrades through the node's host updater (optional `host_update` metadata); older Docker nodes keep rejecting in-container binary replacement. |
| 1.16.0 | (no change) | Node domain ownership checks, DNS health and the DNS write journal are panel-side (migrations 70–71). |
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
rollback may not go below; set it to the version you have tested. Captain's
built-in floor is v1.63.0 to preserve ingress address inheritance, subscription bindings, strict entry IP-family policies, independent server probe plans, interface bindings, complete-route transports, backend intent and health policies;
this also preserves shared infrastructure dates, payment actions, the homepage switch and inbound access policies, and
retains device credentials, exit attribution and the earlier protections for line ownership, client chains, internal
forwarding and public storage selection. A higher configured floor still wins.
This protects the built-in updater, not manual replacement of the executable.

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

### Retired external monitoring exporters

The next release removes Komari/DStatus exporters and their UI. Existing API paths
remain as disabled compatibility endpoints; enabling is rejected. Agent fields and
migration 51 remain intact, but Captain never sends exporter configuration. Old
managed nodes stop on their next successful state pull; standalone nodes must be
upgraded. Built-in monitoring and Glassmorphism are unchanged.


### Managed reverse WireGuard (Captain 1.20 / bosun 0.68)

`Node.wireguard_links`, `Inbound.wireguard_slot`, Xray capability
`wireguard_reverse`, and agent `Report.wireguard` are additive. Captain migration
73 owns managed connections and their dedicated subscriber resources. Both peers
must report current transport capability; A must also advertise Xray support.
Captain withholds the whole managed inbound when either peer is unsupported,
so an older agent cannot ignore the routing field and serve it through direct
egress. Re-pairing clears capability and report writes are bound to the current
node token. Existing VLESS Reverse resources and APIs keep their behavior.

Bosun v0.68.0's built-in updater and rollback retain a v0.68.0 minimum, including
when no link is active. A previous binary of unknown version cannot satisfy a
configured floor. This prevents old binaries from ignoring WG routing fields in
cached desired state. Manual replacement outside the updater is not protected.

Captain v1.20.0 similarly retains a v1.20.0 built-in rollback floor: older panels
would discard the WG fields while rebuilding desired node state. Higher
administrator-configured floors remain in effect on both applications.

### Managed WG forwarding and address preference (Captain 1.28 / bosun 0.75, migration 78)

Migration 78 adds `node_wireguard_links.domain_preference` with an empty default
and `auto_forwarding` with a false default. Existing connections retain their
original IPv6-first domain routing and administrator-managed forwarding rules.
The new UI creates connections with IPv4 preference and automatic forwarding
enabled. Legacy API creation without these fields retains the old defaults;
updates that omit them or send `null` preserve saved intent. Explicit `""` and
`false` restore the respective legacy behaviors. Unknown preference values are
rejected. The connection API paths and existing response fields remain intact.

`WireGuardLink.domain_preference`, `WireGuardLink.auto_forwarding`, and the
`Report.wireguard` fields `domain_preference_supported`, `forwarding_supported`
and `forwarding` are additive. Domain preference is delivered to A; automatic
forwarding is delivered to B. Both save and state delivery require the relevant
fresh capability, in addition to the existing transport checks. Missing,
downgraded or expired capability withholds both transport configurations and
the whole managed subscriber inbound, preventing direct fallback on A.

Automatic forwarding is an explicit per-connection opt-in. It supports UFW and
unambiguous main-table routing, retains manual rules, and uses a separate durable
ownership journal for cleanup. Firewalld, simultaneous backends, custom policy
routing (including a B also serving as A elsewhere), and ambiguous interfaces
are unsupported and reported. A requested automatic connection also fails if
global `firewall_auto_open` is disabled; it does not silently become manual.

The preference changes DNS-family selection, not literal addresses or the
family of AAAA-only answers. It supplies neither IPv6 connectivity nor a retry
of another family after connection failure. Forwarding checks have their own
timestamp: a new heartbeat does not make an old successful check current. Saved
configuration, applied rules and a recent WG handshake do not prove subscriber
connectivity. Available in Captain v1.28.0 / bosun v0.75.0.

Built-in update/rollback floors are Captain `v1.28.0` and bosun `v0.75.0`, so
older binaries cannot ignore the new saved options or ownership journal. Higher
configured floors still apply. Manual binary replacement is outside that protection.

The additive `private_access_policy.split_routing` capability gates core-only
private authorization with WARP, proxy exits and balancers at save and delivery.
Old agents keep existing direct-only behavior; missing or stale capability cannot
silently enable a combination they cannot enforce. Dual protection stays direct-only.
WireGuard prerequisite reports and typed jobs are additive and administrator-only;
unsupported or older agents cannot execute installation jobs.


### Managed lines and client chains (migrations 79–80, unreleased)

Migration 79 adds reusable server lines, stable member identities and optional
Entry line bindings; migration 80 adds an optional client-dialer Entry reference.
The existing Node, Inbound, Entry and per-node forwards APIs retain their names
and ordinary behavior. Omitted new Entry fields preserve existing bindings;
explicit resets remove them. Legacy forward updates preserve line-owned rules
and reject attempts to change those rules outside the line editor.

The panel compiles lines to existing bosun Forward fields. Built-in single-target
relay rules retain old-agent compatibility; candidate groups, realm and PROXY
protocol require bosun ≥ 0.49, and managed nft lines require ≥ 0.62. Unsupported
line rules are withheld when agent support disappears. No new agent protocol
field or bosun build is required.

Managed client chains require a complete supported mihomo, recent Stash or
sing-box subscription. Missing permissions, incompatible formats or invalid
references omit the dependent chain; single-node links never flatten it into a
direct connection. See [subscription rules](SUBSCRIPTIONS.md).

The first release containing this schema must be v1.29.0 or newer. The current
built-in Captain update/rollback floor is v1.29.0, superseding the v1.28.0 floor
above. Manually restoring an older binary requires its matching pre-upgrade
database backup; an additive migration does not make these behaviors safe to
ignore. bosun's own rollback floor is unchanged.

## Explicit internal forwarding addresses (migration 82)

Line `target_address_mode`, later member `address_mode`, and single-forward
`target_address_mode` are additive values (`default` or `internal`). New
resources retain default routing unless explicitly opted in; omitted/null modes
on updates preserve the previous selection. Existing Node `InternalAddr` is
reused without renaming its API or config fields. Omitted/null InternalAddr on
node updates retains the stored value, while an explicit empty string is
rejected when an internal forwarding reference still needs the address.

The panel resolves references into the existing agent Forward target/listen
fields, so no new bosun protocol or minimum version is required beyond the
chosen backend's existing requirements. Internal address selection does not
create a private network or grant proxy access to it. The optional TCP check
uses existing administrator-only diagnostics (bosun ≥ 0.56).

Internal address selection introduced a v1.31.0 update/rollback floor: older panels can
ignore the saved mode and regenerate a public target on an unrelated edit.
The current floor is v1.32.0 to also preserve storage visibility choices.
Manual downgrade requires restoring the matching pre-upgrade database backup.

## Configuration receipts (Captain v1.49.0 / bosun v0.84.0)

Migration 100 adds private node configuration sessions, current delivery heads
and bounded delivery history. It does not backfill successful observations or
change existing configuration, accounting, subscription or authorization data.
No existing API path, field meaning or configuration key changes.

The optional agent fields `State.config_revision`, `State.config_delivery` and
`Report.config_receipt` preserve legacy delivery. The complete `Revision`/ETag
still changes with dynamic users, jobs and service controls. Process headers
only opt into confirmation; legacy agents do not create confirmation records.
A new delivery bypasses an otherwise matching ETag, while identical content and
delivery retain 304 behavior. Confirmation is independently sequenced and never
borrows freshness from a retried customer-traffic batch.

Configuration confirmation requires bosun v0.84.0. Captain uses that formal
module dependency; release verification runs with `GOWORK=off`. Older nodes
retain immediate delivery but cannot provide confirmation.

## Managed-line change receipts (Captain v1.49.0)

Migration 101 adds line incarnations, atomic change/target records and bounded
delivery-to-target associations. Existing lines acquire identities without
fabricated change or successful-application records. Save/delete responses add
`change_id`; existing payloads, routes and immediate configuration delivery are
preserved. History APIs are administrator-only with a separate
`line-changes:read` scope. Older narrow resource tokens gain no additional access.

This panel feature consumes the optional configuration receipts above and adds
no further bosun protocol fields. Legacy nodes still receive forwarding normally
but cannot confirm application. Unchanged metadata and dynamic users do not
create forwarding targets; authorization is never held for a rollout. A target
confirms only its line-owned forwarding within the recorded delivered state,
not whole-path connectivity or the current state of unrelated settings. Line
incarnations, node UUIDs, pairing identities, process sessions and fresh receipts
prevent deleted/recreated resources or stale responses from confirming a new
change. These history records do not by themselves schedule releases. The optional
staged workflow is separate and never freezes user credentials.


## Structural releases and complete paths (Captain v1.49.0)

Migration 102 stores staged ordinary TCP relay plans, immutable forwarding
snapshots and fresh attempt evidence; migration 103 stores fixed path revisions
and protected complete-route ownership. The new administrator APIs use separate
`line-rollouts`, `forward-paths` and `path-routes` scopes. Existing line APIs
retain immediate semantics unless an active plan reserves their resources.

A plan changes only its owned forwarding projection. Current authorization,
credentials, jobs and service controls remain immediate. Complete paths use
existing relay fields without a new node protocol, but cannot use the ordinary
line rollout scheduler. Version pinning preserves the full candidate sequence
rather than creating arbitrary cross-branch combinations. The Captain rollback
floor is v1.49.0; manual downgrade requires a matching backup and verified runtime
recovery. See [operator workflows and limits](LINE_RELEASES.md).

## Complete-route releases, changesets and path health (Captain v1.50.0)

Migrations 104–106 add complete-route release intents, topology drafts and
independently sampled path-health evidence. New resources are administrator-only
and have separate `path-rollouts`, `topology-changesets` and `path-health`
permissions. A changeset additionally requires the original write permission
for every operation it commits. Existing endpoints and response fields remain
available; ordinary line and complete-route release histories stay separate.

Changesets cover supported inbound, ordinary-line and Entry creation/edits in
one SQLite transaction. Preview is read-only and submission checks the draft,
object identities and current topology again. Private creation settings remain
server-side, while existing credentials and access policy stay live references.
See [the supported fields and commit workflow](TOPOLOGY_CHANGESETS.md).

The optional health policy adds `Forward.PathHealth` and independent report
evidence. States without it retain the released structural-digest contract,
so bosun v0.84.0 can still confirm existing configurations. New health requires
bosun v0.85.0 and fresh capability evidence; an older agent cannot enable it.
It tests supported handshakes through the actual candidate listeners, not
customer authentication or Internet reachability. Auto failover is opt-in and
refuses new connections when no confirmed healthy candidate is available.
See [supported handshakes and freshness](PATH_HEALTH.md).

Built-in rollback floors are Captain v1.50.0 and bosun v0.85.0. Older software
can lose active route intent or ignore health filtering. Manual recovery needs
a compatible database backup and an explicitly verified service configuration;
replacing the binary alone does not preserve these guarantees.

## DNS following modes (development)

The optional designer enhancement `dns_mode` accepts `follow`, `dedicated` and
`custom`. Missing mode retains the existing independent-policy or automatic
Claude/dedicated behavior. New UI opt-in defaults to following the main group;
loading or saving an existing draft does not change its mode. Main selection
uses a base select group containing `@all`, then the final base group, or requires
an explicit administrator choice. Dedicated mode creates a manual DNS group
independently of Claude; custom mode retains proxy-only validation.

Follow mode permits an explicitly selected manual DIRECT path and explains this
in the editor, but rejects automatic groups that can reach DIRECT. Ordinary DNS
uses the same named group as the main connection; Claude DNS still follows its
own policy. Domestic/direct and proxy-hostname bootstrap queries remain separate.
No arbitrary service-group inference is performed. A shared load-balancing group
does not guarantee the same individual node for different connections, and DNS
caches/connection reuse can outlive a selection change.

Captain's next-release rollback floor is prepared as v1.66.0: older shared
renderers ignore the new mode and can generate independent or direct DNS paths.
Captain pins bosun `v0.96.1-0.20261113061455-371d59e3f784`, an immutable
source revision containing this renderer. This is a module pin, not a new bosun
product release; task-local go.work is not needed by the integrated mainline. No installed
agent protocol or proxy-core changes are required by this feature.
