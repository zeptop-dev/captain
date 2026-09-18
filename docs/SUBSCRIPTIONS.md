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
appears for that group. Admin → Entries can drag entries into the order
clients see, give them free-form tags (shown in the portal, usable as a
filter and in the designer) and a region.

Settings → Subscription → *Auto flags* prefixes names that lack a flag
emoji with their region flag: the explicit region if set, otherwise
detected from the name (`Tokyo`, `HK-02`, `香港`, `jp1.example.com` …).

Each entry can carry **client extra fields** — a JSON object merged into
its mihomo, Stash and sing-box proxy (`tfo`, `smux`, `dialer-proxy`,
`ip-version` …) for the knobs only some clients understand.

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
