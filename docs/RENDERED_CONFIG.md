# Activated core configuration evidence

The administrator server diagnostics view can show evidence from configuration
files actually accepted by bosun's managed proxy cores. It does not download
native configuration or reconstruct a core's current memory. Older agents and
unsupported/failed observations remain unavailable.

Each artifact identifies the core, format, number of activation files and
processes, activation time and an opaque digest. Digests use an in-memory secret
and are comparable only within a bosun process; restarting the agent or replacing
an adapter may change the digest without changing configuration. No passwords,
user identifiers, addresses, file paths or raw native JSON are sent to Captain.
External certificate contents, host firewall state, realm forwarding and Internet
reachability are outside this proxy-core artifact view.

A `runtime_updates` marker means accepted user/API/auth updates followed the
activation. Those updates are not part of the activation-file digest. Immediate
credential revocation, authorization and billing retain their existing behavior.

Evidence accompanies the same report's configuration receipt. Captain only
stores it when that independently numbered receipt is accepted for its delivery,
process session and complete state. Old reports cannot refresh it. Missing or
malformed optional metadata cannot reject valid accounting; they replace the
artifact observation with an explicit unavailable/invalid state. A dead or
replaced process, failed apply, pending desired state or expired receipt cannot
be displayed as currently confirmed. Historical artifacts do not prove current
health.

`GET /api/admin/nodes/{id}/rendered-config` is administrator-only and requires
both `rendered-config:read` and node reading permission for API tokens. It has
`Cache-Control: no-store`. It returns the latest twenty deliveries plus the active
head if older, with at most one bounded artifact observation per retained
delivery. Migration 108 adds a child table; existing delivery retention, node
deletion and site reset clean it up. The interface performs no deployment or
core restart and cannot activate diagnostics or monitoring.
