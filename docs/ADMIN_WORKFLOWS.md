# Fleet administration workflows

Available in Captain 1.8.0. These features retain the single SQLite database,
staff/customer separation and existing management API contracts. Migrations
58–64 run automatically; take a backup before upgrading. A binary rollback
across these migrations requires the pre-upgrade database snapshot.

## Traffic delivery

Captain accepts the report receipt, customer charges, inbound totals and
outbound totals in **one transaction**. A database failure rejects the report
so the node can retry. The additive `traffic_epoch` separates reporting
generations; the existing sequence-only path remains supported for old nodes.

bosun 0.57.0 keeps one immutable report batch in a private, fsynced journal.
A lost HTTP response or process restart resends that batch unchanged. New
traffic remains in core counters until acknowledgement. Back up the node's
data directory and do not delete its `traffic/` directory during an upgrade.
There is still a small unavoidable gap between resetting an upstream core's
counters and writing the journal: those APIs have no transaction/acknowledgement
facility. Uncollected counters can also disappear when a core crashes. This
does not claim zero loss for arbitrary host/core failures.

## Automation tokens

Account security / Settings lets staff grant a token named resources and
actions such as `users:read`, `users:write` or `nodes:*`. Exact endpoint grants
use the router pattern, for example `GET /api/admin/users/{id}`. `*` means all
resources allowed by the token's base mode and the staff member's role.

- Omitted `Scopes` keeps the legacy full/read behavior. An explicit empty
  array grants nothing. Existing tokens do not silently change permissions.
- Grants only narrow access: a read token cannot write, and a support token
  cannot become an operator. REST and MCP use the same policy. MCP tools with
  multiple underlying resources require every grant they use.
- Restricted tokens cannot create successor tokens or enroll authentication
  credentials. Passkey management requires an interactive staff session even
  for a legacy full token.
- `GET /api/admin/tokens/scopes` returns the resource catalog.

## Users and bulk operations

The user directory combines email search, status, usable subscription, group,
plan and expiry filters. Subscription filters inspect every active plan, not
just the summary row. Sorting, visible columns and page size stay in this
browser. Metadata is a private key/value map on users and nodes; it is not
included in public status pages or customer subscription payloads.

Select up to 200 explicit user IDs, choose an operation, review the server's
preview, then apply. Ban/unban, group changes, permanent URL rotation, expiry
extension, usage reset and deletion return per-user outcomes. Extension and
usage reset require a specific plan; queued subscriptions are not changed.
URL rotation changes the permanent subscription URL. Previously created
temporary links and credentials downloaded from ordinary subscriptions retain
their previous behavior. Since Captain v1.35, both individual and bulk rotation
also revoke all WireGuard device links immediately; downloaded device
configurations stop working after the servers synchronize. The confirmation
explains this additional impact before applying the operation.

Previews expire after ten minutes and belong to the employee who made them.
Execution checks immutable user identity and relevant account/subscription
state again. Changed accounts are skipped; unexpected database errors roll
back the entire batch. Repeating a completed request returns its recorded
result without applying it twice. Results are retained for thirty days.
Orders, commissions and invitation history still prevent physical deletion;
keep those accounts banned instead. Every write goes through the admin log.

API: `POST /api/admin/users/bulk/preview`, then
`POST /api/admin/users/bulk/jobs/{job}` with `{"confirm":true}`;
`GET` on that job retrieves its result. Metadata uses
`GET/PUT /api/admin/users/{id}/metadata` and the corresponding `nodes` route.

## Staff Passkeys

Use **Account security → Passkeys** to register a device or security key.
Registration and revocation require the current password and enabled TOTP.
Login uses a discoverable, user-verified credential; enabled TOTP remains
required after the Passkey check. Password login is retained as recovery.
Deleting a Passkey prevents future authentication with it; it does not revoke
already authenticated sessions.

Set `base_url` to the real HTTPS origin before enrolling. The relying party
and allowed origin come only from that configuration, not request headers.
Local development may use HTTP on `localhost`; a literal IP address is not a valid Passkey RP domain. Changing the domain
requires enrolling credentials for the new relying party. Passkeys are
staff-only; they do not grant portal access or proxy credentials.

Challenges expire after five minutes, are single-use and browser-bound.
Registration is additionally bound to the initiating staff session. Random
WebAuthn user handles survive account renumbering. At most ten credentials
per staff member are stored. The server keeps public keys and authenticator
metadata; the private key stays in the authenticator. Site reset removes all
Passkeys and recreates only the administrator's password/TOTP account.

## Named subscription templates and profiles

**Subscription templates** now includes a reusable template library and named
profiles. Templates have an immutable format and can be copied, edited and
rendered against a sample. A profile binds templates by format and optionally
sets the title, allowed response headers, flag/remark presentation, info lines,
HWID policy and a small branded subscription page. User detail assigns one
profile explicitly; unassigned users use the selected default profile.

