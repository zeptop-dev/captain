# Komari public probe wire contract

This package implements the **read-only public probe** contract of Komari
1.6.1, inspected at upstream commit
`96cf0f79d8cfdf6cc8be54fd1976bbc026d58f7f`. It is independently implemented
against `internal/probedata.Reader`; it does not embed the Komari server.

The embedding server must mount this handler only on an approved isolated
probe origin, supply a current authorization `Gate`, and provide a read-only
probe identity. `Handles` is an exact allowlist for routing. Management API
cookies, API keys, agent credentials, terminals, commands, plugins and Komari
account administration are outside this contract. Login/logout bridging is
owned by the embedding Captain server, not this package.

## Routes and methods

| HTTP route | RPC equivalent / response |
| --- | --- |
| `GET /api/nodes` | `public:getNodesInformation`, wrapped array |
| `GET /api/public` | `public:getPublicSettings`, wrapped settings |
| `GET /api/me` | `public:getMe`, raw identity |
| `GET /api/version` | `public:getVersion`, wrapped version |
| `GET /api/recent/{uuid}` | `public:getClientRecentRecords`, nested reports |
| `GET /api/records/load` | `public:getRecordsByUUID`, flat load records |
| `GET /api/records/ping` | `public:getPingRecords`, records/tasks/statistics |
| `GET /api/task/ping` | `public:getPublicPingTasks`, numeric task catalog |
| `GET /api/clients` | Legacy WebSocket `get` / `get {uuid}` |
| `POST /api/rpc2` | JSON-RPC 2.0 single requests, notifications and batches |
| `GET /api/rpc2` | JSON-RPC 2.0 WebSocket |

In addition to the eight REST-equivalent public methods, RPC exposes:

- `public:listMetricDefinitions`, `public:queryMetrics`,
  `public:getPingMetricStats`.
- `common:getNodes`, `common:getNodesLatestStatus`, `common:getMe`,
  `common:getPublicInfo`, `common:getVersion`, `common:getNodeRecentStatus`,
  `common:getRecords`.
- Framework reads `rpc.ping`, `rpc.version`, `rpc.methods` and `rpc.help`.
  Discovery lists only the methods this handler actually exposes; management
  methods are never advertised. Upstream 1.6.1 has no `rpc.discover` method.

Named parameters and the documented positional argument orders are accepted.
Legacy REST numeric strings and modern numeric JSON are accepted for hours
and point limits. Modern metric query aliases (`metrics`, `metric_keys`,
`metric_key`, time/aggregation/point aliases) normalize to one canonical query.
Public node/task selection is checked before any history query. Carrier task
IDs are mapped consistently between the numeric catalog, legacy history and
modern metric tags; their original Captain series identity stays namespaced.

## Explicit semantics

- `version` reports the compatibility target `1.6.1`. The actual Captain
  version is under `captain.version`, avoiding a false future-Komari feature
  capability inferred from Captain's independent release number.
- Missing/hidden/unsupported measurements are `null`, not measured zero.
  Optional Ping summary measurements follow Komari's omission convention;
  unknown percentiles, standard deviation and derived tail are absent instead
  of `null`. Canonical Captain fields retain their explicit unknown state.
  History summaries omit tasks without observed attempts; the public task
  catalog still retains those assignments. No attempts never means 0% loss.
  Real CPU zero remains zero. Unsupported history aggregations return a typed
  `unsupported_aggregation` error. Metric definitions describe history and
  aggregation availability.
- `net_total_up` / `net_total_down` and nested `network.totalUp` / `totalDown`
  retain interface-lifetime counter semantics. `captain.traffic` contains the
  independently measured billing period, validity and update time.
- Komari latest/recent flat `connections` is TCP + UDP; stored flat history
  uses TCP, matching upstream's existing difference. The exact TCP/UDP values
  and the semantic label are explicit under `captain`.
- Historical RAM/swap/disk totals are returned only when recorded with the
  historical sample. Current inventory is never used to manufacture them.
- Node metadata `created_at` / `updated_at` preserve the stored node row's
  timestamps. Runtime `time` and recent report `updated_at` remain the accepted
  measurement timestamp, independent of later node configuration edits.
  The latest-report map omits never-observed nodes while the node catalog
  retains them. Unknown latest load values are omitted as optional fields;
  the canonical status still carries their explicit unknown state.
- Ping means and loss use accepted attempt counts from the canonical reader.
  Unknown percentiles, jitter or standard deviation remain unknown. An
  aggregate point with complete loss uses the legacy `-1` marker; partial
  loss is separately preserved, rather than turning a failed attempt into a
  negative latency sample.
- Calendar billing cycles are projected to Komari's conventional display
  day counts. The exact decimal amount, currency and calendar cycle remain
  under `captain.info.cost`; these day counts do not change billing behavior.
- Public settings are explicitly projected. Theme metadata/configuration
  callbacks cannot expose arbitrary settings or inject global HTML/scripts.

## Resource and authorization limits

HTTP and WebSocket messages are limited to 64 KiB. Batches contain at most
32 requests; a query selects at most 128 nodes and 32 metrics, with at most
4,000 points per series and a 32,000-point aggregate query budget. History
windows are bounded to 730 days. Legacy unlimited `maxCount=-1` is bounded
to 4,000 output records. Larger positive per-series requests are clamped to
4,000 before reading history; the requested and effective limits are reported
under `captain`. The aggregate budget includes the selected node/task pairs
for ping metrics, and still rejects an oversized expanded query. There are at
most 32 concurrent WebSocket sessions
per handler, a two-minute idle timeout, a 15-second query deadline, and a
five-second write deadline.

Origin checks use the actual request Host, not `X-Forwarded-Host`. Every HTTP
request, batch item and WebSocket message checks the current gate. Before
each write it checks again; idle WebSockets also recheck every five seconds
and close when site/session access is revoked. No management method can be
registered dynamically. Responses are not cacheable.

Contract sources:
[routes](https://github.com/komari-monitor/komari/blob/96cf0f79d8cfdf6cc8be54fd1976bbc026d58f7f/web/router/router.go),
[public RPC](https://github.com/komari-monitor/komari/blob/96cf0f79d8cfdf6cc8be54fd1976bbc026d58f7f/web/rpc/jsonrpc/public.go),
[metric RPC](https://github.com/komari-monitor/komari/blob/96cf0f79d8cfdf6cc8be54fd1976bbc026d58f7f/web/rpc/jsonrpc/public.metric.go),
[common RPC](https://github.com/komari-monitor/komari/blob/96cf0f79d8cfdf6cc8be54fd1976bbc026d58f7f/web/rpc/jsonrpc/common.go).
