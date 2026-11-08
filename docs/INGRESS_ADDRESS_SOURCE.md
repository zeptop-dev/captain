# NAT public address inheritance (unreleased)

Migration 117 adds an explicit source for a NAT ingress's public entry address.
Existing ingresses keep `custom`; saving a node's observed or location IP never
silently opts an ingress into inheritance. No bosun protocol change is required.

An ingress response adds `entry_address_source`:

| Source | Effective `entry_host` |
| --- | --- |
| `custom` | The saved provider IP or hostname |
| `server_ipv4` | The node's configured `public_addr`, as a usable IPv4 literal |
| `server_ipv6` | The node's configured `v6_addr`, as a usable IPv6 literal |

Editor requests use `EntryAddressSource`. Selecting a server source resolves the
current value and ignores a simultaneously supplied `EntryHost`. A missing,
unspecified, multicast or wrong-family address rejects the save. Inheritance is
available for NAT ingresses; IPLC and legacy mapped ingresses retain custom
addresses. `EntryDomain` remains optional and takes priority in client endpoints;
its DNS target is the resolved `entry_host`. Domains use the same normalization
as node domains, including case, trailing dots and internationalized names.

An old client that omits `EntryAddressSource` keeps the saved source. If it
explicitly changes `EntryHost`, that edit switches to `custom`; omitting or
sending null for both fields preserves the source and host. Explicitly sending
`custom` stops subsequent server-address changes from altering the ingress.

Only the server's configured entry addresses are inherited. Confirmed ordinary
observations can change them when their existing per-family follow switch is
on; confirmed fallback observations also require the separate fallback opt-in.
Unconfirmed observations, the IP used for geolocation and raw report values are
never inputs to this selection. Discovering the server's outbound public IP
does not establish its inbound NAT address, port mapping, supported protocols
or client reachability. Choose inheritance only when that address is also the
provider's actual inbound address.

## Subscription endpoint binding

Ordinary subscription entries add `FollowIngress` (default `false` for migrated
rows). When true, their host and mapped port come from the current ingress of
their selected inbound. Explicitly enable this option to adopt an existing
entry. A newly created entry using blank default host and port adopts its
ingress unless `FollowIngress:false` was explicitly supplied. Entries created
with explicit addresses remain fixed, even if those addresses happen to equal
the ingress. Client editors that fill defaults themselves send
`FollowIngress:true` to retain a binding.

A partial entry update preserves the binding. Older clients that omit the flag
and actually change the host, port, inbound or line reference detach it; an
unchanged reference, including `line_id:0` for an absent line in topology drafts,
does not detach it. `FollowIngress:false` freezes the current explicit endpoint.
Server lines and legacy server chains keep their existing endpoint ownership
and cannot also use `FollowIngress`. The binding requires an ordinary ingress
inbound and cannot be attached to a reverse/WireGuard managed listener.

Node address changes, ingress edits and inbound port/ingress edits update the
selected inherited endpoints and their bound entries in one transaction.
Existing fixed entries are untouched. The same transaction recompiles affected
lines and checks active release restrictions; a missing address, incompatible
entry IP policy, mapping failure, protected release or failed database write
rolls back the complete change. Topology drafts support `follow_ingress` and
include affected implicit entries in their existing permission and preview
checks. Address changes invalidate old previews; DNS retry bookkeeping does not.

## DNS and downgrade behavior

Ingress DNS intent is persisted with the endpoint. The address worker retries
current pending intent, including manually edited unpaired servers, with a
five-minute failure backoff and a bounded 25-second batch. A later address edit
replaces the pending target. The worker rereads under the topology lock, so a
retry does not publish an old queued IP. Provider failures and address ownership
conflicts remain pending. Disabled or unmanaged automatic DNS is skipped under
the existing DNS policy. Node-edit responses add `ingress_dns_pending`; their
existing `dns` list reports only the node-domain operation, not completion of
all inherited ingress records. This feature does not automatically delete old
DNS records when the domain or address family changes.

Migration 117 is forward-only. SQLite backups include the source, entry binding
and pending DNS state; site reset deletes them with their parent objects. No
existing endpoint or same-looking manual entry is reclassified by migration.
Before releasing this feature, raise Captain's built-in rollback floor to the
actual release containing it: an older panel ignores these bindings and can
leave stale subscription addresses after node updates. This development change
does not itself declare a new version or an installed-agent requirement.
