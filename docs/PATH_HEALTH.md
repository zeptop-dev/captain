# Complete-path protocol health

Path health is optional and disabled by default. It observes a complete path
route, such as entry A choosing B1 → B2 → landing D or C → landing D. A opens
the candidate's **existing business port** and performs a bounded protocol
handshake through the existing relays to D's actual inbound. No shadow chain,
extra listener, probe user or arbitrary URL is created.

A successful sample proves this particular handshake from A through that
candidate to the landing port. It does not prove client reachability to A,
customer authentication, every transport above TLS, UDP or Internet egress.
The ordinary next-hop TCP probe remains a separate observation.

## Configure and observe

Edit a complete path route and enable its health policy. The entry server must
run a supporting bosun (v0.85.0 or newer) and have a recent capability report.
Upgrading alone does not enable the feature. Older agents and missing evidence
cannot produce a healthy result.

`health` on the route contains:

| Field | Default | Meaning |
| --- | --- | --- |
| `enabled` | `false` | Observe protocol handshakes through each actual candidate. |
| `auto_failover` | `false` | Restrict new connections to candidates confirmed up. |
| `server_name` | empty | Explicit TLS SNI and certificate identity; never a dial target. |
| `interval_seconds` | 15 | Delay between rounds, 10–300 seconds. |
| `timeout_seconds` | 3 | Maximum handshake duration, 1–10 seconds. |
| `failure_threshold` | 3 | Consecutive failures before removing an up candidate. |
| `recovery_threshold` | 2 | Consecutive successes before admitting a candidate. |

For standard TLS, an explicit DNS server name overrides the landing inbound's
unambiguous configured TLS name. Without a verifiable name, the probe is
unsupported. Certificates must validate with the entry host's trusted roots;
there is no insecure toggle. ALPN comes from the actual landing TLS settings.
REALITY and ShadowTLS decoy responses are not accepted as path evidence.

Omission or JSON `null` preserves the existing policy when an older editor
saves a route. An explicit `enabled:false` disables it. Same-value saves and
automatic address recompilation keep the policy even if capability evidence
has temporarily expired. Enabling, changing the policy or moving the entry to
another server requires a fresh capability check.

The health panel displays current-route candidate observations and their
individual freshness. A failed request, expired sample, replaced server
identity or new agent process cannot retain a green result. During staged
publication, old and new entry rules have separate identities; evidence from
the unreleased or unpublished entry cannot confirm the currently published
route. A configuration application receipt remains different from a successful
path handshake. When a staged route enables automatic failover, publication
also waits for at least one fresh eligible candidate from the actual newly
released entry rule after the expansion configuration has been acknowledged.
The old published entry remains in place while waiting; the entry deadline
pauses the plan if health never becomes ready. Monitoring-only routes do not
add this gate. A completed rollout is still not a claim about authentication or
Internet egress.

## Automatic failover

Automatic selection is supported only for native TCP relay with the failover
strategy and a supported handshake. Monitoring alone preserves existing
forwarding behavior, including its ordinary last-resort fallback.

With automatic failover enabled, a new rule starts unknown. It admits no new
connections until a candidate reaches the recovery threshold. Consecutive
failures remove a candidate at the configured failure threshold. While that
threshold is pending, the panel shows the latest failed observation as degraded
even if the candidate remains eligible. Recovery requires consecutive new
successes. Fresh up candidates are tried in declared order.

If every candidate is down, unknown or stale, new connections are rejected.
There is no fallback to a failed candidate or direct connection to the landing.
Health transitions do not interrupt established streams. Normal service pause,
configuration replacement and actual network failure still affect them.
Disabling automatic failover restores ordinary selection; disabling health
stops its probes. Existing service-control guards remain authoritative.

Captain unavailability does not prevent a still-authorized agent from sampling
locally. Its local selection follows those samples, while the panel's old
evidence expires. Offline recovery cannot acknowledge a newly delivered state.
An expired or revoked service lease stops service and probe activity through
the existing agent lifecycle.

## API and permissions

- `GET /api/admin/path-routes/{id}/health` requires administrator role and the
  independent `path-health:read` token grant.
- `PUT /api/admin/path-routes/{id}/health` accepts `{version,health}` and requires
  `path-health:write`. It changes only the health policy with the route's CAS.
- Changing health through direct route edits, staged plans, removal or
  compensation requires the same additional write grant. A route or rollout
  grant alone cannot change health policy. Unchanged or omitted policy does
  not require an extra grant.

Responses expose fixed, non-secret observations: current entry tag and
candidate digest, sample epoch/sequence/time, latest result, hysteresis status
and remaining freshness. Report reception does not replace the probe time.
Browser clients must count down `fresh_for_ms` locally and withdraw positive
state when requests fail or freshness expires.

## Protocol and validation boundaries

| Landing configuration | Probe | Evidence |
| --- | --- | --- |
| Standard TLS with verifiable SNI | TLS ≥1.2 with trusted chain and name validation | TLS listener at the end of this candidate. |
| Plain authenticated HTTP proxy | Fixed `OPTIONS *`, require 407 and `Proxy-Authenticate` | Proxy authentication challenge, without sending credentials or a destination. |
| Plain authenticated SOCKS5 | Offer username/password method (`05 01 02`), require `05 02` | SOCKS5 authentication-method response, without authentication or CONNECT. |
| REALITY, ShadowTLS, raw VLESS/VMess/Shadowsocks/Snell/Mieru and other unsupported handshakes | None | Unsupported, never healthy by a first-hop TCP connect. |

Protocol response tests cover Xray 26.3.27-r1 and official sing-box 1.14.1-r2
on Linux, plus Xray 26.3.27 and sing-box Extended 1.14.1-2.7.2 on Darwin:
HTTP 407 with authentication header, SOCKS5 `05 02`, valid TLS trust/name,
wrong-name rejection and untrusted-certificate rejection. These direct-core
tests establish handshake compatibility, not user authentication or egress.
Separate native-relay tests traverse two unequal actual TCP branches and cover
a failed deep hop while the first-hop port stays open, recovery, all-down
rejection and preservation of an established connection.

Probe connections create no authenticated customer session and do not enter
the user traffic ledger. Their small real network/relay byte counts naturally
appear in server NIC and relay statistics. They are not free supplier traffic.
Probes can use private addresses only through the explicitly compiled topology;
the policy has no arbitrary destination, request body, URL or proxy credentials.
