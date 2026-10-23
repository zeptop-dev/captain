# Nodes, inbounds and the paths traffic takes

How a server becomes a node, what it serves, and how traffic gets in and
out of it. Version notes name the bosun release a feature needs; Captain
1.2 ships against bosun 0.49.

## Groups and display order

Use **Servers → Groups and order** to create, rename, delete and reorder monitoring
groups. Drag a handle, use the up/down buttons, or focus a handle and use
Space plus arrow keys. Select a group to reorder its nodes; select several
nodes to move them together. Changes remain a draft until **Save**. **Cancel**
discards them. A concurrent node creation, deletion or layout change rejects
an outdated save; cancel and reopen to load the latest layout.

Node management and the public status page share this order. Groups follow the
configured sequence, with Ungrouped last; nodes retain their configured order
within each group. Migration preserves the existing node order until the first
layout edit; new nodes append at the end. Deleting a group moves its nodes to
Ungrouped without deleting them. Empty groups remain available in administration
but do not appear publicly. Node details also offer a single-node group selector;
moving a node appends it to the destination group.

Group names are public on the status page. They are independent of user and
billing groups. Hidden and unpaired nodes remain excluded from the public page;
changing groups does not change visibility. Traffic/peak or visitor-selected
column sorting temporarily overrides the configured default order. Operators
and administrators can edit layout; support users cannot. No bosun update is
needed. The legacy `/api/admin/nodes/{id}/monitor-group` endpoint remains valid;
`GET/PUT /api/admin/nodes/layout` adds revision-checked bulk layout management.

## Adding a node

Admin → Servers → Add server gives you a one-time pairing code and two ways to
use it:

```sh
curl -fsSL https://panel.example.com/api/agent/install.sh?pair=CODE | sh
```

or the generated Docker Compose installer command (`--mode docker --web-upgrade`). The installer fetches bosun, writes
`/etc/bosun/config.yaml`, creates the service, pairs with the panel and
applies the state it receives. A node that pairs with an empty public
address gets it filled in from the address it paired from.

### Monitor-only nodes (Captain 1.18 / bosun 0.66)

Select **Monitor only** when adding a node. The generated installer appends
`--monitor-only` for both native and Compose installations. It installs bosun
without downloading or initializing proxy cores. Native installs also skip
installing nftables/iproute2 solely for proxy features. Host resource collection,
interface traffic accounting, latency probes, alerts, the public status page and
bosun upgrades remain available.

Nodes have a mode badge/filter and a mode card in their details. Full
administrators can switch modes. Existing nodes remain full nodes; switching
requires a fresh mode-capable report (bosun v0.66.0+). Saving the requested mode
is not proof it is running: the card shows the last reported mode and errors.

Before switching a full node to monitor-only, remove all inbounds (even disabled
ones), forwards, managed reverse links and node proxy policies/overrides. Wait
for queued jobs to finish. Historical monitoring data and downloaded core files
are retained; this action is not an uninstall. The agent stops cores, checkpoints
traffic, clears its proxy network helpers, persists the mode and restarts.

Switching back enables the configured core adapters after restart. Full mode
needs the normal host prerequisites, including nftables/iproute2 for isolation
and shaping; missing prerequisites are reported by the doctor. If a manually
written monitor configuration has no core definitions, add them before switching.
The UI enables proxy controls only after the agent confirms the requested mode
and revision. The API and agent independently refuse proxy state and core/WARP
jobs while monitoring; global users, proxy credentials and proxy configuration
are never included in monitor-only desired state.

The runtime mode is stored in the private `data_dir/node-mode` file so Docker
read-only config mounts work. Requests and acknowledgements have monotonic
revisions; the local revision is bound to the authenticated panel identity so
re-pairing cannot inherit another panel's generation. Late requests cannot undo
a mode change. Once enabled, the updater refuses releases older than v0.66.0,
which would ignore this guard. New fields are additive; old full-mode nodes
continue working without mode support.

A node can be given a **host name** (Node → Domain, e.g.
`jp1.example.com`): new inbound recipes use it as the TLS name and entries
advertise it instead of the address, so a certificate for that name reaches
the node with no further setup.

Node host names are checked when creating or editing a node. Names differing
only by case, a trailing DNS root dot or equivalent internationalized spelling
are treated as the same name. An exclusive name already used by another node
is rejected with that node's name; editing a node does not conflict with itself.

For intentional DNS round robin, CDN or load balancing, enable **Shared domain /
externally managed DNS** below the domain field. This permits reuse and stops
Captain from automatically changing A/AAAA records for that name across all
nodes and line ingresses. Existing records remain in place; manage DNS and
compatible protocols, ports and credentials yourself. Sharing a name does not
configure a CDN or synchronize the proxy configuration.

Existing duplicate names are retained and marked in the node list and editor.
Their automatic DNS updates are paused, but their other fields remain editable.
Resolve the duplicate or explicitly select shared DNS. Older API clients that
omit or send null for `DomainShared` preserve the saved mode. Returning a shared
node to exclusive mode is refused while another node uses the name. The rule
applies to node host names, not certificate names, TLS SNI or REALITY targets.

Nodes learn about changes within seconds: bosun keeps a long-poll request
open on the state endpoint, so nothing has to be pushed and no persistent
connection is needed. The node page shows host metrics, the cores' status,
the doctor's verdicts and which inbounds were not applied and why.

**Node jobs** run from the console: upgrade to a release, roll back to the
previous build, scan REALITY targets, run a speed test. Each is recorded
with its result.

## Removing a node

Node → Delete asks for the node name and offers three actions:

- **Keep bosun in standalone mode** imports the currently provisioned inbounds,
  users, per-inbound access and limits, routes, certificates and forwards, then
  restarts bosun with the local driver. UUIDs/passwords and immutable traffic
  identities remain. Captain's plans, balances, history, expiry schedule and
  subscription URLs stay in Captain; standalone subscription URLs are new.
  Existing local administrator credentials remain. A headless node gets a panel
  on `127.0.0.1:2053`: use SSH to set its password with
  `bosun admin set -user admin -password 'NEW_PASSWORD'`, restart bosun, then use an
  SSH tunnel or configure a reverse proxy. Imported per-inbound limits remain
  until that user is edited locally; configure ongoing quotas/expiry locally.
- **Uninstall bosun** stops the agent and its cores, removes its service, binary
  and `/etc/bosun`, and cleans up its own firewall/forwarding/shaping rules.
  `/var/lib/bosun` is removed unless **Keep local data** is selected. This does
  not uninstall OS packages or change SSH, interfaces or unrelated firewall rules.
- **Only remove the Captain record** retains the legacy behavior, including for
  offline nodes: delete the node and its inbounds without contacting bosun.
  The node may continue serving the cached configuration in managed mode.

Remote actions require a full administrator, an online paired node running
**bosun ≥ v0.55.0**, and the standard Linux systemd/OpenRC installation paths.
Docker containers and custom installation paths must be managed on the host.
The worker runs outside bosun's service so it can report after stopping it.
Captain deletes its record only after a successful result. Failures/timeouts
retain the record; reopen the dialog to see or resume the same task. An unclaimed
request expires after five minutes, so a returning offline node cannot execute
an old uninstall. A standalone startup failure restores the managed files and
attempts to restart the original service.

