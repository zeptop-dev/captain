# Monitoring and infrastructure insights

These additive Captain features reuse existing bosun measurements, proxy-line
compilation and the shared SQLite file. No agent or core change is required.
Migration 121 preserves old settings and permissions; new anomaly alerts and
return-route notification policies default to disabled.

## Monitoring storage

Administrators see the shared database file, WAL/SHM, reusable pages, monitoring
table/index allocation when SQLite's dbstat is available, and the latest bounded
retention run. Failed/unsupported size measurements remain unknown. Table sizes
include indexes but do not imply that SQLite can return those pages to the OS.

Explicit maintenance runs one existing bounded cleanup round and a PASSIVE WAL
checkpoint. It does not VACUUM the shared business database, delete current data,
change retention, stop the panel or promise to reclaim every WAL page. Large
backlogs can need multiple rounds. Concurrent manual runs conflict; ordinary
scheduled cleanup retains its bounded batches. Heavy administrator resource and
network-history reads have a separate one-query lane and five-second budget;
waiting does not reserve the sole SQLite connection. Existing short/minute/hour/
day storage and network percentile semantics remain unchanged.

GET /api/admin/settings/monitor-storage and POST /maintain use the new
monitor-storage scope. Narrow settings, nodes or monitoring grants do not gain
this new capability automatically.

## Return-route changes

A server's diagnostics / return-routes view adds expected CT/CU/CM classifications,
a consecutive confirmation count and optional notifications. Unknown outcomes,
incomplete paths and missing ASN data do not confirm a change or a recovery.
Only accepted first job completions advance state; a replay is inert. Policy or
city/family edits fence pending older jobs and reset confirmations. Rules-version
changes generate their own event and rebuild the classification baseline.

Events are private, retain their original times/rule version and show the latest
100 of at most 200 per server (180-day retention). They are not reconstructed
from pre-upgrade history. Notifications compare the current confirmed route with
an explicitly configured expectation, for the scheduled city/family. A stale or
unknown measurement leaves an incident unresolved instead of inventing recovery.
Changing conditions ends old incidents as disabled. No automatic routing change
or direct fallback is introduced. Paths describe server-to-target observations;
ASN labels, possible GIA, and route changes do not prove a user's ingress path.

The existing return-routes GET response adds watch/events. Policy PUT at
/api/admin/settings/nodes/{id}/route-watch requires route-watch write permission.

## Network anomaly alerts

The server settings / monitoring page provides off, fixed and adaptive latency
policies, loss threshold, sample minimum and a 2–30 minute window. Supported data
are fresh, identified carrier measurements and interface-bound task series.
Legacy or unbound samples without a verifiable target identity remain displayable
but do not train a baseline. Protocol, family, target and interface are isolated;
custom-task edit generations prevent A→B→A from reviving a previous baseline.

An initial clean, sufficiently covered window builds a median baseline. The
adaptive upper threshold is baseline + max(relative allowance, absolute floor).
A covered abnormal window freezes learning; recovery holds learning for another
window. Successful measurements, time coverage and sample counts are required
for latency recovery; full timeout windows can still trigger packet-loss alerts.
Permission/DNS/tool errors are unknown measurements, not fabricated packet loss.
Repeated sequences do not add samples. Long gaps/agent epochs restart learning;
configuration edits and disabling collection clear learning state. This is
monitoring only and never changes user accounting or node access.

GET/PUT /api/admin/settings/nodes/{id}/network-alerts requires network-alerts
read/write. Current evidence expires in the browser using elapsed monotonic and
wall time and on failed requests/offline state. Pending edits are retained on
failure. Existing global collection settings still control execution.

The incident list offers type-specific 24h/3d/7d/indefinite silence or removal.
This applies to a server and exact incident kind, including subsequent incidents
of that type. Unlike some upstream defaults, both failure and recovery notices
are suppressed. Incidents remain recorded and availability is unchanged. A
continuing, previously unnotified failure can notify after the silence expires;
a failure entirely inside silence does not create a late recovery. The existing
maintenance/silence window and acknowledgement behaviors remain independent.

## Cost analysis

Infrastructure adds an analysis tab with a UTC month and optional asset filter:
recorded base payments, additional expenses, estimated accrued operating cost,
monthly/annual recurring budget and estimated remaining cycle value. Currencies
remain separate; no live FX service or implicit currency conversion is introduced.

Migration 121 snapshots the terms known at upgrade, without guessing older
prices. Database triggers append snapshots for asset price/currency/cycle/due/
active changes, including linked-server edits; metadata-only edits do not create
versions. History and totals are read from one database snapshot. Display shows
the latest 100 prices/expenses, while totals cover all recorded versions.

Accrued operating cost normalizes recurring prices by calendar month and the
recorded effective interval. Earlier unrecorded periods remain unknown. Budget
uses current active recurring terms, even if overdue. Remaining value uses the
current cycle's real calendar dates. These are estimates, not payment receipts,
refund entitlements or automatic renewal. Current-cycle and forecast figures are
as of now, even when inspecting a different month's actual/accrued totals.

Additional IP/traffic/other expenses use explicit currency, date, minor-unit
amounts and idempotency keys. A repeated identical request returns the original
record; different data with the same key conflict. Reversal appends a negative
entry dated today, exactly once, retaining the original. Assets with such history
cannot be physically deleted. These records do not affect subscription balances,
supplier transfers or server expiry. Existing payment idempotency hashes are
unchanged. The analytics and expense endpoints under settings/infrastructure use
an explicit infra-analytics scope; narrow infrastructure/settings tokens need an
additional grant.

## Guided line creation

The create-line menu adds an optional guide: front server(s), optional relay pool,
strategy, final inbound and a suggested listening port. Required ingress mappings
remain explicit; exhausted mappings leave the port unset. It opens the existing
line editor, where backend protocol limits, ports, dependencies, preview revision
and saving are still validated. Cancelling before save creates no resources.
The guide does not change existing lines, grant node access, add another topology
model or silently add direct fallback. Existing managed-line API scopes apply.
