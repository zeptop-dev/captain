# Structural releases and reusable paths

Captain v1.49.0 adds optional staged releases for ordinary TCP relay lines and
fixed-version path templates for complete candidate routes. Existing line saves
retain immediate delivery. The new workflows are administrator-only and use
separate `line-rollouts`, `forward-paths` and `path-routes` API token resources.
Captain v1.50.0 extends staged releases to complete routes through the separate
`path-rollouts` resource. Immediate saves remain available.

## Staged line releases

Open a line editor and choose **Staged release**, or choose **Staged removal**
from its card. Review the before/after forwarding snapshots and ordered servers
before starting. A server must have a fresh, applied configuration receipt from
bosun v0.84.0 or later. An unchanged landing server is a prerequisite, not a
fictional modified target. Opaque configurations that cannot produce structural
receipts are unsupported.

The immutable snapshot contains only this line's forwarding fields and its
declared intent. Users, passwords, access expiry, pending jobs and server traffic
controls continue through the current state builder. Pausing a release cannot
freeze a customer's access or postpone a service pause.

1. **Expand** installs the union of old and new listeners from downstream to
   entry. Each step waits for an authenticated, independently sequenced receipt.
2. **Publish** changes the saved line and its subscription endpoints together
   after all required expansion steps confirm. New lines cannot acquire Entry
   bindings before publication.
3. **Cleanup** removes old listeners from entry to downstream, again waiting for
   each server's receipt. Deletion first requires detaching subscription Entries.

Changed listeners must safely coexist. A target change on the same listening
port is rejected; assign separate ports for the new path. The active plan
reserves its resources, so ordinary editors cannot steal a future port or
overwrite its owned line. Unrelated lines remain editable. TCP relay is the
supported backend: UDP, nft, realm and reverse connections cannot use staged
releases. Complete TCP relay routes use the dedicated workflow below.

**Release history** keeps the snapshots and individual attempts. Pause stops
advancement and retains the last released configuration. Failed or overdue
receipts pause the plan; the per-step timeout defaults to 300 seconds and can be
30–3600 seconds. Retry creates a new attempt and delivery identity, so a delayed
success from an earlier attempt cannot advance it. Refresh the page after a
version conflict before choosing another action.

Compensation creates a separate plan from the earlier intent using the current
compiler and current topology. It is not a database restore or a replay of an
old full agent state. It can be refused when the original intent can no longer
be safely compiled. Completed historical steps do not promise current health;
fresh receipt evidence and end-to-end service checks remain separate.

API entry points:

- `POST /api/admin/line-rollouts/preview` and `POST /api/admin/line-rollouts`
  accept `{ "line": <ordinary line>, "remove": false, "timeout_seconds": 300 }`.
  Send the normalized preview `draft` with its `preview_revision` when starting.
- `GET /api/admin/line-rollouts` accepts `line_id`, `before` and `limit`;
  `GET /api/admin/line-rollouts/{id}` reads a particular plan.
- `POST /api/admin/line-rollouts/{id}/pause`, `/retry` and `/compensate` require
  `{ "version": <current plan version> }`.

## Complete candidate paths

Open **Nodes & lines → Lines → Forwarding lines → Complete routes**. Create
path templates first, then combine their fixed versions behind one entry server
with one final inbound. For example, A can select the whole path B1 → B2 or the
whole path C1 → C2 before both reach D. B1 is never arbitrarily combined with C2.

A template step is one server, a fixed revision of another template, or a fixed
version of an ordinary linear line. A reused source line must use TCP relay and
have one server per group. Its listening ports and final inbound are left
unchanged; optionally include its landing server as another new relay hop.
References preserve the selected version when a source changes. Editing a
template creates a new immutable revision; existing routes do not upgrade
automatically. Cycles and oversized expansion are rejected.

Assign each expanded hop its own explicit listening port. Reusing a physical
server in different candidates still requires distinct listeners. Preview the
exact expected rules before saving. Failover and weighted round-robin choose
the candidate's first hop for each new TCP connection. A first-hop TCP probe
does not detect every failure deeper in the candidate, and balancing does not
combine bandwidth for one connection.

The route owns dedicated forwards and a protected backing line used by normal
subscription Entries. Its editor and API own changes to this backing line;
ordinary line editors cannot rewrite it. Source edits do not change pinned
sequences, while live address/ingress updates safely recompile the route's
current connections. Dependency checks protect referenced resources. Service
status evaluates the actual listener adjacency, not a Cartesian product of
servers at each depth.

The APIs are `/api/admin/forward-paths` and `/api/admin/path-routes`, with
revision reads, use inspection, optimistic versions and route previews.
Templates describe structure, not credentials or existing shared listener
ownership. TCP relay is supported; arbitrary reverse-tunnel edges, UDP, nft,
realm remain outside this implementation.

## Staged complete routes

In a complete-route editor, choose **Staged release**, review the ordered
listener changes, then start the plan. Route removal also has a staged action
after detaching subscription Entries. Ordinary `line-rollouts` endpoints cannot
read or operate these plans; use `path-rollouts`.

Each candidate expands from its last relay toward its first relay, then the
entry is published last. Cleanup removes the old entry before its branches.
Ordering follows dedicated listeners: if two candidates share a physical
server, that server may receive several cumulative configurations. Confirmation
of an earlier configuration cannot satisfy a later step on the same server.

Publication changes the saved route, its fixed template references and bound
subscription endpoints in one transaction. During expansion, the old intent
remains published; a newly created route cannot acquire subscription bindings
until it is published. Active plans protect both old and future template
references. Pause, new-attempt retry and explicit compensation have the same
semantics as ordinary releases, including rejection when the current topology
cannot safely reconstruct the destination intent.

- `POST /api/admin/path-rollouts/preview` and `POST /api/admin/path-rollouts`
  accept `{ "route": <complete route>, "remove": false, "timeout_seconds": 300 }`.
  Use the normalized `draft` and its `preview_revision` when starting.
- `GET /api/admin/path-rollouts` accepts `route_id`, `before` and `limit`;
  `GET /api/admin/path-rollouts/{id}` reads a plan.
- `POST /api/admin/path-rollouts/{id}/pause`, `/retry` and `/compensate` require
  `{ "version": <current plan version> }`.

Optional [path handshake health](PATH_HEALTH.md) is a separate policy and
observation stream. A configuration receipt does not prove path health. A plan
that changes health policy, including compensation, also requires the health
write permission; publishing an unchanged policy does not add that permission.

## Upgrade and recovery

Migrations 100–103 add configuration receipt evidence, line change history,
release plans and immutable path definitions. Existing user identities,
subscriptions and accounting records keep their meaning. Install bosun v0.84.0
before using confirmation or staged releases. Existing agents retain their
ordinary immediate-delivery behavior.

Migration 104 adds complete-route release intent and protected template
references; existing ordinary plans retain their meaning. Complete-route staged
delivery alone still uses bosun v0.84.0 receipts. Enabling path handshake health
requires bosun v0.85.0 and fresh capability evidence.

The built-in Captain rollback floor is v1.50.0: an older panel cannot preserve
complete-route release intent or health policies. Manual downgrade requires
a matching database backup and a separately verified runtime recovery plan.
New release/path records are included in SQLite backups and cleared on site
reset. Never use an old snapshot to restore revoked user credentials.