Precedence:

1. An explicit user profile wins over the default profile. It is a complete
   profile selection, not a merge with the default. Omitted profile settings
   inherit the global subscription settings.
2. A matching response rule's template/headers win over the profile.
   Otherwise the profile's format binding wins over the global text template,
   then the built-in renderer. Blocking rules always apply.
3. An explicit user device limit wins over the profile device limit; absent
   both, existing plan/global fallback applies. HWID enforcement and access
   checks are not bypassed by the branded page or preview.

Allowed headers are `Profile-Title`, `Profile-Update-Interval` (1–168 hours),
`Profile-Web-Page-Url`, `Support-Url`, `Announce` and `Announce-Url`.
URL values must be HTTP(S). Security, cookie, content and traffic-accounting
headers cannot be overridden.

Enable a profile's page and visit the existing subscription URL with
`?client=page`. It offers standard format downloads, uses no external assets
or scripts, and escapes operator text. It is still a bearer URL: share it
only with the intended subscriber. A page view consumes one use of a temporary
link; following a format link consumes another. Browser visits without an
HWID header cannot pass a policy requiring that header.

Profile previews use a selected user's current lines without changing the
assignment, claiming devices or consuming temporary links. Profiles do not
grant plans, groups or inbound permissions. Assigned/default profiles and
referenced templates cannot be deleted until their references are removed.

## Configuration presets

Inbound editors and routing editors have named preset/fragment libraries.
Capture a draft, edit its typed fields, preview the difference and load it
into the editor. Nothing changes on a node until the ordinary Save action.
Editing/deleting a preset does not change existing listeners automatically.

- Inbound presets replace protocol settings while preserving the target
  tag/listen/port, group and user permissions. They strip source server keys,
  users and certificate paths. Loading generates fresh keys, so applying one
  to an existing listener can invalidate clients. Standard TLS uses the
  target node's certificate automation. Compatible cores are shown; the
  normal form/save also checks the node's current capabilities and ports.
- Outbound fragments use typed share-link remotes, node-owned WARP accounts
  or balancers. Remote endpoint credentials are intentionally stored in the
  private library. Raw core JSON outbounds are not accepted as presets.
- Route/outbound fragments append in order and preserve DNS/default exit.
  Duplicate tags, missing references and mixed chain/balancer cycles fail.
  Review all affected listeners: routing is node-wide. Routing support still
  depends on the selected core; Hysteria's existing routing limits remain.
- Presets cannot replace accounting identities, control APIs, private-network
  guards or arbitrary core overrides. The preview is a typed desired-state
  diff, not a dump of generated secret-bearing core files.

Libraries use `/api/admin/config-presets` CRUD and a `POST /preview` endpoint.
They are available to admins/operators and follow the `config-presets`
token scope. bosun's standalone UI offers the same behavior under
`/api/config-presets`; managed/fixed mode cannot mutate its local library.

## Infrastructure costs

**Infrastructure costs** is administrator-only. Add a supplier, then link an
asset optionally to a node. Record its currency, amount in minor units,
monthly interval, anchor day and next due date. Zero months means a one-time
expense. Importing a node's display details copies descriptive information;
confirm amount/currency manually because the old price field is free text.

After paying the supplier outside Captain, use **Record payment**. The server
writes the immutable supplier/asset/currency/amount snapshot and advances one
billing period in one transaction. A request key makes retries idempotent;
the asset revision prevents two tabs from advancing the same period. Paying
a one-time asset archives it. January 31 advances to February's last day,
then back to March 31 when the anchor is 31. Dates use UTC calendar days.

Amounts are integer minor units (USD 500 = $5.00, JPY 500 = ¥500). Totals group
by currency without exchange-rate conversion. Recorded payments cannot be
edited/deleted. Assets with history can be archived but not deleted, and
suppliers with assets cannot be deleted. Deleting a node unlinks the asset
and retains its history. These records never alter customer balances, orders
or commissions. Site reset deliberately clears this ledger too.

Due/overdue items remain visible in the console. A configured administrator
Telegram chat receives daily renewal reminders from the reminder window
onwards; unconfigured channels send nothing. Leases and durable receipts
avoid normal retries/restarts duplicating notices; a failure after remote
delivery but before receipt persistence can still cause a retry. No SSH access,
automatic payment or third-party billing credential is required.

API lives under `/api/admin/settings/infrastructure`: supplier/asset CRUD,
`POST /assets/{id}/payments`, and paginated `GET /payments` (100 rows per page,
`offset` and optional `asset_id`). A payment requires `request_key`, `revision`,
`amount_minor` and `paid_date`; optional `reference` and `notes` are retained.

The token editor obtains its permission suggestions from `GET /api/admin/tokens/scopes` (fixed in Captain v1.8.1). It requires an authenticated staff session or a token with read access to the token resource; the catalog grants no additional permission.
