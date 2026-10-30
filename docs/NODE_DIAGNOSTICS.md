# Temporary host diagnostics

An administrator can open temporary, one-second CPU, memory and network-interface diagnostics for a server. These are passive host observations, not a connectivity test or a measure of proxy-user throughput. Business service pause and disabled ordinary monitoring do not prohibit an explicitly requested diagnostic lease.

Samples are kept only in Captain process memory: at most 120 samples per active server. They do not enter resource history, traffic accounting, alerts, ordinary heartbeats, configuration receipts or deployment evidence. Captain restart, site reset, re-pairing, process replacement and the last viewer leaving discard the ring. There is no database migration or offline recovery of a diagnostic lease.

The panel and node must both implement diagnostics protocol version `1`. Availability comes from a recent authenticated capability exchange, not a version string. Old agents continue their ordinary operations; old panels do not activate the new sampler. This feature does not change the existing rollback floor.

## Viewer API

The base is `/api/admin/node-diagnostics/{id}`. Full administrator role is required; operator, support and portal sessions cannot use this API. Personal API tokens additionally need the appropriate `node-diagnostics:read` or `node-diagnostics:write` permission and the existing `GET /api/admin/nodes/{id}` permission (`nodes:read` also grants it). Endpoint grants remain supported. Each lease belongs to the exact authenticated session or personal token that created it, not merely a staff ID.

| Request | Meaning |
| --- | --- |
| `POST {base}/leases` with `{"request_id":"<random UUID>"}` | Create an idempotent viewer lease. The request ID becomes the lease ID. |
| `PUT {base}/leases/{lease_id}` | Renew this viewer's unexpired lease. |
| `DELETE {base}/leases/{lease_id}` | Idempotently release it; `204` also covers an unknown own ID. |
| `GET {base}?lease_id=…&after=sequence` | Read newer samples without renewing. |

IDs must be 16–64 ASCII letters, digits, `_` or `-`; use a new random UUID for each start. A released ID has a five-minute tombstone so release can arrive before a delayed create. The UI can therefore release its known request ID on unmount even before a create response arrives, and must release again after a late successful create/renew response. The final bound after communication failure is the viewer TTL, followed by the node's shorter local lease bound. Do not reuse a released request ID.

Create and renew return:

```json
{"lease_id":"<request_id>","ttl_ms":30000,"renew_after_ms":10000,"interval_ms":1000}
```

Very early repeated renewals return the current remaining TTL instead of scheduling another timer. The browser renews every ten seconds and polls once per second only while visible and online. It stops renewal and best-effort releases on hide, unmount or request failure.

A snapshot contains `status` (`waiting`, `live`, `stale`, or `expired`), `node_id`, `interval_ms`, `fresh_for_ms`, `epoch`, `latest_sequence` and `samples`. `expired` has no samples. An unavailable supporting process prevents lease creation with `409 diagnostics_unavailable`; resource limits return `429 diagnostics_budget_exceeded`; another owner's lease or an invalid renewal returns `404 diagnostics_lease_not_found`. Polling cannot recreate an expired lease.

Each sample contains:

- `epoch`, `sequence`, RFC3339 `sampled_at` and actual `elapsed_ms`;
- nullable `cpu_percent`, `memory_used` and `memory_total` (bytes);
- nullable `net_up_bytes_per_second`, `net_down_bytes_per_second`;
- `interfaces`, each with `name`, nullable `rx_bytes_per_second` and `tx_bytes_per_second`;
- `truncated`, indicating the interface limit was reached.

The first sample, counter reset, missing source or sampling gap has `null` rates. Zero is a measurement, never a substitute for missing data. Sequence starts at one within an epoch, increases monotonically, and is at most JavaScript's maximum safe integer. The UI clears its local points and cursor when `epoch` changes. Normal control renewals preserve the epoch and ring.

A sample is fresh for at most three seconds, reduced by its age when received. The UI measures `fresh_for_ms` from the request's start using both wall and monotonic clocks, and revokes live status on offline, failed/paused fetch or deadline expiry. Historical points may remain visible while the current state is stale; their presence is not live evidence.

## Agent control and resource bounds

The existing State long poll advertises `X-Bosun-Diagnostics-Version: 1`, a fresh `X-Bosun-Diagnostics-Nonce`, the current `X-Bosun-Diagnostics-Challenge` and an `epoch:generation` `X-Bosun-Diagnostics-Cursor`. It uses the existing process `X-Bosun-Config-Session` identity. `State.diagnostics` is response-only metadata with `session`, `request_nonce`, `challenge`, `epoch`, `generation`, `remaining_ms` and `interval_ms`.

An unknown process first receives an inactive random challenge bound to the current diagnostic head. Only a subsequent echo can replace that head; a delayed first request or an old-parent challenge cannot retire the newer process. Process replacement changes the epoch and clears samples. This fence is independent of configuration projection support, so an opaque configuration does not prohibit passive diagnostics.

With viewers present, control refreshes about every five seconds and grants no more than fifteen seconds of node sampling. Lease creation, renewal, release and expiry have a separate wake channel. A metadata-only `200` preserves the ETag, full State revision, structural revision and config delivery, and cannot trigger core apply. The cached shared State is never modified. The node excludes diagnostic metadata from offline snapshots.

The node accepts control only when nonce, process session and authorization generation match. A repeated control generation cannot extend its deadline. The deadline starts at request dispatch, not response receipt; an over-ten-second long-poll response triggers one immediate fresh request instead of extending a stale command. A worker keeps its sequence across a temporary expiry within the same epoch, resets its rate baseline, and starts a new sequence only when the server changes epoch. `401`, `403`, close or expiry stop sampling and cancel pending transport. Business service-control pause does not stop this passive worker.

`POST /api/agent/diagnostics` accepts one authenticated sample, at most 8 KiB and sixteen interfaces, for the current active session and epoch. Duplicate, old, malformed, excessively frequent or inactive samples do not refresh the ring. Sample timestamps must be within ten seconds behind or three seconds ahead of panel receipt time; keep host clocks synchronized. No sample backlog is queued for later upload. Current viewer authorization is rechecked during node control, sample acceptance and viewer reads; revoked sessions, expired/deleted tokens, disabled/demoted staff and changed scope cannot keep a viewer alive. Explicit token start records last use; high-frequency reads and authorization checks are read-only.

Bounds are four viewers per staff principal, eight per node, 32 total and eight sampled nodes at once. Capability-only entries are bounded to 1024 nodes and pruned after inactivity. Candidate challenges, retired session markers, release tombstones and renewal timers are bounded as well. These are process-local budgets, not administrator-configurable sampling intensities.

## Validation

Tests cover shared viewers, budgets, sample limits/ring bounds, stale observations, process challenge races, epoch replacement, lease expiry, authorization revocation, release-before-create, safe integer cursors and concurrent access. HTTP tests check actual permissions, session/token ownership, reset with reused IDs, disabled monitoring and business pause. They verify diagnostic renew/sample/read operations make no SQLite changes and do not update ordinary node evidence, history, billing or config delivery; metadata-only state responses preserve configuration identity and do not leak into legacy responses. Node tests separately cover the passive sampler, missing/reset/gap rates, stale responses, local expiry, revocation, offline snapshot exclusion and no configuration apply for a diagnostic-only response.
