# Server addresses, DNS synchronization and Mihomo dual stack

A registered server address, a DNS record and a reachable listener describe
three different things. Registering IPv6 does not publish an AAAA record or
open a port. A successful DNS check establishes only what the queried
resolvers returned, not whether clients can connect.

## Inspect addresses and DNS

The server list and Information page show IPv4 and IPv6 separately. Existing
untyped address values are classified by their actual family. Empty addresses
remain empty rather than being inferred from a domain answer.

The domain's IPv4 / IPv6 badges link to Diagnostics → DNS resolution health.
A and AAAA are queried independently through the panel resolver and
Cloudflare's public resolver. Each family retains its expected addresses,
answers, query errors and status. An absent, unconfigured family is neutral;
an unexpected record, mismatched answer, stale snapshot or failed query is
not presented as a successful check. Shared domains are checked for
resolution only, without claiming they point to this server.

“Recheck DNS” is read-only. The existing 30-second check cooldown remains;
repeated clicks do not accelerate failure confirmation or reset DNS caches.
DNS health runs in Captain and remains available for an unpaired server.

## Manually synchronize records

A full administrator can choose “Sync DNS records” in DNS diagnostics:

1. Captain reads the saved server domain and addresses, identifies its managed
   Cloudflare zone and reads the current records.
2. A review dialog shows each proposed A / AAAA creation, update or no-op,
   including the current and expected address.
3. Confirming submits the reviewed revision. Captain reauthorizes the request
   and rereads provider records before writing. A changed server configuration,
   credential or provider plan requires a fresh review.
4. Results distinguish success, failure and an unknown outcome. After a write,
   recheck resolution to observe propagation. Old health results are invalidated
   so they cannot be mistaken for post-write evidence.

Manual synchronization works when automatic DNS is disabled. It does not
change that setting, and later automatic writes remain disabled. The domain
must still be managed through Cloudflare with usable credentials. This action
synchronizes the saved server domain; it is not a general-purpose DNS editor.

Shared domains, conflicting ownership, proxied records and multi-record
address pools retain their existing protections. Empty address fields do not
cause record deletion. NAT / dedicated-line domains that refer to a different
public endpoint cannot be replaced with the server's own address through this
operation. Existing TTL values are preserved.

GET `/api/admin/nodes/{id}/dns-sync` only returns a reviewed plan. POST to the
same path accepts `{ "revision": "..." }`. Both require a full administrator;
API tokens additionally require the new `node-dns-sync` scope. Existing narrow
node scopes do not gain DNS write permission. Existing DNS history records
manual writes with their origin and durable before/after intent, without
credentials or raw provider errors.

Cloudflare does not offer a record compare-and-swap operation here. Rechecking
the plan protects against changes observed before the final write, but cannot
make a simultaneous edit in an external DNS console atomic. A request whose
result was lost must be reviewed again instead of blindly repeated.

## Publish a dual-stack subscription

In Subscription templates → Visual design, enable “Node dual-stack connection
selection (Mihomo)”. Saving a draft or preset keeps the choice; only applying
the selected format changes the published template. Existing profile and
response-rule template overrides retain their priority. The existing template
cache may take up to 15 seconds to reflect an applied change.

The generated Mihomo configuration enables `ipv6: true` and
`tcp-concurrent: true`. These are client-wide dialer settings, including
applicable direct connections. They do not force TUN, change `dns.ipv6`, or
replace an explicit node `ip-version` preference. Other client formats are
reported as unsupported for this enhancement rather than silently ignoring it.

For the feature to help, the subscription node's actual connection address
must be a domain with usable A and AAAA endpoints. A literal IP or TLS/REALITY
SNI alone does not provide dual-stack selection. Both listeners and firewalls
must allow the connection; the client also needs usable IPv6.

VLESS, VMess, Trojan, Shadowsocks/ShadowTLS, AnyTLS, Snell, SOCKS5 and HTTP(S)
use the relevant common TCP dialer. In audited Mihomo v1.19.33, Hysteria2 and
TUIC also use the common dialer around QUIC candidate connections. Mieru and
WireGuard can use either family but do not establish the same parallel
connection race. A successful dial is not a complete proxy health check, and
chained connections depend on their upstream dialer.

The companion bosun document `docs/DESIGNER_DUAL_STACK.md` contains the pinned
upstream source audit, transport qualifications and real-client test commands.
The standalone Snell adapter preserves its existing default IPv4 listener.
To expose Snell on both families, explicitly configure an appropriate listener
(such as `::` on a compatible host) and verify both paths. This subscription
option does not rebind existing server listeners.
