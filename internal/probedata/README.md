# Public probe read model

`Engine` projects both native v2 and Komari-compatible reads from the same public
allowlist. A signed-in staff identity does not expose hidden/unpaired servers,
IP addresses when disabled, probe targets, device identifiers, paths, process
lists, collector epochs, credentials, or disabled public categories. Management
inspection continues to use the existing management API. Public UUIDs are
persisted per node incarnation; deletion and site reset remove them. A private
stable-policy fingerprint detects identity/privacy changes during reads. Snapshot
reads retry at most three times and then fail closed. Ordinary heartbeats and
metric timestamps do not change that fingerprint.

Inventory is separate from runtime observations. `last_seen` is presence;
`received_at` is the accepted metric receipt time. `fresh=false` leaves historical
values identified as stale. Nullable values distinguish unavailable metrics from
measured zero. Host traffic has its own validity, metering timestamp and actual
cycle, independent of proxy billing. Costs preserve decimal strings and explicit
calendar cycle types.

## History

New accepted beats atomically write the fixed public metric vocabulary, with
count, sum, min, max, first and last. Duplicate resource sequences and revoked
pairing tokens never write additional measurements. NIC traffic deltas use the
same accepted, reset-aware counters as server traffic. There are no inferred
historical deltas from a present cumulative total.

Retention follows the existing prune job: recent second buckets 2 hours, minute
buckets 48 hours, hourly buckets 60 days and daily buckets 730 days. Daily
boundaries match the existing receipt-time calendar so old and new history
cannot be double-counted on non-UTC hosts. The public
snapshot includes the most recent 10 minutes. Query output is bounded by entity,
metric and point counts and fills missing observations with null, never zero.
The per-series `captain_source` identifies accepted samples, rollups or legacy
averages. Queries can downsample averages by each metric's valid observation
count; unavailable measurements do not enter that metric's denominator.

Before this feature, `node_stats` saved per-metric sum/count only. Historical
average/sum/count remain available. An extrema/first/last query touching those
older averages returns an explicit unsupported-aggregation error instead of
inventing samples. New raw/rollup statistics provide average, sum, count, min,
max, first and last. The metric catalog advertises actual capabilities. GPU keys
are recognized, but public GPU history is unavailable because the existing
public page settings have no explicit GPU disclosure control. Existing private
GPU collection and diagnostic history are unchanged.

Ping latency averages are weighted by successful attempts; loss is weighted by
all attempts. Stored histograms are combined before computing quantiles, never
by averaging individual bucket percentiles. A partial/absent histogram produces
null quantiles and extrema for the complete query window. Histogram quantiles
are upper bounds with at most 5% or 0.1 ms error, marked `quantiles_approximate`.
A historical bucket average is never presented as the latest individual ping.
Standard deviation is null when the underlying samples did not retain enough
information. Canonical `PingStat.loss` is a ratio from 0 to 1, and
`p99_p50_ratio` means p99/p50; Komari's legacy presentation conversions belong in
the compatibility transport.

All existing `/api/probe` response types are retained. This package contains no
agent protocol, billing policy or Komari management/terminal emulation.
