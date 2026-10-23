# Subscriptions

What a user's client downloads, how it is shaped per client, and everything
that decides which servers they see. Version notes name the bosun release a
feature needs; Captain 1.0 ships against bosun 0.47.

## The endpoint

`GET /sub/<token>` (and `/s/<code>` for short and temporary links). The
format is guessed from the `User-Agent` unless `?client=` overrides it or a
response rule decides. Every answer carries `Subscription-Userinfo` with
usage and expiry, a profile title and a `Content-Disposition` filename;
clients disagree about which they read, so all three are sent.

A banned account gets `403 account disabled`. An expired one, or one out of
quota, gets an empty document with the usage header, so the client keeps
the subscription and can show why it is empty.

The renderers live in bosun's `pkg/subscription`, shared with bosun's
standalone panel, and the mihomo and sing-box documents are validated
against the real clients.

| Format | Client | Notes |
|---|---|---|
| `clash` | mihomo / Clash Meta | YAML; `proxies` replaced in the template |
| `stash` | Stash | its own keys (`sni`, hysteria2 `auth`/`up-speed`, tuic `version`/`alpn`) |
| `singbox` | sing-box | JSON; the only format that can express a per-user Snell key |
| `egern` | Egern | YAML with `{{proxy_names}}` in policy groups |
| `surge` | Surge | text profile |
| `surfboard` | Surfboard | ss, vmess and trojan only (its manual's coverage) |
| `loon` | Loon | ss, vmess, vless (+REALITY), trojan, hysteria2, anytls over tcp/ws/http |
| `qx` | Quantumult X | ss, vmess, vless (+REALITY), trojan over tcp/ws |
| `uri` | v2rayN, Shadowrocket … | base64 list of share links |
| `wireguard` | any WireGuard client | `.conf` |

Per-format coverage follows each client's own reference, so a server a
client cannot express is left out of that client's document rather than
written in a dialect it will reject.

## Templates and the designer

Admin → Sub templates edits the document *around* the servers, per format.
YAML templates (clash, stash) get their `proxies` list replaced, and a
`{{proxy_names}}` entry inside any proxy group expands to every server
name. Text templates (surge, surfboard, loon, qx) replace `{{proxies}}`
with the server lines and `{{proxy_names}}` with the comma-joined names.
Loon and Quantumult X default to a bare node list, because that is what
their remote subscriptions are.

The same page has a visual designer (bosun's `pkg/subdesign`): proxy groups
whose members are all servers, servers carrying an entry tag
(`{{proxy_names:tag=hk}}`), servers whose name matches a region pattern
(`{{proxy_names:match=HK|香港}}`) or other groups, plus an ordered rule list
drawn from the [ACL4SSR](https://github.com/ACL4SSR/ACL4SSR) catalogue with
presets (basic, ACL4SSR standard, standard plus region groups). "Generate &
apply" writes every format's template at once.

## Entries: what each user sees

An entry is a server as users see it: a display host and port on top of a
landing inbound's settings. An inbound restricted to a user group only
appears for that group. Admin → Nodes can drag subscription nodes into the order
clients see, give them free-form tags (shown in the portal, usable as a
filter and in the designer) and a region.

Settings → Subscription → *Auto flags* prefixes names that lack a flag
emoji with their region flag: the explicit region if set, otherwise
detected from the name (`Tokyo`, `HK-02`, `香港`, `jp1.example.com` …).

Each entry can carry **client extra fields** — a JSON object merged into
its mihomo, Stash and sing-box proxy (`tfo`, `smux`, `dialer-proxy`,
`ip-version` …) for the knobs only some clients understand.

### Managed server lines and client chains

The **Nodes & lines** page keeps subscription nodes and reusable server lines
in separate tabs. A subscription node can use an existing inbound, a managed
server line, or a client-side proxy chain. The server detail page shows the
saved configuration and latest reported status, with links back to the editors. The UI calls the agent host
a **server**; the existing Node/Inbound/Entry API names remain unchanged.

**Connection address and port** are the real endpoint exported to the client.
Typing a different port does not create a listener, NAT rule, or port forward.
For a direct/manual node, arrange that path first. For a managed server line,
Captain derives the endpoint from the selected first-group member and keeps
it synchronized when the line or its ingress mapping changes. Several
subscription nodes can still publish different endpoints for one inbound.

**Client-side chains** select another subscription node by ID as the front.
The client connects through that front to the destination's connection address;
Captain does not deploy an extra server forward. Renaming either node, adding
region flags, or applying a subscription profile keeps the reference intact.
Both nodes authenticate the subscriber independently and may charge traffic;
this differs from raw server forwarding, where only the landing inbound bills.

The full mihomo, recent Stash and sing-box subscription formats render managed
client chains (`dialer-proxy` / `detour`). Stash requires iOS/tvOS 3.2.5+ or
macOS 4.1+ for full configurations; old Clash implementations are not promised.
Both ends must be supported by the chosen renderer and visible to this user.
Disabled, group-restricted, user-blocked, missing, ambiguously named or
unsupported dependencies cause the dependent node to be omitted, never silently
exported as a direct node. Other formats and single-node share URIs omit managed
client chains. The portal marks them as requiring a full supported subscription.
The current sing-box renderer cannot chain a destination's ShadowTLS carrier,
so that combination is omitted. UDP support still depends on every hop.
Mihomo and Stash omit managed HTTP destinations and their dependent chains:
HTTP's lack of UDP can make mihomo skip the selected rule and fall through to
direct routing. Ordinary HTTP entries and HTTP fronts keep their existing behavior.

The chain limit is eight nodes. Cycles and self-references are rejected. A
referenced node, inbound, server or owned reverse connection cannot be deleted
until its client-chain references are removed. Saving a server-line binding and
a client-dialer binding on the same node is rejected; a front node may itself
publish a server-line endpoint. Advanced client extra fields remain available,
but replacing `type`, `server`, the endpoint port or `udp`, changing the final
`name`/`tag`, or attaching an unmanaged dialer to a front node makes the
managed chain or dependency ineligible. Managed destination dialer fields take precedence;
Stash's conflicting `interface-name`
is removed on a chained destination. Arbitrary custom templates that rename or
remove rendered proxies must preserve these dependency references.

**External nodes** (Admin → External nodes) are offered alongside your own
entries: paste share links (vless, vmess, trojan, ss, hysteria2, tuic,
anytls) or add an airport subscription (base64 or URI list, re-synced
hourly, per-source User-Agent), optionally restricted to a user group.
Nothing of yours runs behind them, so their traffic is not charged. They
are TCP-probed from the panel every ten minutes; a source with *hide
unreachable nodes* drops the ones whose last probe failed.

## Remark variables and info lines

Entry names and the *Info lines* in Settings → Subscription may contain

`{{DAYS_LEFT}}` `{{EXPIRE_DATE}}` `{{TRAFFIC_LEFT}}` `{{TRAFFIC_USED}}`
`{{TRAFFIC_LIMIT}}` `{{USED_PERCENT}}` `{{STATUS}}` `{{PLAN}}` `{{EMAIL}}`
`{{USERNAME}}`

and a switch form, `{{STATUS:ACTIVE=✅|EXPIRED=😓|LIMITED=⛔|DISABLED=❌}}`.
They are filled per user at fetch time, so the client's own server list
shows the state of the account. Info lines are rendered as dummy
Shadowsocks servers pointing at loopback and placed first in every format,
which is how every client shows them as text.

## Response rules

Subscription templates → *Response rules* is an ordered list evaluated on
every `/sub` request. A rule matches on request headers (`User-Agent`,
`x-hwid`, `x-device-os`, … or the `?client=` parameter) with
contains / equals / prefix / regex / present / absent, all or any, and then
either serves a chosen format — with extra response headers and an optional
template override — or refuses with 403, 404, 451, or drops the connection
without a reply. The first match wins; with no match the format is guessed
from the User-Agent as usual. A tester on the same page shows which rule a
given request hits, and the user drawer's fetch history records the rule
name that served each fetch.

## Device identification (HWID)

Happ, FlClashX, V2Box, Streisand and other clients following the
Remnawave/Happ convention send `x-hwid`, `x-device-os`, `x-ver-os` and
`x-device-model` when they fetch a subscription. Settings → Subscription →
*Device identification* turns this on:

- every device is recorded (at most 64 per user, or the configured limit if
  it is higher; devices unseen for 90 days are forgotten);
- the plan's device limit — or a per-user override in the user drawer, or
  the fallback limit — is enforced per device at fetch time;
- a device over the limit receives an empty document with
  `x-hwid-max-devices-reached: true`, `x-hwid-limit` and an optional
  `announce` text the client displays;
- clients that send no `x-hwid` keep the online-IP counting unless
  *Require x-hwid* is on, in which case they get 404.

Users see and remove their own devices in the portal. Admins see them, the
per-user limit and the last fetches (address, client, device, what was
served) in the user drawer; that history is kept 30 days and trimmed to the
newest 200 rows per user.

## Short and temporary links

Settings → Subscription → *Use short links* gives users
`https://sub.example.com/s/<8 chars>` instead of the long token URL. Old
links keep working, and rotating a user's token rotates the code too.

A user's drawer can also issue **temporary links** limited by number of
uses and/or hours — for a trial or a support case — revocable at any time.
A use is spent only when a document is actually served, so a request a
response rule refuses does not consume one.

## Rate limits

`/sub` is a public URL, so it is limited to 60 fetches per token and 600
per client address in five minutes; over that it answers 429 for five
minutes. Client-supplied strings (User-Agent, HWID, platform, model) are
truncated before they are stored.

## Subscription hosts

Settings → Subscription → *Subscription URLs* lists the hostnames users'
links are built from (`https://sub.example.com`, or a pattern like
`https://s[1-9].example.com` to spread clients over several names). They
are covered by the panel's own certificates; see
[DEPLOY.md](DEPLOY.md).

## Quotas the node enforces by itself

A node option, *mita native quotas*, also writes each user's allowance into
mieru's own quota system (the window is the plan's reset cycle), so the
core keeps enforcing the limit while the panel is unreachable.

## Additions in 1.8.0

Named template libraries, explicit/default user presentation profiles, response-rule precedence, HWID limits and optional subscription pages are described in [administration workflows](ADMIN_WORKFLOWS.md#named-subscription-templates-and-profiles).


## Independent WireGuard devices

Native WireGuard subscribers can register a separate named device from the portal
or an admin/operator's customer drawer. Each registration gets an independent
key and tunnel address, plus a WG-only subscription URL; ordinary account
subscriptions retain their legacy single peer. Give each physical device its own
registration: copying one device configuration to two clients still lets their
endpoints replace each other. Managed reverse WG remains a server-to-server
connection and does not use these subscriber device peers.

Registration requires a currently paired full server, bosun v0.77.0 or later,
and a fresh report advertising the installed Xray binary's WG device capability.
Existing device subscriptions and desired peer state retain the latest capability
for the current pairing during a brief disconnect. Re-pairing clears that evidence;
unknown or incompatible capabilities return 503 instead of an empty subscription.
An explicit `entry` selection checks only that authorized entry; a full device
subscription fails as a whole if any of its eligible servers is unconfirmed.
Disabled accounts get 403; missing access, expired plans, blocked/disabled entries
or revoked device credentials get 404. Client-chain entries are excluded because
a WG-only credential must not disclose another protocol's front proxy credentials.

Use an explicit `client=wireguard`, `clash`, `stash`, `singbox` or `egern` format
on device URLs. Native `.conf` downloads require `entry=<id>` when more than one
WG entry is eligible. Unsupported formats return 400. Device subscriptions use
built-in renderer templates and omit arbitrary entry client fields, so custom
static proxies, providers or credential overrides cannot widen their scope.
Ordinary subscriptions keep their existing templates, fields and response rules.

The registration limit follows the user's override, subscription profile and plan,
with a maximum of 16 native WG devices per account. When HWID checking is enabled,
registered native devices reserve shared device slots alongside existing HWID
clients. A native device URL authenticates that device directly and does not
require an `x-hwid` header; changing an HTTP header cannot add another peer.
Deleting a native device or rotating the account subscription token releases its
slot and revokes its URL. Both individual and bulk token rotation revoke these
devices. The agent removes revoked peers when it applies the updated state, so an
offline server cannot be promised immediate disconnection. Tunnel address slots
can be reused after deletion, but device IDs and random credentials never are.