API: `POST /api/admin/nodes/{id}/removal` accepts
`{"mode":"standalone"}` or `{"mode":"uninstall","keep_data":true}` and returns
`{"id":"JOB_ID"}`. `GET` on that path returns the latest job or `null`.
After `done_at` is set, `error` is empty and `result.phase` is `complete`, use
`DELETE /api/admin/nodes/{id}?removal_job=JOB_ID`. The original DELETE without
that query still removes only the record. Workers authenticate their claim and
completion at `POST /api/agent/removal` with the existing node credential.

If result delivery fails, the node retains a root-only plan/result under
`/var/tmp/bosun-removal-JOB_ID/`. After inspecting the worker service/log and
confirming it is no longer running, rerun its staged worker with
`/var/tmp/bosun-removal-JOB_ID/worker internal-node-removal /var/tmp/bosun-removal-JOB_ID/plan.json` to deliver
an existing `result.json` without repeating the operation. Keep this directory
private: it contains the node credential. Do not remove a `running.lock` while
its worker is alive.

## Inbounds

Open **Nodes → Manage inbounds**, then select the server. Server details show
configured listeners and reported runtime status, with links to this editor.

The **Core** selector filters choices by protocol, transport and features such
as Shadowsocks 2022, ShadowTLS, PROXY protocol, fallbacks and Snell mode.
**Automatic (recommended)** follows the node's configured core priority;
REALITY prefers Xray. A manual choice is retained when settings change; an
incompatible choice must be corrected before saving.

With bosun ≥ 0.55.0, the form previews automatic selection and greys out
compatible cores that the node has not enabled. Enable those in the node's
`config.yaml` first; selecting a core does not install or enable it. Idle
cores are available even while stopped. Older, offline or unreported nodes
show unknown availability and validate when applying the configuration.
The list separates the configured choice from the last reported running core.
A failed apply has no confirmed running assignment. Existing `Core` values
and the default selection order are preserved; no database migration is needed.

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
  With bosun v0.61.0 or later, the scanner also measures incoming TLS wire records, including headers,
  encrypted overhead and stapled OCSP data. Records over the conservative
  8192-byte limit used by the pinned Xray are rejected by automatic selection.
  Older bosun versions without this check show **Not checked**, not a pass.
  This is basic screening, not a full REALITY connection: accumulated records,
  a different ClientHello or a different destination edge can still fail.
  Confirm candidates with the node's actual core and client.
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

### Native WireGuard devices

A regular subscription retains one WireGuard identity per customer and inbound.
Use **WireGuard device subscriptions** on the portal home page or in the user's
admin detail drawer to give a laptop, phone and router independent peers. Each
device has its own subscription link; sharing one device's configuration across
independent clients still shares the same peer. All of a customer's devices
remain on the same account for usage and limits. This feature is separate from
the managed A-to-B reverse tunnel below, whose peers represent servers.

Use bosun v0.77.1 or later and the supported Xray `26.7.28-r3` package.
In **Core management**, download that version and explicitly activate it;
upgrading the panel or agent does not change the default core package. Wait for
the node's capability report before creating devices. An unavailable or old
core is not treated as ready merely because the server is online.

