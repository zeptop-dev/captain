# Subscription node connection IP version

The node editor places **Connection IP version** next to the published host
and port. It controls the client connection to that endpoint. It does not
change the server listener, server egress, DNS records, geolocation policy,
or the intermediate hops of a server-managed line.

- **IPv4 only** resolves the connection hostname using IPv4. It does not fall
  back to IPv6 when IPv4 fails.
- **IPv6 only** uses IPv6 and does not fall back to IPv4.
- **Dual stack (automatic)** permits both families. Selection and concurrent
  racing depend on the client and protocol; this is not a latency guarantee.
- **Follow client/template** preserves the historical behavior, including
  existing client-specific extra fields. Existing entries start here.

New hostname entries default to dual stack in the editor. Literal IPs use
their matching family. Dual stack requires a hostname; a literal of the
opposite family is rejected. The saved hostname remains a hostname rather
than a snapshot of one DNS response. Both DNS records, the listener and the
client network must actually support the selected family.

For a hostname entry, the create form can create IPv4, IPv6 and Dual variants
in one transaction. They share the inbound, hostname and port, while each
retains its own subscription identity and policy. A failed creation leaves
none of the three entries. If the browser cannot determine whether a request
completed, inspect the node list before creating again.

## Format compatibility

The policy is a typed `Entry.IPVersion` field, not an unconverted JSON extra.

| Format | Hostname IPv4/IPv6 restriction | Dual stack |
| --- | --- | --- |
| Mihomo / Clash Meta | Per-node `ip-version` | `dual`; IPv6 admission enabled when not explicitly set |
| sing-box | Per-outbound `domain_resolver` strategy | Native resolver default; no racing guarantee |
| Surge | Per-proxy `ip-version=v4-only` / `v6-only` | `dual`; IPv6 admission enabled when not explicitly set |
| Other formats and share URIs | Omitted when no verified family setting is available | Hostname retained; depends on client settings |

The ordinary protocol support matrix still applies. WireGuard does not have
an independently verified endpoint-only family selector in these formats:
Mihomo's field also changes tunneled target DNS, while legacy sing-box uses
separate endpoint resolution. For WG hostnames, choose automatic behavior;
strict hostname choices and batch creation are rejected. A matching fixed IP
can be published without a family selector, subject to protocol support.
If an existing device entry becomes incompatible, its subscription returns
503 instead of an empty document that could remove installed devices.

Unsupported forced-family entries are omitted rather than exported as
automatic connections. Their portal cards show the selected family and
explain that a compatible full subscription is needed; a misleading standalone
share link is not offered. The editor lists family-option compatibility.

Mihomo and Surge templates explicitly disabling IPv6 conflict with IPv6 or
dual entries. They are not silently overwritten: subscription generation and
user preview return a clear 422 error. An absent IPv6 setting is enabled only
when emitted entries require it. The editor checks saved global templates
when the caller can read them; per-profile templates and response rules are
checked when the final document is generated.

Client-chain destinations are resolved through the preceding proxy in some
clients. Select the policy on the directly connected front node; explicit
policy on the dependent landing node is rejected. A missing or incompatible
front also removes its dependent chain from that client format.

Conflicting raw client fields, including endpoint, resolver, proxy-detour and
IP-version overrides, must be removed before choosing a typed policy. Legacy
entries retain their raw fields while in follow-client mode.

## API and upgrade behavior

Migration 116 adds `entries.ip_version` with an empty default. Existing API
paths and response fields remain; `IPVersion` is additive. Omitting it on
create preserves legacy defaults; omitting it on update preserves the saved
policy. Empty explicitly selects follow-client behavior.

`POST /api/admin/entries/variants` accepts the usual create payload and creates
the three fixed policies atomically, returning `{ "entries": [...] }`. It
uses the same entry and inbound-access permissions as ordinary creation.
`GET /api/admin/entries/ip-version-support` accepts `host`, `protocol`, and
`ip_version` and returns family-option compatibility, without resolving DNS.
Template summaries require the original template-read permission too.

This is panel-side subscription generation using the updated shared bosun
module. Existing agents require no new state fields or runtime behavior.
Older panels do not understand this policy and can generate unrestricted
connections after a downgrade. Before release, set the supported rollback
boundary to the first release containing this feature; do not promise that
an older panel preserves a forced-family selection.
