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

The visual designer (bosun's `pkg/subdesign`) combines an editable base design
with optional enhancements. Its single preset selector lists **Basic** first,
then the cached official ACL4SSR presets, then administrator-saved custom
presets. Basic routes LAN and Chinese domains/IPs directly and all other
traffic through the proxy selector; it does not include advertising filters.
Legacy `acl4ssr` and `acl4ssr-regions` preset API keys remain loadable but are
not presented as official imports.

**Sync official presets** retrieves all `.ini` files in ACL4SSR's `Clash/config`
directory at one fixed Git commit. Downloads are bounded and checked against
their Git blob IDs. A failed sync preserves the old cache; a successful sync
changes neither the draft nor published templates. Unsupported syntax remains
listed with a reason and cannot be loaded as a silently simplified design.
The saved source revision pins the **preset structure**. Rule-list URLs inside
it retain their upstream update behavior; this does not freeze list contents.
Imported/adapted configuration data retains its ACL4SSR source and
[CC BY-SA 4.0 attribution](https://github.com/ACL4SSR/ACL4SSR/blob/master/LICENCE).
No upstream presets are embedded in the MIT application source.

The selector also shows **Official file notes**, extracted from introductory
comments in each cached INI file. Extracted notes and a link to that exact upstream
revision appear before loading and beside the selected preset. Existing caches
already contain the comments, so displaying them does not require another sync.
These are upstream comments, not a complete feature comparison; missing notes
are identified explicitly. If the library is refreshed while an older draft
is open, the notes identify the library version without changing the draft.

The current enhancement options are:

- **Supplement regional groups:** add missing Hong Kong, Taiwan, Japan,
  Singapore, US and Korea selectors. Each new region has an automatic latency
  group first, followed by all matching nodes for manual selection. Automatic
  checks use a 300-second interval and a 50-ms switch tolerance where the client
  supports it. Explicit entry regions take priority; otherwise grouping uses
  the original node name, before profile prefixes or automatic display flags.
  Ingress hostnames and IP geolocation do not establish the exit region.
  Generated empty regional groups are omitted after access and client-protocol
  filtering; groups explicitly used by a rule, final or DNS policy remain as
  REJECT. Other empty generated groups still reject traffic. Existing custom
  or imported name-match groups keep their behavior, and disabling the
  enhancement leaves the editable base unchanged. Save and apply the design
  again to update published templates; saving a draft alone does not publish.

  The member picker uses `@region:HK` (template `{{proxy_names:region=HK}}`)
  for region selection. Existing `@match:` / `match=` expressions retain their
  name-matching semantics. A node with no identifiable region remains available
  in all-node selectors rather than being assigned an arbitrary country.
- **DNS protection:** native Mihomo, Stash, Surge and Egern DNS configurations
  with encrypted upstreams and optional Claude DNS policy. The constraints below
  distinguish application DNS from client bootstrap/proxy resolution.
  It does not force TUN, change the controller/listening
  ports or disable system IPv6. Client capture, browser DNS and local overrides
  still determine actual behavior; this is not a successful leak-test result.
- **Claude dedicated routing:** explicit service domains routed through a dedicated
  selection group or a selected existing proxy group. No blanket telemetry
  keywords, NTP/time-zone claims or availability guarantees. Rules precede the
  base rules; administrators can inspect the effective result before applying.
- **WebRTC mitigation:** reject common STUN/TURN UDP ports and known STUN
  names. This can affect calls and is not complete browser IP protection.
- **Restrict UDP 443:** a separate advanced option affecting common QUIC
  connections; it does not block all UDP or every possible QUIC endpoint.

**Update Claude rules** is a separate administrator action. It retrieves the
community-maintained [v2fly Anthropic list](https://github.com/v2fly/domain-list-community/blob/master/data/anthropic)
at one verified Git revision, then adds Captain's two exact-host supplements
(`anthropic.auth0.com` and `anthropic.com.cdn.cloudflare.net`). This is a
community classification, not a list maintained or endorsed by Anthropic.
Only suffix (`domain`) and exact-host (`full`) entries are supported; unsupported
syntax rejects the whole update and preserves the previous library.

The designer shows **library**, **draft**, and **applied global template per format**
versions separately. Updating the library displays additions/removals and does
not edit the draft or published subscriptions. Review the library-to-draft diff
and choose **Use in draft**, then preview and apply to publish it. Saved designs
and custom presets keep their selected rule contents, so later source updates
cannot change their rendering. Older designs without a snapshot retain the
original ten built-in rules until explicitly changed; disabling the option
keeps the saved version for later reuse. With DNS protection enabled, the same
snapshot controls Claude DNS policy, preserving exact versus suffix matches.

Applied-version status refers to each format's global template independently.
Publishing selected formats retains every unselected format's version. Existing
publications from older versions and manual text edits have unknown provenance;
subscription profiles, response rules and client overrides can use different
content. The panel does not claim that updating the cache publishes to clients.
The domain list and its upstream MIT attribution are preserved in generated
client output. Rule-library synchronization executes no remote scripts and
changes no node configuration.

Regional supplementation, Claude routing, WebRTC mitigation and UDP-443
restriction have adapters for **Mihomo (`clash`), Stash, Surge, Surfboard, Loon,
Quantumult X and Egern**. Exact hosts remain exact; suffix rules retain domain
boundaries; the UDP option never becomes a TCP-443 block. Imported rules are
checked individually instead of being restricted merely because they came from
an official INI. Inspection reports unsupported features and non-blocking
client requirements separately. Apply
writes only the selected supported global templates, atomically with the saved
draft; unselected formats keep their existing templates. The existing API's
omitted `formats` field still requests all seven and fails if any is unsupported.
Sing-box is not a visual-designer output format.

Client-specific limits:

| Client | DNS protection | Other requirements and limits |
|---|---|---|
| Mihomo | Proxied encrypted DNS; independent direct bootstrap | Existing remote-list behavior retained; a warning identifies unsupported URL-REGEX in ProxyMedia |
| Stash | Proxied encrypted DNS with `follow-rule` | Independent proxy-hostname DNS requires iOS/tvOS 3.6+ or macOS 4.3+; geosite data may need its first download |
| Surge | Proxied encrypted DNS, with IP-address proxy endpoints | A protected subscription containing hostname candidates or external proxy providers is rejected to prevent DIRECT fallback; only `.cn` gets the domestic resolver |
| Surfboard | The requested DNS proxy policy cannot be expressed | Its encrypted DNS is always direct; ProxyMedia's URL-REGEX cannot be represented |
| Loon | The requested DNS policy cannot be guaranteed by the documented profile | Disabling plain-DNS fallback needs an app setting; UDP logical rules require 3.1.7+; native domain-first matching remains |
| Quantumult X | Local DNS cannot be bound to the selected DNS proxy policy | UDP restrictions apply to captured tunnel traffic; process/URL rules in ACL4SSR `ProxyMedia.list` cannot be represented |
| Egern | Default DNS follows selected proxy policies | Its separate proxy-DNS path remains encrypted/direct; only `.cn` gets the domestic resolver; ProxyMedia's process rules cannot be represented |

Explicit group timeouts or differing per-group test URLs are rejected where
the target cannot preserve their meaning. These checks do not guarantee an
arbitrary future upstream list will remain compatible.
Process rules in Stash, Surge and Loon also have a macOS-only native scope;
the designer reports that limitation instead of promising equivalent mobile
behavior. Mihomo's established remote-list delivery remains available with an
explicit warning for the known unsupported URL rule, rather than claiming the
entire list is faithfully implemented.

Loon and Quantumult X use credential-free, bounded inline-rule resources hosted
by the panel to keep inline rules in their original position among remote lists.
Quantumult X also fetches Captain's embedded, versioned rule-conversion script;
it runs in the client's resource-parser environment, does not fetch URLs itself,
and rejects an entire resource on unknown rule types instead of dropping lines.
The panel does not fetch arbitrary rule URLs or disclose subscription tokens in
these resource URLs. Downloading resources still requires the panel to be reachable.

**Save draft** changes no subscription. **Save as preset** stores a separate
named copy including the base, enhancements and source attribution. Deleting
that copy cannot remove an already applied template. All library, synchronization,
draft, preview and apply endpoints are administrator-only. Users receive the
administrator-published template through the existing subscription precedence:
a matching response-rule template, then the selected/default subscription
profile's template, then the global template, then the built-in renderer.
Applying a global design does not overwrite named profile/response templates.
Independent WireGuard device subscriptions continue to ignore custom templates.

## Entries: what each user sees

An entry is a server as users see it: a display host and port on top of a
landing inbound's settings. An inbound restricted to a user group only
appears for that group. Admin → Nodes can drag subscription nodes into the order
clients see, give them free-form tags (shown in the portal, usable as a
filter and in the designer) and a region.

**Nodes & lines → Subscription nodes → Manage categories** manages display
categories with a name, a short plain-text description and an order. Each
subscription node can belong to one category; edit a node or select several
visible rows to assign them together. Managed reverse-connection nodes can also
be categorized through the batch action. Deleting a category leaves its nodes
intact and makes them uncategorized. Existing and automatically created nodes
start uncategorized.

Categories are separate from tags, regions, server monitoring groups and access
groups. They do not grant access, change billing or routing, add subscription
tags, or reorder the subscription. Category ordering controls category selectors;
nodes keep their existing order. Clear all list filters before dragging nodes
to change that global order. The customer portal shows category labels and a
category filter derived only from nodes the customer can access; empty or
inaccessible categories are not exposed. The same category data can support
future site and service-status presentation without defining publication policy.

The admin API adds `/api/admin/entry-categories` (list/create), `/{id}`
(update/delete), `/order` (complete ordered `IDs`), and
`PUT /api/admin/entries/category` (`IDs`, `CategoryID`) for atomic bulk assignment.
`Entry.CategoryID` is nullable. On ordinary entry PATCH, omitting it or sending
null preserves the current category; send `0` to clear it. Bulk assignment
requires an explicit category ID, including `0` for uncategorized. The portal's
existing server array adds an optional `category` object
(`id`, `name`, `description`, `sort`); no global category catalogue is public.
The portal rank is relative to the caller's visible categories. Category CRUD
uses the separate `entry-categories` API-token resource; assigning a node's
category uses `entries`. Both operations retain the existing administrator and
operator role restrictions. Migration 87 stores this display metadata; it adds
no bosun protocol fields and requires no node-agent upgrade.

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

Native WireGuard subscribers can register a separate named device from the portal's Connect page
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