Adding or revoking peers must synchronize to the server and can restart the
shared Xray process, interrupting other connections served by that instance.
Revoking a device invalidates its link immediately, but downloaded credentials
stop working only after successful synchronization. An offline server cannot
apply the change yet. Renaming a device changes its label without creating a
new peer. See [device formats, limits and reset behavior](SUBSCRIPTIONS.md#independent-wireguard-devices).

## Entries

An entry is what a user sees in their client: a display host and port on
top of a landing inbound. That indirection is what lets a relay, a line
ingress or a domain stand in front of the same inbound. Tags, regions,
ordering and auto flags are described in
[SUBSCRIPTIONS.md](SUBSCRIPTIONS.md#entries-what-each-user-sees).

## Server settings and lines

Server details contain **Server proxy settings**: NAT/dedicated-line address
and port mappings, outbounds and routing, and advanced per-core overrides.
These settings apply to the selected server. NAT mappings describe existing
provider configuration; saving them does not create provider-side NAT rules.
Administrators and operators retain their existing editing permissions;
egress upstream exceptions remain administrator-only. Unpaired and
monitor-only servers cannot edit proxy settings.

**Nodes & lines → Lines** contains forwarding lines, reverse WireGuard and
VLESS Reverse. Forwarding lines separate **Forwarding groups** from
**Single port forwarding**. Server details show these connections and link
to their line editors. Saved links to the former server-setting sections
under `/entries` redirect to the corresponding server details.

## Managed server lines

**Nodes & lines → Create line → Port forwarding** builds an ordered series of
server groups ending at an existing or newly created inbound. Each member has
its own listener port and optional NAT/IPLC ingress. The compiler creates the
owned raw forwards on those servers; clients still authenticate at the final
inbound. Creating the line and publishing a subscription node are separate
steps, so one line can be reused by several subscriber-visible nodes.

Select a first-group member when publishing a node. Multiple entry members do
not automatically configure DNS load balancing. Later groups support ordered
TCP failover or weighted round-robin with the built-in relay. UDP/both
multi-candidate groups allow round-robin only: the existing TCP probes do not
establish UDP health. Established connections remain on their selected target.
TCP failover uses next-hop TCP dial results. If that listener accepts the
connection but its downstream path fails, the preceding relay does not retry
another member; it does not test the entire downstream path.
The first version accepts at most eight groups, eight members per group, and
no repeated server. It does not implement least-connections, remaining-quota,
lowest-latency or calendar-based scheduling.

`relay`, `nft` and `realm` are forwarding backends, not different kinds of
reverse tunnel. This editor permits multiple next-hop candidates only with
`relay`. Managed `nft` lines require bosun ≥ 0.62 and a destination address in
the same family as the listener; hostname destinations are resolved by the node
when applying the rule. `realm` is a managed userspace process. Listener/protocol,
ingress ranges, reserved ports, existing listeners, monitor mode and agent version are checked before saving. A target
using mieru BOTH needs a dual-port model and is not supported by this editor.

For nft lines with known literal entry addresses, the compiler selects a
matching wildcard listener and rejects cross-family forwarding. A realm member with
a known IPv6 entry gets an IPv6 wildcard instead of realm's IPv4-only default.
An explicit local ingress bind is preserved; a public NAT address is never
assumed to be assigned to the server. Unknown hostnames are not resolved during
saving and still require successful node application and connectivity checks.

Realm 2.9.6 UDP uses only the first DNS answer, although its TCP dialer can try
the other answers. For managed realm UDP/both lines, ordinary next-hop servers
therefore use a registered IP in a family shared with the sending server (IPv4
when both families are registered, otherwise IPv6). This applies to intermediate
hops and the landing; subscriber-visible domains remain unchanged. Node address
edits and confirmed automatic address following recompile these targets in the
same transaction. Address inventory is a selection preference, not proof that
the operating system has a usable route.

Shared/external node domains and explicit ingress addresses are preserved,
including their mapped ports. If a domain intentionally leads through an
external relay or CDN, declare that path as an explicit ingress or mark its DNS
as externally managed; an ordinary non-shared node domain may otherwise be
replaced by its registered direct IP for this runtime path. With unknown or
disjoint address inventories, the original target is retained. Domain-only or
external realm UDP targets still require a usable first DNS answer and a real
UDP connectivity test; no global IPv4 setting or DNS lookup is added at save time.

Kernel DNAT also requires the host's **FORWARD** policy to permit the traffic.
Automatic listener-port opening does not manage these routed allowances;
UFW routed-deny policies, firewalld or Docker forwarding rules can still block
a saved nft line. Configure scoped forwarding rules separately. The WireGuard
automatic-forwarding option applies only to its own WG connections.

Line edits update owned forwards and all associated subscription endpoints in
one database transaction. Existing individual forwards remain independent and
are never silently adopted. Owned forwards must be edited through their line;
used lines and resources cannot be deleted accidentally. Updates carry a
version so an outdated editor cannot overwrite another administrator's work.
Runtime identities depend on the compiled rule, so renaming a line does not
restart its forwarding sessions.
Status describes each forward's **next-hop TCP probe**, not a full end-to-end
proxy test; UDP is explicitly untested. Agent application and real connectivity
must still be checked after saving.

The same page offers **Reverse WireGuard** and **VLESS Reverse**, reusing their
existing capability checks and ownership rules. These are complete reverse
connections with A-side authentication/accounting and B-side initiated tunnels,
not arbitrary edges in a multihop forwarding graph. Their creation and editing
are centralized here; server details keep read-only summaries of these connections.

## Port forwards (relay tunnels)

### Reusing a server's internal address

Register the server's existing internal IP once in **Servers → Edit → Internal
address**. Captain does not assign that IP or join a provider network. When
creating a forwarding line, explicitly choose **Server internal address** for
each subsequent-group member or the final landing. The preceding server then
connects to that server's registered internal IP and actual listener port,
without applying the public port mapping. The preview shows the chosen
destination. First-group subscription endpoints retain their public addresses.

**Default address** preserves the existing public/domain or dedicated-ingress
selection. Merely filling an internal address never changes an existing line.
An internal selection has no automatic public fallback. The destination must
listen on that internal address or a compatible wildcard; an explicitly
different bind is rejected. Intermediate members reached internally bind that
address when no ingress bind is set. Address registration is not evidence of
network reachability or of the provider's traffic charging policy.

Single port forwarding supports the same choice when the target is a managed
inbound. Internal mode follows that inbound rather than a hand-written address;
it cannot be combined with untracked additional targets. Use forwarding groups
for several managed candidates and select their connection modes individually.
Changing an internal address refreshes dependent targets atomically. The server
editor lists affected lines/forwards; clearing a referenced address or changing
its listener incompatibly is rejected. Removing referenced resources requires
first editing or removing those forwards/lines.

Full administrators can run a TCP connection check **from the preceding
server** using the existing diagnostic jobs (bosun ≥ 0.56). Save and apply new
listeners first. A successful root-agent check does not verify proxy
authentication, UDP, or the forwarding backend's permissions. UDP requires a
real client test. Built-in relay runs in bosun; realm runs as the proxy account
and needs a matching, narrowly scoped **Private upstream exception** on the
preceding server for a private target. nft still needs the host's FORWARD rules
to allow the path. These permissions are not broadened by selecting an address.

The internal-address field also retains its online-device purpose: known relay
source IPs are marked **via relay**, and all such sources for one user count as
one device. It does not recover the number of actual clients hidden behind a
relay or select the agent's connection to Captain.

### Standalone forwarding rules

**Nodes & lines → Lines → Forwarding lines → Single port forwarding**: listen on a port of this node and relay raw TCP,
UDP or both to a landing server. Clients connect to the relay while the
landing inbound keeps doing authentication and per-user accounting. Pick
another managed node's inbound as the target and one click creates an entry
advertising this relay's address; the node reports each rule's
reachability, RTT, connection count and bytes.

A rule's backend is one of:

| Backend | What it is | Trade-off |
|---|---|---|
| built-in relay | bosun's own userspace relay | connection and byte counters, PROXY protocol support |
| `nft` | nftables kernel DNAT (bosun ≥ 0.18) | IPv4/IPv6 target (IPv6 needs bosun ≥ 0.62), same address family as the listen address, optional source preservation when replies route back through the node; no counters |
| `realm` | bosun installs and runs [realm](https://github.com/zhboner/realm) | high throughput, hostname targets, UDP; no counters |

**Several targets.** A rule can have further targets (the split-arrows
button on the rule): a backup line for when the first is down, or more
lines to spread connections over. *Failover* sends new connections to the
first target whose health check passes, and a connection whose dial fails
moves on to the next target before the client notices; *round-robin*
spreads connections over the healthy targets by weight. Each target's
health, RTT and connection count shows next to the rule. Built-in relay:
both modes for TCP; realm: TCP round-robin only (it does not retry another
target, and its UDP path uses the primary target);
nft: one target. A connection always uses one target, so this keeps a
relay up and spreads load across lines — it does not make one download
faster. Needs bosun ≥ 0.49. With bosun ≥ 0.62, the displayed measurement
explicitly says TCP; UDP is untested, not offline on a refused TCP connection.
Mixed rules show the two separately. UDP hop selection is independent of TCP
probe health. Selecting Hysteria2/TUIC for a new forward defaults to UDP;
existing/manual protocol choices remain configurable. Older UDP health reports
are shown as unknown because their probe transport was not recorded.

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

## Ingresses and port mappings (NAT / IPLC)

A node behind NAT, an IPLC or a dedicated line may expose only provider-assigned
ports. **Servers → select server → Server proxy settings → NAT / dedicated-line ingress** records the provider's existing
configuration; it does not create NAT mappings or change the provider firewall.
Choose NAT or IPLC / dedicated line. Existing `mapped` records keep their old
addresses, ranges and optional direct listeners after migration.

- the **local NIC address** on the VPS — inbounds and forwards bind to it so replies go
  back through the line;
- the line's **far-end address** — what a relay must forward to; not
  reachable from the public internet;
- the provider's **public entry**, if the service includes one (a China
  Mobile entry address, for example);
- the usable **local port range** and an optional public-port offset; or
  multiple explicit mappings, each with local start/end and public start.

For example, local `20001–20099` → public `30001–30099` and local `40000` →
public `443` can coexist in one NAT ingress. A single port has equal local
start/end. Local and public ranges must each be non-overlapping and stay within
1–65535. Explicit mappings replace the legacy range/offset, rather than adding
to an unrestricted default. Mappings apply equally to TCP and UDP; check which
protocols the provider forwards. Reserved ports are **local** ports (e.g. SSH)
and cannot be used by an inbound or a forward.

Inbounds and forwards both select an ingress. Create and update validate their
listeners against its port policy; a nonempty listen address must match its
bind address. Mieru BOTH also validates its second, adjacent UDP port and the
corresponding public mapping. The relay backend does not bypass these checks.

**Require an ingress on this node** prevents any inbound or forward on that
node from choosing direct access. The new NAT form enables it by default;
existing records default to off. Leave it off for a multihomed node that still
needs direct public listeners. All selected ingresses enforce their own port
policy regardless of this switch. This controls managed proxy and relay
listeners, not SSH, the management UI, or other programs on the server.

Enabling this switch or editing a port policy checks existing inbounds and
forwards first, including disabled configurations. An incompatible edit is
rejected without changing the saved configuration. An ingress still referenced
by an inbound or forward cannot be deleted; reassign or remove those listeners
first. Manually configured subscription nodes keep their explicit connection addresses:
review them after changing public mapping addresses or ports. Nodes bound to a
managed line are updated together with that line.

The existing API routes and range/offset fields remain. New fields are
`kind: nat|iplc|mapped`, `port_mappings` (rows with `local_from`, `local_to`,
`public_from`) and `require_ingress`; ingress editor requests use their existing
PascalCase equivalents. Omitting or sending null for the new mapping or switch
fields on update preserves saved values. Forward `ingress_id` is an optional
string containing the ingress ID. Captain resolves its bind address before
sending state, so managed nodes do not require a new agent to use this feature.
The bosun standalone editor implements the same policies locally.

Inbounds pick an ingress (direct is the default, or the first ingress on nodes with
no public address). Any protocol may ride a line, and a recipe applied
while an ingress is selected takes the first free, non-reserved port of the
range. Whether a given protocol passes is up to the provider's entry — some
carrier entries only pass non-TLS protocols such as mieru. Entries then
advertise the public entry on the mapped port.

A line **without** a public entry is served through a relay node: add a
port forward there whose target is the far-end address (the picker fills it
in) and use the relay's address in the entry. For a NAT target without a far-end
address, the picker uses its public address and mapped public port. A relay
itself behind NAT can select its own ingress; creating a subscription entry
from that forward uses the relay's mapped public address. Direct inbounds on the same
node (Hysteria2, REALITY) keep using the node's own address or domain.

With the probe on, every ingress that has both a local NIC address and a
far-end address gets an automatic RTT task (bosun ≥ 0.15 binds the TCP
connect to the NIC; even a refused port measures the line), shown under the
ingress name on the status and speed-test pages. Lines are usually private,
so carrier latency is not measured through them.

## Node connections: VLESS Reverse

Open **Nodes → Lines → VLESS Reverse**, select exit server B, then
**Configure transits**.
Select up to 32 transit nodes A in one wizard. Each connection owns two Xray
listeners on A: a user-facing inbound (VLESS + REALITY by default), and a
separate VLESS + REALITY receiver for B's dedicated tunnel identity. It also
creates a subscription entry pointing at A. Users authenticate and their
traffic is counted on A; B initiates the reverse connections and exits directly.
B needs no public tunnel listener. Ordinary inbounds on either node remain
independent, including ordinary A inbounds that exit locally.

For each A, choose the local user and tunnel ports, optional NAT/IPLC ingress,
user group and REALITY SNI. The wizard previews both public endpoints after
port mapping. Both ports must be available and, for NAT, forwarded by the
provider; Captain cannot create the provider's forwarding rules. B must be
able to reach A's public tunnel endpoint, and A must be able to reach the
chosen REALITY target. User protocol settings reuse the inbound editor and
require Xray support; the transport between B and A remains VLESS + REALITY.

Each A has **Auto select** and **Probe current target** buttons. Scans run on
that transit, not on B or Captain, so results reflect A's path to the target.
Selecting a target updates both the tunnel SNI and the REALITY user listener;
the advanced protocol editor and bulk SNI action keep those values consistent.
An ordinary TLS user listener keeps its own certificate name. Editing the
target or closing/removing the form cancels result polling and prevents stale
results from replacing the current draft; a job already queued on A may finish.

Enter a target verified with a real REALITY connection from each A; new
connections have no preselected SNI. Avoid `www.microsoft.com` with the pinned
Xray 26.3.27: some Microsoft responses include OCSP data that pushes the TLS
Certificate record beyond its REALITY implementation's 8192-byte buffer.
An ordinary TLS 1.3 handshake can succeed while REALITY fails. This is a
[reported upstream limitation](https://github.com/XTLS/Xray-core/issues/6356),
not a permanent SNI blacklist. The [upstream buffer fix](https://github.com/XTLS/REALITY/pull/33)
does not change the already pinned core; a core upgrade requires separate
compatibility testing.

Save the full set once. Later, reopen the same wizard to add/remove A nodes,
change a connection or disable it. Existing connections retain their identities
and keys. Removing a connection deletes only its owned listeners and entry;
changing settings can restart the affected node's shared Xray process and
interrupt other connections on that process. Saving is atomic in Captain,
but each node applies its new state independently. Conflicting concurrent
edits are rejected. Managed resources cannot be edited through the ordinary
inbound/entry forms; remove connections before detaching a node to standalone.
Deleting either node also cleans up the connection's owned resources.

Both nodes need bosun v0.60.0 or later, advertising `vless_reverse` under Xray core
capabilities and Xray enabled. Older nodes receive none of the managed reverse
inbounds, so ignoring a new field cannot silently turn A into a direct exit.
The wizard distinguishes upgrade required, offline, awaiting application,
connected, disconnected and unknown. Connected means A reported an active
tunnel identity after both nodes applied their current configuration; it is
not an end-to-end website availability check. Xray reconnects automatically;
use either node's self-check and diagnostics to investigate failures.

Reverse user traffic is pinned to its reverse outbound. It fails closed when
B is unavailable, without falling back to A's default exit. B still enforces
private-destination isolation and audit blocks. Xray overrides of routing,
outbounds and policy are refused while managed connections exist; configure
ordinary traffic through the routing editor instead. Tags beginning `reverse-`
are reserved on these nodes.

**Limits:** this is a TCP reverse transport, not WireGuard. Xray may maintain
multiple workers/connections; it does not promise one connection or zero CPS.
It may help with incoming TCP connection pressure, but cannot establish an
ISP's cause of packet loss or remove B's connections to destination websites.
The current socket-mark-based tc per-user speed limits do not apply to the
multiplexed reverse path. Captain aggregates usage after reports arrive; shared
quotas across several A nodes are not an instantaneous global hard cap. Do not
use this mode when those per-user speed guarantees are required.

## Outbounds, landing and egress

**Servers → select server → Server proxy settings → Outbounds & routing**: add an exit from a share link — bosun
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
and the outcome is shown in a toast. Shared or duplicate node names are
skipped, as are ingress writes whose target conflicts with a node's address.
Existing multi-address pools and proxied records are not overwritten; their
DNS must be managed externally. The token needs DNS edit permission on
the zone, which the DNS-01 token already has.

**DNS history and recovery.** Node details → DNS health → Automatic DNS change
history (full administrators only) records each attempted automatic A/AAAA write.
Captain saves the previous record, requested value, record ID, TTL, proxy status,
actor, source and time **before** sending the write. The intent is committed with
SQLite FULL synchronization on the reserved connection, then the previous sync
policy is restored; ordinary application writes retain their existing policy.
If that save fails, no write is sent. Successful provider responses are stored separately from the requested
value. A provider rejection is marked failed; timeouts, interrupted requests or
an unavailable completion record remain **outcome unknown**. Check Cloudflare's
current records before retrying or restoring an old value. Writes preserve an
existing record's TTL. Cloudflare and SQLite cannot commit atomically, and an
external editor can race the read/write sequence; this is an audit trail, not a
transaction with the DNS provider or an automatic rollback system.

History is retained for 180 days. Deleting a node detaches its history without
discarding the record snapshots; the fleet DNS view still lists it. Site reset
clears this history but does not change external DNS. Records overwritten before
this feature, or edited outside Captain, cannot be reconstructed by Captain's
history: use the intended node/entry IP, a prior export or the provider's audit
history. Disable the conflicting automatic writer before manual recovery.

Node details and Monitoring → DNS health also show expected and observed A/AAAA
addresses, resolver sources and check time, with an immediate check action. See
[DNS health checks](MONITORING.md#dns-health-checks) for comparison and alert rules.

## Additions in 1.8.0

Inbound and routing editors support typed named presets with a draft diff and impact preview. Node detail also links to administrator-only supplier/asset costs. See [administration workflows](ADMIN_WORKFLOWS.md).

## Private upstream exceptions (bosun ≥ 0.62)

Full administrators can edit **Node → Routing → Private upstream exceptions**.
Enter an IP/CIDR, TCP/UDP and one port per row. Use this for an authenticated
SOCKS upstream reachable over a private WireGuard/BGP network, for example
`10.10.0.2/32`, TCP, 1080. The endpoint remains an ordinary outbound in Routing;
this policy only permits the core's network socket through the OS guard.

`GET/PUT /api/admin/settings/nodes/{id}/egress` reads/writes
`{"upstreams":[{"cidr":"10.10.0.2/32","protocol":"tcp","port":1080}]}`.
Only administrators (including appropriately scoped administrator tokens) may
access it; old nodes reject nonempty updates. GET reports `supported` and
`minimum_version`. PUT requires the list: omission/null is rejected, `[]`
explicitly clears it. The new column is separate from routing, so older
routing clients cannot erase the policy. Updates use the topology lock.
Check the node self-check after saving: a saved policy alone is not proof that
Linux/nft installed it. Local YAML exceptions also apply.

Exceptions cover all cores on the node, not one inbound/outbound. They do not
add user direct routes; cores without destination filtering, or unresolved
hostnames, may also let users reach an excepted endpoint. Keep the exception
narrow and authenticate the upstream. Per-inbound private access is not part
of this feature. Disabling host `cores.egress_guard` no longer leaves stale
rules after restart, and root-only control APIs remain protected.

### Per-inbound private access (Captain 1.14 / bosun 0.63)

In the inbound editor, use **Private access** (under **Advanced** in the
standalone editor and reverse wizard). The default is
**Off**. **Internal networks** allows RFC 1918 and IPv6 ULA; **Custom addresses**
accepts 1–64 literal IP/CIDR rules, optionally restricted to TCP/UDP and a
single destination port or range. Blank protocol means both; port 0 means all
ports. CGNAT needs a custom rule. Loopback, link-local, core control APIs and
known cloud metadata endpoints remain protected even inside a broader grant.
Prefer a narrow address and port for a single internal service.

The permission applies to every authorized user of that inbound. Use the
existing user/group assignment to keep it internal. Full Captain administrators
alone can enable or edit an inbound granting this access. The standalone
administrator can configure it in the same editor. It grants destinations,
not a new exit: existing split routes, audit blocks and bound direct exits
still apply. OS WireGuard routes work as ordinary system routes. For managed
VLESS Reverse, edit the user inbound through the exit node's connection wizard;
the corresponding reverse client on B enforces its own policy, independently
of other transits. A continues tunnelling to B. Authentication and accounting
remain on A; the existing reverse-path per-user tc limitation is unchanged.

This requires bosun ≥ 0.63.0 advertising `private_access`, Linux nftables,
a non-root core account, enabled egress protection and sing-box or Xray.
Capabilities reflect the node's runtime prerequisites. Native Hysteria, mita
and snell-server, WireGuard inbounds, shared-key Snell and Xray SOCKS/HTTP
inbounds do not support this policy. Hysteria2 may use sing-box instead.
For dual-layer enforcement, the whole node must use direct/freedom outbounds: proxy chains,
remote proxies, WARP, balancers, GeoIP split rules, global private grants and
raw overrides of routing, DNS, policy, inbounds or outbounds are rejected.
This restriction avoids claiming enforcement when a remote proxy or override
controls the final socket. Node-wide upstream exceptions remain a separate
feature; private grant sockets cannot borrow their broader exceptions.

Changing the effective permission/mark set restarts supervised proxy cores to
close old sockets before installing the new grants; existing connections will
reconnect. Installation failure stops those cores and reports an apply error.
Ordinary limited users' private socket marks share their existing tc class.
A known older/unsupported node cannot save an enabled policy, and delivery
withholds such inbounds/clients after a capability downgrade. Do not downgrade
a node while relying on this feature.

The additive `private_access` object lives in the existing inbound settings:

```json
{"private_access":{"mode":"custom","rules":[{"cidr":"10.10.0.2/32","protocol":"tcp","port_start":8080}]}}
```

Use `{"mode":"internal"}` for the preset and `{"mode":"off"}` to disable.
Older API clients that omit or send null for this field preserve an existing
policy. Templates do not export private permissions; applying a preset retains
the destination inbound's current permission. No database migration is needed.

## Managing node core packages

Open a node's details and expand **Core management**. Full administrators can download a catalog version, then explicitly activate or switch it. Downloading does not change running services. The node serializes the change with apply/report, rejects incompatible or stale requests, checks readiness and restores the prior instance if the replacement fails. Connections served by the switched distribution are interrupted.

Update bosun first: the page requires its new `core_inventory` report and `core_manage` job support. The management API remains `POST /api/admin/nodes/{id}/jobs` with `kind: "core_manage"`; parameters are `action` (`download` or `activate`), `distribution`, `version`, and the reported `revision`. Captain adds the execution deadline and allows only one pending operation per node. Older nodes retain their existing behavior and cannot be assigned the new SSH/Extended features without a current capability report.

Official sing-box and `singbox-extended` can coexist. Core package/version selection is separate from each inbound's protocol and `Core` choice. Extended Mieru must be selected explicitly and requires clients to refresh subscriptions after switching from mita; it does not support specific listen-IP/ingress binds or mita native rolling quotas. SSH is a TCP proxy with subscriber passwords and a generated host-key pin, not system SSH login or a shell. Only supported subscription formats expose it.

See [bosun core management](https://github.com/zeptop-dev/bosun/blob/master/docs/CORE_MANAGEMENT.md) for persistence, release sources, supported combinations and recovery behavior.


## Managed reverse WireGuard (Captain 1.20 / bosun 0.68)

Open **Nodes → Lines → Reverse WireGuard**, select exit server B, and create a
connection to transit A. Both are Captain-managed full bosun nodes. A accepts VLESS REALITY
subscribers, authenticates them and accounts their upload/download. B initiates
a kernel WireGuard tunnel to A and provides the exit; tunnel overhead is not
charged again. The existing VLESS Reverse option remains separate.

Since Captain v1.35, server lists and details display the accepted proxy traffic
against B while retaining A's collection details. Authentication and the
customer's single usage charge remain on A. This does not turn B's interface
counters into per-user billing. Earlier history may be incomplete; see
[traffic definitions](MONITORING.md#probe-and-status-page).

Both nodes need Linux kernel WireGuard support, `wg`, `ip`, `nft` and `tc`,
root network-administration privileges and enabled egress protection with
non-root proxy cores. A also needs Xray. The panel waits for fresh capability
reports from both nodes; upgrading the panel alone cannot enable this feature.

The prerequisite cards show A and B separately, including missing `ip`, `wg`,
`nft`, `tc`, `sysctl`, service PATH, core identity and privileges. With a bosun
that reports setup support, **Install missing dependencies** queues a bounded
five-minute task and refreshes the report on completion without clearing the
connection draft. Only full administrators can request it. The installer uses
fixed package names on native root Debian/Ubuntu, Alpine and supported dnf
systems; it does not rerun the bosun installer. Containers, service PATH issues,
kernel support and core/guard configuration require manual attention. Binary
updates do not silently install new OS packages. Tool readiness is not a tunnel
connectivity test; host firewall forwarding still needs the checks below.

Choose A, a subscriber TCP port, a separate local WireGuard UDP port, the literal
IP and UDP port reachable from B, MTU (1280–1420), and a REALITY server name.
Run the REALITY check from A. If external NAT maps A's UDP port, enter the public
mapped endpoint separately. Nodes requiring an ingress mapping cannot serve as
A in this initial editor. Restrict the subscriber entry's user group as needed.

The host firewall must also permit forwarding on B from the managed `bwg<slot>`
interface to the intended public or LAN interface, including return traffic.
Bosun's destination ACL and SNAT do not override a later UFW/firewalld default
drop. Listener port auto-open and host forwarding are separate operations.
A fresh handshake can therefore show **Connected** while subscriber requests
still fail. Check the host firewall before changing the tunnel or private-access
grants.

### Automatic forwarding on B

The new connection form enables **Automatically manage forwarding on B**
(`auto_forwarding`) by default. Migration 78 leaves existing connections in
manual mode (`false`); enable this option explicitly when editing an existing
connection to start automatic management. Older API clients that omit the field
also create manual connections. Omitting it or sending `null` while editing
preserves the saved choice; an explicit `false` returns to manual management.

Automatic forwarding currently supports active UFW with unambiguous routes in
the main routing table. Firewalld, simultaneous firewall backends, custom policy
routing, and routes or private grants that cannot be assigned safely to one
interface are rejected with a visible error. This includes a B that also serves
as A for another managed WG connection: A's source-routing rules require manual
forwarding management on that host. Do not change the global FORWARD policy or
disable the host firewall to work around these limits.

The node's global `firewall_auto_open` setting must also be enabled. If a
connection requests automatic forwarding while that global setting is `false`,
application fails and the managed WG data path is frozen; bosun does not report
success or silently switch the connection to manual mode. To manage an
unsupported host yourself, turn off the connection's automatic option and
prepare its forwarding rules manually. With no active UFW or firewalld,
the reported result is **No supported firewall active**, not proof that other
iptables/nftables or Docker rules allow forwarding.

Bosun derives rules for the exact managed WG interface, peer tunnel address,
public or authorized LAN interface, and TCP/UDP protocol, including replies.
Allowances for a separate LAN interface include its authorized destination and
port scopes; grants reached through the public interface use that interface's
allowance and remain restricted by the nft ACL.
The existing nft destination ACL still controls every connection; host
allowances do not grant extra private access or permit forwarding into another
managed tunnel. Earlier host deny rules may still block traffic.

An independent persistent ownership journal records intent before UFW changes.
Only unchanged, provably owned rules are removed when a link is deleted,
disabled, switched to manual management or cleaned up during restart/uninstall.
Existing manual rules are retained rather than adopted. Conflicting or externally
edited rules are preserved and reported for review; those retained rules still
need an administrator's cleanup. Synchronization is retried while managed WG
is active.

UFW's official CLI updates its own user chains when adding or deleting rules.
The preservation guarantee covers manual rules persistently created through
UFW. Temporary rules inserted directly with iptables into UFW-managed chains
are outside the supported ownership model and may be replaced by normal UFW
synchronization.

The connection card shows B's last forwarding check separately from the tunnel
handshake, including manual management, unsupported configuration and failure.
Old, missing and expired checks do not count as a successful check. Saving a
connection, applying rules and completing a handshake each confirm different
steps; test access from the actual subscriber client before declaring the path
usable.

### Manual forwarding

For example, with UFW, link slot 1 and exit interface `eth0`, the administrator
can allow this interface pair, A's tunnel address and the supported protocols:

```sh
ufw route allow in on bwg1 out on eth0 proto tcp from 10.10.0.5 to any
ufw route allow in on bwg1 out on eth0 proto udp from 10.10.0.5 to any
```

Use the actual slot, peer address and exit interface; an authorized LAN
destination may use a different interface. Also permit replies if the host's
existing established/related rules do not already allow them. Bosun still
restricts destinations before these rules. Remove the corresponding manual
rules when retiring the link; automatic cleanup does not own them.

### Domain address preference

New connections select **Prefer IPv4** (`domain_preference: "ipv4"`). This fixes
dual-stack domains choosing IPv6 when B only has a working IPv4 exit. Select
IPv6 preference only when that is appropriate for B. Migration 78 keeps existing
connections at the empty value, preserving their original IPv6-first rule order;
the editor also offers that legacy setting. Omitted or `null` update fields
preserve the saved choice; an explicit empty string restores the legacy behavior.

The preference selects between DNS answer families. Literal IPv4/IPv6 addresses
keep their family, and an AAAA-only domain still uses IPv6. It does not create an
IPv6 exit, translate addresses, or retry the other family after a connection
failure. In particular, choosing IPv4 preference does not make an IPv6-only
destination reachable from an IPv4-only B, and failures never fall back to A's
direct exit.

Saving and delivering these new options requires fresh capability reports:
domain preference from A and automatic forwarding from B. Upgrade the relevant
node and wait for its report before enabling an option. If a required capability
expires or disappears after a downgrade, Captain withholds both WG endpoint
configurations and the entire managed subscriber inbound; it does not leave an
inbound that could exit directly from A.

### Private access and connection lifecycle

Private access defaults off. To reach B's LAN, select **Explicit addresses** and
add destination CIDRs, protocols and ports. This grants access to every authorized
subscriber on that entry. B performs SNAT, so target machines need no additional
return route and see B as the source. Different B nodes may use overlapping LAN
addresses. Endpoint management services, loopback, link-local and metadata remain
isolated. Broadcast discovery and LAN-initiated connections are not supported.

A never falls back to its own direct egress when the tunnel fails. Changes that
affect permissions stop old connections before applying the new rules. A user's
configured speed limit is shared across its ordinary egress and all WG links on
that A; separate A nodes retain independent enforcement. Usage follows the existing
per-inbound reporting and quota rules. Handshake/interface bytes are diagnostics.

Saved, applied and connected are distinct states: connected requires a recent
handshake and confirmation of the current configuration on both endpoints.
Offline agents cannot acknowledge a revocation immediately. Delete or edit links
through this card; their managed inbound and entry cannot be changed separately.
Remove links before switching an endpoint to monitor mode or detaching it.
Each node supports at most 64 links. Tunnel host addresses are allocated from
`10.10.0.0/16` and `fd42:626f:7375::/112`; existing address, route and interface
ownership conflicts are rejected rather than overwritten.

## Observed public addresses and automatic following (Captain 1.21 / bosun 0.69)

The **Machine public addresses and entry** card keeps the bosun-observed public
IPv4/IPv6 separate from the node's published entry addresses. Updated full and
monitor-only Captain agents probe every 30 seconds for the first three minutes
after startup, then every five minutes, using two independent HTTPS
responders per family, with direct, family-specific sockets and no environment
HTTP proxy. These are outbound public addresses, not proof of inbound reachability
or the node's private interface addresses. OS-level VPN routing can still affect
what the responders observe.

Existing node addresses remain fixed by default. Administrators can enable
following separately for IPv4 and IPv6 after a fresh agent report. A changed
address must agree across responders and be confirmed on two distinct sampling
cycles before Captain updates the enabled family. Cached reports do not count
again. A family with no published address can confirm after 30 seconds; replacing
an existing address still requires at least two minutes of stable observations.
The fast startup schedule requires an updated bosun; older agents keep their
five-minute cadence. Reports are normally minute-spaced, so first publication
typically takes one to two minutes, plus DNS provider and resolver latency.
Failed, disagreeing, stale or missing observations retain the last known
address and never delete A/AAAA records. The card shows the last successful
observation time; older agents cannot enable following until upgraded. A large
agent clock error also prevents accepting observations.

A manual edit to a node address disables following for that family. Unchanged
fields and older clients omitting the follow settings preserve the existing
choice. Explicit NAT/line ingress addresses, subscription Entry addresses and
managed WireGuard literal endpoints are not inferred from outbound probes and
are not rewritten. For dynamic direct nodes, use the node domain in subscription
entries and enable automatic DNS for that domain. For fixed NAT public entries,
leave following off: machine observations still update independently.

Address reports wake a bounded background worker immediately; a one-minute sweep
remains as recovery for missed notifications. Confirmed changes invalidate cached
node state. DNS synchronization uses the existing Cloudflare auto-DNS configuration,
shared-domain/CDN/multiple-record guards and durable change history. It runs
outside agent report/billing acknowledgements and retries failures at most every
five minutes. DNS changes and node address changes are not an atomic external
transaction; a pending indication and DNS health/history expose failures.
Successful synchronization triggers a DNS health check. For up to ten minutes
after a matching successful write, unresolved results are rechecked about every
30 seconds and displayed as awaiting propagation. Existing failure statuses and
the five-minute/ten-minute alert thresholds remain intact; skipped or failed
writes are never presented as successful propagation.
Re-pairing clears observations and requires new confirmations. Source-restricted
networks or unavailable public responders leave observations unknown rather than
guessing an inbound address from the Captain connection.

The new-node form enables automatic IPv4 and IPv6 detection independently by
default. Leave both enabled for a direct node; disable a family to enter its
fixed public entry. Pairing does not fill the IPv4 entry from the request source
when automatic IPv4 is enabled: it waits for confirmed bosun observations.
Unavailable IPv6 remains blank. Older agents require upgrading to support this
workflow. Existing nodes and API clients omitting the additive creation fields
`AutoIPv4` / `AutoIPv6` retain the previous fixed-address behavior.

The optional internal address registers an existing server IP. It identifies
relay sources in online-device views and device-limit accounting, and can be
explicitly selected as a forwarding destination. All known relay sources for
one user count as one device. Filling this field alone does not change existing
paths, create private-network connectivity, configure a tunnel, or select the
connection used by bosun to reach Captain. See
[internal forwarding addresses](#reusing-a-servers-internal-address).

## Three-carrier return routes (Captain 1.21 / bosun 0.69)

The administrator-only node monitoring section now offers **Return routes**.
Choose Beijing, Shanghai or Guangzhou and IPv4 or IPv6, then run a three-carrier
check. Each measurement follows the node's OS default route toward the selected
public targets. It does not measure the customer's forward path, or force
traffic through a proxy inbound, WireGuard tunnel or configured application
outbound. OS policy routing/VPNs can affect the path.

Requires bosun v0.69.0 or newer, Linux traceroute supporting ICMP mode, and
permission to use raw ICMP sockets. Missing software, permissions or IPv6
connectivity produce unavailable/failed/incomplete results. The check does not
install software, alter firewall rules or grant container privileges. The
existing network-diagnostic task channel works for both full and monitor nodes;
no additional inbound port or SSH access is required.

Checks show the destination, responding hops, RTT, origin ASNs, reached status,
missing ASN count and classification evidence. Public-hop ASN enrichment uses
Team Cymru's DNS service through the node resolver; private hops are not sent.
ASN lookup failure preserves the path but leaves its network identity unknown.
IP origin ASNs describe announced prefixes, not contractual transit products or
geographic locations. The public target catalog is a dated snapshot, not a
guarantee that every target is continuously reachable or serves all customers in
its labelled region. No automatic target substitution occurs.

CN2/163/9929/4837/CUG/CMIN2/CMI/CMNET/CTGNET evidence is kept separately.
Destination addresses do not count as transit evidence. Repeated routers cannot
inflate CN2 hop counts. Only a complete responding path with at least two
distinct CN2 routers, no missing public ASNs, no ambiguous origin ASN and no
other recognised network may show **Possible CN2 GIA**. Even then this is a
hypothesis; the tool does not certify GIA, GT or a purchased service tier.
Missing intermediate responses do not prove user-traffic packet loss. Use the
adjacent network-quality monitoring for ongoing measurements.

Schedules default off, with 6/12/24-hour intervals for one chosen region and
family per node. Saving starts a fresh interval; use the manual button for an
immediate test. Offline/unpaired/unsupported nodes are not sent checks. A busy
network diagnostic delays a scheduled run by five minutes. Each node admits
one diagnostic; a return-route task uses at most three bounded traceroute
processes and four ASN queries concurrently, with a 90-second total timeout.
Tasks expire after 120 seconds. Disabling a schedule does not cancel an already
dispatched check.

History is limited to 30 days and 120 checks per node, separate from ordinary
diagnostics. A result compares observed network labels against the preceding
matching region/family/rules version and the same target. A change is evidence
to investigate, not an automatic degradation alert: missing hops or enrichment
can also change the labels. No route details are added to the public status page.

Sources for the initial public target catalog and ASN-query protocol:
[backtrace catalog](https://github.com/oneclickvirt/backtrace/blob/main/model/model.go),
[Team Cymru DNS mapping](https://www.team-cymru.com/ip-asn-mapping).
The classifier and collector are implemented in bosun; no NextTrace or
backtrace implementation was copied or embedded.

### Managed return-route tool (Captain 1.22 / bosun 0.70)

The return-route card checks tool availability through the node's existing job
channel. **Check again** refreshes status; **Install NextTrace** downloads pinned
NextTrace Tiny v1.7.4 for Linux amd64/arm64, verifies its embedded SHA256 digest,
and atomically installs it under `data_dir/tools/nexttrace/v1.7.4`. Only full
administrators may request installation. No shell installer or OS package
manager is invoked. The managed tool is preferred; Linux traceroute remains the
fallback. An incompatible system implementation (including BusyBox variants)
is reported explicitly by the tool check.

Checks distinguish executable compatibility and IPv4/IPv6 raw ICMP socket
availability. Installing an executable does not grant NET_RAW or change a
container's privileges. Container data volumes must be writable, persistent and
owned by the service user; the same managed installation works inside that
volume. Existing images are not silently rebuilt or modified on the host.
NextTrace is a separate GPL-3.0 upstream executable downloaded from
https://github.com/nxtrace/NTrace-core/releases/tag/v1.7.4; its source and license
are available at that tag. No upstream implementation is linked into bosun.
Built-in enrichment, reverse DNS and map uploads are disabled. The existing
Team Cymru/public-hop-only policy and conservative classification remain intact.

## Host management

Captain 1.23 / bosun 0.71 add **Node details → Host management** for full
administrators. It observes actual host configuration, including settings made
through Quench or a terminal. There is no onboarding score or mandatory baseline.
The page refreshes stale observations while open; offline nodes retain their last
observation and cannot be edited. Observations are not proof of actual SSH login.

Each supported item has its own change review. The initial write scope is native
root Debian/Ubuntu with systemd. Containers and other environments remain
read-only; their observations describe the agent environment, not the host.
Capabilities are rechecked on the node before each operation.

- Hostname and timezone/NTP use existing system facilities. Hostname changes do
  not rename the Captain node or update DNS; `/etc/hosts` is preserved.
- SSH supports ordinary configurations, retaining existing content and adding a
  small managed first-value block. Socket activation, conditional Match/access
  restrictions, custom includes and listen-address policies require manual work.
  Port changes keep old and new listeners until a separate login is confirmed.
  NAT mappings and cloud security groups must be arranged with the provider.
  Prepare local firewall access first and pause an active sshd Fail2ban jail
  before migrating; re-enable it after the final port is in place.
- Administrator access adds a public key to a root-owned key store and a dedicated
  passwordless-sudo grant (full root privileges). Existing accounts/grants are
  preserved. Recovery removes the new grant/key but retains any newly created
  account/home. Existing account renames and deletion are not performed.
- Firewall editing adds a scoped allow rule to an already active UFW backend.
  It does not enable a firewall, replace foreign nftables rules, or flush bosun
  rules. Fail2ban edits only its installed sshd jail. Security-update policy uses
  installed unattended-upgrades and the distribution's active timer; automatic
  reboot remains disabled. These operations do not install missing packages.
- DNS supports systemd-resolved and ordinary, externally unmanaged resolv.conf.
  It preserves search/options directives and never changes domain records.
  Link/DHCP resolvers can still take precedence.
- Network settings select an available TCP congestion algorithm and optionally
  supported security sysctls, preserving qdiscs, WireGuard and user limits.
- Swap creates one dedicated file and fstab entry, preserving existing swap.
  Resize/removal of existing swap is not offered; unsupported filesystems or
  insufficient space fail explicitly.

Access, DNS and firewall changes await explicit confirmation for ten minutes.
A root-owned `bosun-host-recovery.timer` runs independently of bosun and Captain,
checks deadlines every 30 seconds, and remains enabled across reboots. Failed
restores remain visible and are retried; conflicting external configuration edits
require manual recovery instead of being silently overwritten. Keep provider
console access available for changes that affect remote access. A successful
node heartbeat is not a substitute for verifying the new SSH login and sudo.

Changes carry a configuration revision and durable, panel-bound job receipts to
reject stale plans and duplicate execution. Recovery files and receipts live in
`data_dir/host-management` with restrictive permissions. Captain retains up to
200 operations for 180 days plus the latest checks; ordinary job cleanup does
not delete this history. This is separate from proxy core backups and updates.

### Automatic listener firewall rules (Captain 1.24 / bosun 0.72)

The Firewall item in Host management includes automatic rules with their inbound,
forward, WG link, certificate or system-listener sources and last synchronization
time. Refresh host state for a fresh snapshot. Older nodes show an upgrade hint.
Only active UFW/firewalld listener allowances are managed; cloud security groups,
NAT mappings, raw nftables/iptables policies and WG forwarding rules are separate.
The per-connection WG forwarding option is described
[above](#automatic-forwarding-on-b); listener auto-open alone does not
enable it on existing connections.
An outbound destination port is not a local listener. TCP and UDP are separate,
and shared rules remain until their last listener disappears. Synchronization
runs on configuration changes and retries every minute.

UFW comments and journal-owned firewalld rich rules identify new managed rules.
Existing operator allowances are preserved. Legacy records lack ownership proof:
those old openings remain and must be reviewed manually after removing services.
Externally edited rules or a changed firewall backend/zone are retained for review.
Firewalld synchronization uses the recorded default zone, runtime and permanent
configuration, without reloading unrelated rules. A synchronized allowance is
not a public connectivity test or a guarantee against earlier deny rules.

In the group/order editor, drag a node handle onto a group card (including Ungrouped) to append it to that group. The target highlights during dragging and group counts update in the draft. Save applies the change; Cancel discards it.

The hostname shown below a node name follows regular reports from agents that support hostname reporting. The administrator-assigned node name stays unchanged. Older agents and missing or invalid hostname readings retain the last known hostname. Both Captain and bosun need the hostname-reporting update; restarting an older bosun alone does not refresh this field.

### Private-access readiness

Node details and the inbound editor show the reported prerequisites for
per-inbound private access. The checks distinguish Linux support, a non-root
proxy account, nftables, egress protection, global exemptions and process
capabilities. Full administrators can request **Recheck**, a read-only node job
that additionally tests kernel nftables access. It neither restarts proxies nor
changes network policy. Concurrent pending requests reuse the same job.

After fixing a reported condition, recheck without closing the inbound form.
Readiness data expires after three minutes; offline or older agents show an
unknown state, not a successful check. Upgrade bosun and enable a supported
sing-box/Xray adapter to obtain detailed reports. Older capability reports retain
the existing validation behavior. Passing prerequisites does not bypass protocol,
route or runtime policy checks and does not guarantee destination reachability.

The displayed preparation command creates the configured non-root account but
preserves configuration and firewall rules. Configuration changes require a
bosun restart and interrupt proxy connections. Existing Docker deployments may
need `cap_add: [NET_ADMIN]` and container recreation; new full installations add
this automatically. Do not enable privileged mode just to satisfy this check.

#### Automatic protection mode

Ordinary inbound private-access settings show the current guard-derived mode;
there is no separate mode selector. With the egress guard enabled, authorization
uses dual protection. With `cores.egress_guard: false`, a full administrator must
explicitly acknowledge the absence of nft destination isolation before granting
access, and the adapter must support independent address/DNS enforcement. A
missing acknowledgement or unsupported combination blocks saving and execution.
Changing this node configuration requires restarting bosun; the panel does not
disable a firewall remotely.

Automatic intent and acknowledgement are stored separately from the derived mode.
Re-enabling the guard restores dual protection for supported direct topologies;
a WARP/proxy topology is blocked rather than silently losing isolation. Removing permission clears the
acknowledgement. Existing policies retain their original behavior until edited;
older nodes without automatic-policy capabilities cannot receive new automatic
policies. Adapter features and runtime prerequisites are reported separately.

This mode supports sing-box / Extended exits. Upgraded nodes retain existing
WARP, proxy-chain, balancer and direct routing choices. It uses core
address/transport/port rules instead of nft destination isolation, while retaining
the non-root account and existing control API protection. It therefore does not
remove every nft or agent capability prerequisite. Global exemptions remain
unsupported. Xray and managed reverse tunnels are unavailable in this mode;
Xray's existing dual-layer permissions are unaffected.

DNS answers containing both an authorized and an unauthorized private address
are rejected. An enabled authorization belongs to the entire inbound's permitted
users, as before. Changing the scope or protection mode interrupts existing proxy
connections so old permissions cannot linger. Old nodes without the new explicit
capability cannot receive this policy.


Core-only split routing uses the additive `private_access_policy.split_routing`
capability. Captain refuses the expanded combination for old/stale nodes, both
when saving and delivering state. Multiple WARP exits can coexist with per-inbound
permissions. Destinations are interpreted at the selected exit: granting a LAN
CIDR does not automatically route it directly to this server's LAN. Add an explicit
direct split rule if that is the intended path. The authorization check consumes
resolved addresses before routing, including mixed-answer rejection; remote
proxies receive checked IPs rather than a domain to resolve independently.
