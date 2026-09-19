# Nodes, inbounds and the paths traffic takes

How a server becomes a node, what it serves, and how traffic gets in and
out of it. Version notes name the bosun release a feature needs; Captain
1.2 ships against bosun 0.49.

## Adding a node

Admin → Nodes → new node gives you a one-time pairing code and two ways to
use it:

```sh
curl -fsSL https://panel.example.com/api/agent/install.sh?pair=CODE | sh
```

or, with Docker, a `docker run` line carrying `BOSUN_CAPTAIN` and
`BOSUN_PAIR`. The installer fetches bosun, writes
`/etc/bosun/config.yaml`, creates the service, pairs with the panel and
applies the state it receives. A node that pairs with an empty public
address gets it filled in from the address it paired from.

A node can be given a **host name** (Node → Domain, e.g.
`jp1.example.com`): new inbound recipes use it as the TLS name and entries
advertise it instead of the address, so a certificate for that name reaches
the node with no further setup.

Nodes learn about changes within seconds: bosun keeps a long-poll request
open on the state endpoint, so nothing has to be pushed and no persistent
connection is needed. The node page shows host metrics, the cores' status,
the doctor's verdicts and which inbounds were not applied and why.

**Node jobs** run from the console: upgrade to a release, roll back to the
previous build, scan REALITY targets, run a speed test. Each is recorded
with its result.

## Inbounds

An inbound is one listening service on one node: protocol, port, listen
address, transport and its settings. Quick-setup recipes fill in a working
configuration for VLESS+REALITY, Hysteria2, mieru, Shadowsocks 2022,
SS2022 + ShadowTLS and Trojan+WS, generating any keys server-side so an
API-created inbound cannot break a node.

Supported protocols: VLESS (+ REALITY), VMess, Trojan, Shadowsocks
(including 2022 ciphers), Hysteria2, TUIC, AnyTLS, mieru, Snell, SOCKS,
HTTP, NaiveProxy, WireGuard. Which core serves which protocol is bosun's
decision; see its [README](https://github.com/zeptop-dev/bosun).

**Scoped users** — an inbound bound to a user group only provisions that
group's users; an ungrouped inbound gets every user with a usable
subscription. A user whose plan expires or runs out of quota disappears
from the node's desired state, which is what stops their traffic.

**Port conflicts** — saving or enabling an inbound whose transport and port
is already taken by another enabled inbound or by a forward on the same
node, on an overlapping bind address, is refused with 409 instead of
failing on the node. UDP is counted for Hysteria2, TUIC and WireGuard, both
for Shadowsocks and Snell, and mieru per its transport (a `BOTH` inbound
also reserves `port + 1`).

**Protocol knobs worth knowing**

- **REALITY** — the target (`dest`) can be scanned from the node: Admin
  runs a scan job and gets a list of candidates with real TLS 1.3 / h2 /
  X25519 / certificate checks and CDN detection, then picks one. A node can
  also serve its own HTTPS *decoy* site on loopback and use it as the
  target, so a prober sees a genuine certificate for your name.
- **ShadowTLS** — a Shadowsocks inbound can be wrapped in ShadowTLS v3
  (the *SS2022 + ShadowTLS* recipe, or the switch on any Shadowsocks
  inbound). The public port performs a real TLS handshake with a site you
  name — `www.apple.com:443` by default — and only an authenticated client
  is handed through to the Shadowsocks inbound, which moves to loopback: a
  prober sees that site's certificate and nothing else. Every user gets
  their own ShadowTLS password derived from their UUID, on top of the
  Shadowsocks key, so revoking a user revokes both. Strict mode (on by
  default) refuses a ClientHello the site itself would not accept.
  sing-box only — Xray has no ShadowTLS, REALITY is its answer to the same
  problem — and the subscription carries it for mihomo, Stash, sing-box,
  Surge, Loon and Shadowrocket. Needs bosun ≥ 0.48.
- **Snell** — served by sing-box (bosun ≥ 0.41; its Snell server speaks
  v5, obfs http). With *Multi-user (sing-box)* every user connects with
  their own key and traffic is accounted per user, but only sing-box
  clients can present a user key, so such lines appear in sing-box
  subscriptions only. Obfs tls still needs Surge's own `snell-server`.
- **mieru** — MTU, multiplexing level and handshake mode per inbound reach
  the `mierus://` links and the mihomo/Stash lines. Transport `BOTH` serves
  TCP on the port and UDP on `port + 1`; links list both and mihomo takes
  TCP.

## Entries

An entry is what a user sees in their client: a display host and port on
top of a landing inbound. That indirection is what lets a relay, a line
ingress or a domain stand in front of the same inbound. Tags, regions,
ordering and auto flags are described in
[SUBSCRIPTIONS.md](SUBSCRIPTIONS.md#entries-what-each-user-sees).

## Port forwards (relay tunnels)

Node page → Port forwards: listen on a port of this node and relay raw TCP,
UDP or both to a landing server. Clients connect to the relay while the
landing inbound keeps doing authentication and per-user accounting. Pick
another managed node's inbound as the target and one click creates an entry
advertising this relay's address; the node reports each rule's
reachability, RTT, connection count and bytes.

A rule's backend is one of:

| Backend | What it is | Trade-off |
|---|---|---|
| built-in relay | bosun's own userspace relay | connection and byte counters, PROXY protocol support |
| `nft` | nftables kernel DNAT (bosun ≥ 0.18) | fastest, IPv4 target, optional source preservation when replies route back through the node; no counters |
| `realm` | bosun installs and runs [realm](https://github.com/zhboner/realm) | high throughput, hostname targets, UDP; no counters |

**Several targets.** A rule can have further targets (the split-arrows
button on the rule): a backup line for when the first is down, or more
lines to spread connections over. *Failover* sends new connections to the
first target whose health check passes, and a connection whose dial fails
moves on to the next target before the client notices; *round-robin*
spreads connections over the healthy targets by weight. Each target's
health, RTT and connection count shows next to the rule. Built-in relay:
both modes; realm: round-robin only (it does not retry another target);
nft: one target. A connection always uses one target, so this keeps a
relay up and spreads load across lines — it does not make one download
faster. Needs bosun ≥ 0.49.

Ports are checked against the node's own inbounds. Xray-style domain/IP
splitting inside a tunnel is deliberately not offered: use the landing
node's routing rules instead.

**Device counting behind a relay.** A relay hides the client's address from
the landing node, so by default every connection arriving from one of the
panel's own nodes counts as *one* online device (and is marked "via relay"
in the user drawer). For the real per-client picture, tick *Expect PROXY
protocol* on a landing inbound that is only ever reached through this
panel's forwards (xray only): forwards targeting it then send a PROXY
protocol v2 header automatically (built-in relay or realm backend), the
landing node sees the real client, and devices are counted exactly. Direct
connections to such an inbound fail, by design.

## Line ingresses (IPLC)

A node behind an IPLC or a dedicated line has more than one way in. Node
page → Line ingresses registers each line with the addresses the provider
gives you:

- the **local NIC address** on the VPS — inbounds bind to it so replies go
  back through the line;
- the line's **far-end address** — what a relay must forward to; not
  reachable from the public internet;
- the provider's **public entry**, if the service includes one (a China
  Mobile entry address, for example);
- the usable **port range** and an optional port offset.

Inbounds pick an ingress (direct is the default, or the line on nodes with
no public address). Any protocol may ride a line, and a recipe applied
while an ingress is selected takes the first free, non-reserved port of the
range. Whether a given protocol passes is up to the provider's entry — some
carrier entries only pass non-TLS protocols such as mieru. Entries then
advertise the public entry on the mapped port.

A line **without** a public entry is served through a relay node: add a
port forward there whose target is the far-end address (the picker fills it
in) and use the relay's address in the entry. Direct inbounds on the same
node (Hysteria2, REALITY) keep using the node's own address or domain.

With the probe on, every ingress that has both a local NIC address and a
far-end address gets an automatic RTT task (bosun ≥ 0.15 binds the TCP
connect to the NIC; even a refused port measures the line), shown under the
ingress name on the status and speed-test pages. Lines are usually private,
so carrier latency is not measured through them.

## Outbounds, landing and egress

**Outbounds & landing** (node page): add an exit from a share link — bosun
renders it in the serving core's dialect (sing-box takes every protocol,
Xray vless/vmess/trojan/ss/socks/http) — chain exits with `via`, then pick
a default exit for the whole node or add rules such as `inbound:tag`,
`domain:`, `ip:`, `protocol:`, `port:` → outbound / direct / block. Needs
bosun ≥ 0.12.

