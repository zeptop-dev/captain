# Standalone inbound credentials

Development feature, not released or deployed yet. Planned compatibility floors:
Captain v1.68.0 and bosun v0.99.0; confirm the actual containing releases before tagging.

Create/edit an inbound and choose **Standalone use** after selecting SOCKS5 or
Shadowsocks 2022. SOCKS uses one explicit username/password. SS2022 uses one
cipher-sized base64 server key; no subscriber identity key is appended. Existing
listeners keep their subscriber behavior unless an administrator explicitly switches.

Standalone inbounds never appear in plan selections or subscriber-node creation.
The store rejects legacy access grants and subscription entries too, and the line
compiler rejects them as managed landings. An ordinary forward to a manually supplied
address remains an independent network operation. Credentials do not create a customer,
plan or subscription and do not deduct customer quotas. Host-interface traffic remains
counted under the existing interface/cycle settings. Per-inbound usage display is not
part of this feature. Server service-control policies still apply.

An existing listener must have no subscriber entries, lines or grants before it can
switch. Rejected writes leave the previous listener intact. Switching back retains an
explicit unassigned policy. Direct management requires inbound-write and node-access
write authority; the topology draft editor deliberately excludes these listeners and
their credentials. Existing admin node-detail permissions still govern credential
viewing. Connection-copy is a JSON description, using the saved external ingress and
mapped port when configured; it does not create NAT/firewall mappings.

A recent actual adapter report must advertise `standalone_auth`. A first state pull
also requires `X-Bosun-Standalone-Auth: 1`; this prevents a downgraded process from
receiving credentials based solely on the previous process's stored report. The state
request returns409 if its desired state contains an independent listener but the
request lacks this capability. Old nodes without independent listeners are unchanged.
Independent inbounds have no EffectiveUsers, no customer counters, and their log lines
are excluded from customer online-device attribution, including username collisions.

Shared validation restricts this initial mode to SOCKS5 and SS2022. Managed reverse,
WireGuard slots, ShadowTLS and per-user private-access grants are not combined with it.
The existing default destination guard is not relaxed. All state fields are additive;
settings are stored in the existing inbound JSON, with no schema migration. Configuration
structure records only the independent-mode boolean; credentials remain outside the
structural digest, while complete state revisions still change on credential rotation.
