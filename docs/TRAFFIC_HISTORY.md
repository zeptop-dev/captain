# Traffic batch source history

The administrator server diagnostics view can show recently accepted traffic
batches, their source intervals and journal backlog. This is operational
provenance, not a second billing ledger or a reconstruction of missing host
monitoring samples.

`GET /api/admin/nodes/{id}/traffic-history?before=<cursor>&limit=50` returns an
`items` array, optional `next_before`, retention limits and an optional current
`delivery` snapshot. Limits are 1–100 rows. Interactive administrators can read
it; API tokens need both `traffic-history:read` (or the endpoint grant) and the
existing node-detail read permission. Operator, support, public probes and
customer endpoints do not gain access.

Each item contains an opaque numeric cursor `id`, `epoch`, decimal-string
`sequence`, `received_at`, `accounting_day`, `up_bytes`, `down_bytes`,
`totals_known`, `discarded_samples`, `metadata_status`, and optional `sample`.
Large journal sequences must not be converted to JavaScript numbers. Byte
totals describe accepted customer traffic rows in that batch, not the actual
amount charged to every subscription. They do not add inbound/outbound
aggregate counters again. Unknown totals must not be rendered as zero.
`discarded_samples` counts report rows rejected by existing attribution/range
checks; it is not a known number of missing bytes.

The original receipt cursor, traffic totals, subscription updates and new
history row commit in the same transaction. Repeated or older receipts neither
charge again nor update the first reception time. Expired history is not
recreated by retrying an old batch. Metadata never changes UTC accounting-day
selection, first connection time, dynamic rate windows, package selection,
expiry, quota, subscription renewal, or order relationships. Delayed traffic
continues to be handled at the existing reception/accounting time.

With a supporting agent, `sample.collection_started_at` and
`collection_ended_at` bracket counter-read operations. Independent `sources`
identify a core and counter family, with optional `started_at` and
`duration_ms`, `ended_at`, `segments`, `quality` and fixed `reasons`.
`observed` means identifiable read boundaries, not an assertion of lossless
upstream accounting. Initial reads, process changes, failed resets and old
journals cannot establish continuous coverage. Merged checkpoints are marked
`coalesced`; their totals are not proportionally split into imaginary minute
samples. See the matching bosun document for the fixed reason vocabulary.

Node sampling timestamps and panel receipt timestamps use independent clocks.
Display them separately; subtracting them is not a trustworthy transport
latency. An absent sample is `legacy`, an invalid optional object is `invalid`,
and valid metadata preserves its own `observed`, `partial` or `unknown` quality.
Wrong JSON types, overlong metadata and invalid timestamps do not block valid
traffic accounting. Raw invalid content is not retained or exposed.

`delivery` contains agent `sampled_at`, panel `received_at`, `fresh_for_ms`,
`pending_bytes`, `buffered_bytes`, `journal_bytes`, and optional `oldest_at`.
It covers already collected journal data only. An identical or older snapshot
cannot refresh reception time. Freshness expires within three minutes of both
observation and reception; conflicting clocks withdraw current status. Clients
must subtract request/page elapsed time and withdraw current values on request
failure. Historical values are not a claim about a currently offline agent.
Re-pairing hides the previous identity's history and queue status.

Migration 107 starts with empty history. There is no backfill from old receipt
cursors, daily totals or current host snapshots. Retention is at most seven
days, 12,000 rows per server and 100,000 rows globally; global capacity can
shorten an individual server's visible history. Each sample is at most 4 KiB
and 18 sources. Insert-time eviction is bounded, and age cleanup uses the
existing scheduled 500-row/two-second budget. Lifetime accounting receipt
cursors are never pruned with observations. Server deletion and site reset
remove history through their existing database lifecycle.

An empty range can mean no retained observations, an old agent, re-pairing or
retention; it does not establish zero traffic. Missing CPU/memory history stays
missing, and a recovered cumulative NIC delta does not supply past NIC rates.
No replayed batch is fed into current resource availability, service control,
configuration receipts or path health. Older agents continue normal accounting,
and older panels ignore the optional fields. This feature requires no higher
rollback floor than the existing release floor.