**Egress follows ingress.** On a node with several public addresses, the
node option *Egress follows ingress* makes every inbound that is bound to a
specific address send its users' traffic out from that same address
(sing-box, xray and hysteria; mieru cannot). Give each inbound its bind
address; any-address inbounds keep the default route, and a default landing
outbound takes precedence. Needs bosun ≥ 0.44.

**Private destinations are refused.** A node's own neighbourhood —
loopback, link-local, cloud metadata, RFC 1918, CGNAT — is rejected for
user traffic in the cores' own routing and again by an nftables egress
guard, and the cores' unauthenticated control APIs are reachable only by
root. Details in bosun's README; the live regression checks it on every
release.

## Domains and certificates

Admin → Domains & certs registers the domains you own — each with
Cloudflare as the DNS provider, using the global token from Settings → ACME
or its own token when the zone lives in another account, or "manual" —
shows what uses each one (node host names, inbound TLS names, subscription
hosts, the panel itself) and manages certificates:

- **Issue** — Let's Encrypt through DNS-01 on the panel, one certificate
  for any set of names (`example.com` plus `*.example.com` together is
  fine), stored in the database and renewed 30 days before expiry; the
  first failed renewal notifies the admin. The Cloudflare token stays on
  the panel: nodes need neither a token nor port 80.
- **Upload** a PEM pair, or let a certificate manager (Certimate, an
  acme.sh deploy hook) POST renewals to the per-panel webhook shown on the
  page. The JSON keys are lenient: `domain`/`domains`, `certificate`,
  `privateKey`/`private_key`.
- **Deploy by coverage** — every node whose standard-TLS inbounds use a
  covered name (exact or wildcard) receives the pair in its state; bosun
  (≥ 0.13) uses it ahead of ACME and lists it as method `custom`. Deleting
  a certificate lets the nodes fall back to node-side ACME.

**Automatic DNS records.** A registered Cloudflare domain with *Auto DNS
records* on (the default) gets A/AAAA records created or updated whenever a
node with a host name under it is saved (node domain → public / IPv6
address), or a line ingress with an *entry domain* is saved (entry domain →
the provider's entry address). Records are never deleted and never proxied,
and the outcome is shown in a toast. The token needs DNS edit permission on
the zone, which the DNS-01 token already has.
