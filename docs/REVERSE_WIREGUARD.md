# Reverse WireGuard connections

B initiates the WireGuard connection to A. A terminates the selected client
protocol, authenticates users, meters their traffic and sends business sockets
through the transport to B. The client protocol and the WG transport are
separate settings. Existing VLESS + REALITY connections remain compatible.

In the connection editor, select an inbound protocol, an explicit compatible
core, and its TLS/transport settings. Certificate requirements and client
subscription support are unchanged. Ordinary TLS + Vision is available for
VLESS; REALITY is not required.

| Entry protocol | Supported adapter |
| --- | --- |
| VLESS, VMess, Trojan, classic Shadowsocks | Xray or sing-box |
| SS2022, ShadowTLS-wrapped SS | sing-box / Extended, supported authentication mode |
| Hysteria2, TUIC, AnyTLS, Naive | sing-box / Extended |
| Snell multi-user | sing-box / Extended |
| SOCKS5, HTTP | sing-box / Extended |
| SSH, Mieru | explicitly selected sing-box Extended |
| WireGuard users | Xray with device accounting, version 26.6.27 or newer |

The actual node capability report determines availability. Selecting a protocol
does not install or upgrade a kernel. Native mita, hysteria and snell-server are
not enabled for this transport: their current adapters cannot preserve the same
per-user marking/accounting guarantees. They are never silently substituted.
Extended Mieru retains its wildcard-listen/native-quota restrictions; SSH is
TCP direct-tcpip only. Naive/SOCKS/HTTP bare-login accounting remains aggregated
per account when a core serves multiple such entries; no fictitious per-entry
user attribution is added. Subscription client compatibility is unchanged.

Choose **Create an independent connection** to create transport only. Use
**Attached inbounds** to bind existing inbounds on A. Each keeps its own
credentials, access policy and subscription entries. Choose its core explicitly
before binding. Shared entries must have identical private-access rules; the
panel never combines permissions into a broader ACL. Independent SOCKS5 and
single-key SS2022 can bind through sing-box, keeping their unbilled standalone
semantics. To edit a borrowed inbound, detach it, review/edit its configuration,
then bind and enable it again. Detaching disables it to prevent accidental direct
egress. Deleting a shared connection requires detaching its borrowed entries.

A complete TCP/UDP route can terminate at a WG-bound inlet. Its preview includes
B as an additional dependency. This represents a proxy termination at A followed
by WG egress; it does not insert an OS WG interface as an arbitrary relay hop.

Both nodes require the existing Linux root/WireGuard/routing/nft/tc readiness
checks and fresh reports. Core-only private access, per-ingress source egress,
opaque routing overrides and unsafe private ACL combinations remain rejected.
Managed source addresses bind both IPv4 and IPv6. Missing transport enforcement
must block traffic; it cannot fall back to A's ordinary Internet connection.
User WireGuard keys and addresses are separate from the server transport. Set
its MTU no higher than the transport MTU and avoid overlapping transport addresses.

Applying a protocol or binding change can restart the affected proxy process.
A recent handshake/configuration receipt is not a full client reachability test.
