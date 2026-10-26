# Running the console: staff, access, integrations

Who can do what, how the panel is reached, and everything it can talk to.

## Finding a task

The console has six main navigation groups: Overview, Users, Nodes,
Subscriptions, Monitoring and Operations. On desktop, section headings group
always-visible common pages; only secondary settings and tools collapse.
Headings do not navigate. On phones, section buttons expand their pages, and
choosing a page closes the drawer. Secondary expansion survives navigation.
Settings stays at the bottom; account security is in the avatar menu.
Subscription profiles, templates and response rules have separate direct links
to the existing editor modes, with one active navigation item.

Settings is a directory, with separate pages for each editor. Registration and
trial rules are under Users; certificates, connection recording, audit and
speed policies under Nodes; subscription URLs, HWID and clients under
Subscriptions; collection, the public status page, ping tasks and heartbeat
under Monitoring; notices, referrals and purchase credit under Operations.
Global security, notification integrations and maintenance stay under Settings.
The directory search covers names and descriptions across these categories,
without loading configuration values. Moving a setting does not change its
admin-only API permission.

Editor URLs such as `/admin/settings/mail` can be bookmarked. Only the selected
editor loads its data. Unsaved changes survive background refreshes, and leaving
the editor asks whether to discard them. A failed initial load must be retried
before editing. User-group and monitoring tabs, and subscription template modes,
also keep their selection in the URL. Dashboard links preserve node, order and
asset filters in URLs as well. Existing page and API URLs remain valid.

Creating a user opens that user's detail drawer; granting a plan leaves it open
for subscription checks. Node details show connection, confirmed running
inbounds and enabled entries separately: an enabled entry is not proof of
end-to-end connectivity. An inbound's **Add entry** action carries its selection
into the entry editor. Alert rows link full administrators directly to the
node's existing network diagnostic form.

## Dashboard tasks

The dashboard places node issues and daily tasks before four business metrics,
the traffic chart and system maintenance checks. Offline nodes have paired but
not contacted Captain for three minutes; unpaired nodes are separate setup
tasks. Self-check failures describe the last reported check, and acknowledged
incidents remain unresolved until recovery. Asset reminders use each active
asset's UTC calendar date and reminder window, including overdue items. Pending
orders are payment status, not necessarily a manual intervention or a failure.

The existing `GET /api/admin/dashboard` adds an optional `attention` object.
Node, incident and asset counts require the source endpoint's role and API token
permission; omitted counts mean unavailable, never zero. No asset costs, notes,
node addresses or self-check output are included. Failed reads return an error;
the console marks any retained snapshot as stale and offers refresh. Traffic
labels use UTC to match accounting buckets; the current day is incomplete.

## User groups

Users → User groups lists and creates groups beside the user list and renewals
views. Assign a group in the user detail drawer or through a plan; inbound group
restrictions use the same groups. Admin and operator accounts can manage them;
support accounts cannot. The APIs remain `GET` and `POST /api/admin/groups`.

## Staff roles

Settings → Staff creates console accounts with a role:

| Role | Can |
|---|---|
| **admin** | everything |
| **operator** | everything except settings, system, staff, the landing page, and the audit rules and log — those write every node's core config and record where users went |
| **support** | tickets, plus read-only users, orders, plans and the dashboard. No connection log, no subscription links, and the user payloads leave the subscription token out |

Console accounts (`staff`) and customers (`users`) are separate tables in the
same SQLite database. Each has its own ID sequence: on a new installation,
admin 1 and customer 1 are different accounts. Creating staff does not consume
customer IDs. The same email may be used once in each table, with independent
passwords, sessions and OIDC links. Staff accounts have no proxy UUID or
subscription token; to use a subscription, create a customer and assign or
purchase a plan. Console credentials and cookies cannot access the portal.

Migration 52 preserves existing customer IDs, UUIDs and subscription URLs,
and moves staff passwords, TOTP, API tokens and OIDC links to the console
namespace. Valid console sessions survive; old staff sessions issued by the
portal are revoked. If a former staff account has orders, subscriptions,
balance or other customer history, that history remains attached to a disabled
customer with no password. It cannot sign in or use a subscription until an
administrator deliberately enables it; password login also needs a new
customer password. Staff without customer history is removed from `users` entirely. Existing databases
are not renumbered; restore the pre-upgrade snapshot to roll back this schema.

The last admin cannot be demoted, disabled or deleted.

**Choose a customer ID at creation** — the full administrator can optionally
enter an unused positive account ID in the Create user dialog. Leave it blank
for automatic numbering. `POST /api/admin/users` accepts the optional `id`
field (for example, `999999`); omitted or null keeps automatic allocation.
Explicit IDs must be between 1 and 9007199254740991. Invalid IDs return 400;
IDs already occupied by a customer or historical records return 409. Operators
can still create automatically numbered customers, but cannot choose an ID.

Migration 65 initializes an independent customer numbering cursor from the
highest existing customer ID, without changing any accounts. From then on,
manual creation and ID changes do not advance that cursor. Automatic creation
advances it and skips occupied IDs and retained history; deletion does not
rewind it. A fresh installation starts at 1, and site reset resets the cursor.

A manually created testing account is an ordinary customer: its ID can be
changed and it can be deleted under the same rules as other customers.
Directly granting a plan does not create an order, so testing subscriptions,
connections and traffic this way does not itself prevent deletion. A chosen
number has no special privileges or automatic exclusion from statistics.

**Delete a customer** — accounts referenced by orders, commissions or other
users' `invited_by` links cannot be deleted, to preserve that history. Keep
them banned instead. This also applies to disabled customer records retained
when staff accounts were separated. The delete API returns 409 with an
explanation; the console shows it in the selected language. Deleting an
eligible customer does not affect a staff account with the same ID or email.

**Change an account ID** — the customer detail drawer and the staff edit
dialog each have a separate Change ID control. Only the `admin` role can
use it, including for its own account. Choose an unused positive integer up
to 9007199254740991; customer and staff IDs are checked in their own namespaces.
An ID still referenced by another account's historical records is reserved.
The operation preserves subscription URLs, proxy credentials, balances,
orders, invitations, history, sessions, API tokens and two-factor setup.
Scripts using the numeric account ID must use the new ID for later requests.

The API is `PUT /api/admin/users/{id}/id` for customers or
`PUT /api/admin/admins/{id}/id` for staff, with `{"id":42}` as the request
and response body. Invalid IDs return 400, missing accounts 404 and occupied
IDs 409. As with all staff management, a cookie session is required for the
staff endpoint; API tokens cannot call it. Node accounting uses a separate
immutable identity, so pending traffic reports cannot be billed to a new
owner of the old account ID.

**Two-factor sign-in** — Account security → Two-factor authentication: each staff
account can add a TOTP authenticator, and the console then asks for the
code after the password. Re-enrolling requires the current code. The replay
guard is keyed on the step that matched, so the next code still works. API
tokens are unaffected.

## API tokens and MCP

Account security → API tokens & MCP issues personal bearer tokens (`cap_…`) for the
admin API and for AI agents. Captain serves the Model Context Protocol at
`POST /mcp` with tools for nodes, users, plans, orders, tickets, the probe
and TCPing; write tools require `confirm: true`. See [MCP.md](MCP.md).

A token carries its owner's role, narrowed by what it was issued with:

- **scope** — *read-only* answers only GET requests, and only the MCP read
  tools;
- **expiry** — 30, 90 or 365 days, after which it stops working on its own;
- staff accounts can never be managed with a token: `/api/admin/admins`
  needs the interactive login;
- `/mcp` sits behind the same admin allow-list as `/api/admin`, so an agent
  or a scraper has to come from an allowed address too.

## Access control and the client address

- **Admin allow-list** — Settings → Staff and site security → Console access control
  (`admin_allow_cidrs`: bare addresses or CIDRs) restricts the admin login,
  every `/api/admin` route including API tokens, and `/mcp`. Saving a list
  that would exclude your own address is refused.
- **Login throttling** — five failed sign-ins from one address within
  15 minutes lock that address for 15 minutes. The counter includes the
  TOTP step, and it lives in memory: a restart clears it.
- **Browser origin check** — cookie-authenticated writes (console, portal,
  and the login / register / reset routes) must carry an `Origin` or
  `Referer` of the panel's own host, so a page on another site cannot drive
  the API with a victim's session even where `SameSite=Lax` would let the
  cookie through. `Authorization: Bearer` requests and reads are exempt.
- **Session kinds** — customer sessions (password, OIDC or password reset)
  never reach `/api/admin`, and console sessions cannot enter authenticated
  `/api/portal` routes. Console sign-in uses the staff namespace, with TOTP
  when enabled. Changing a password drops only that account's sessions.
- **Which address is "yours"** — behind a proxy, `X-Forwarded-For` read
  from the right, skipping trusted proxies. The full rules, and how to
  configure `trusted_proxies`, are in
  [OPERATIONS.md](OPERATIONS.md#admin-access-allow-list-and-client-addresses).

## Theme and page injection

Admin → Brand and homepage includes the shared brand name, browser tab title,
light-background and dark-background logos, and browser icon (favicon). These
apply to the built-in homepage, public service-status page, customer portal,
sign-in pages and admin console. The existing portal title can still override
the portal header text. Page descriptions use the homepage description. This
does not change deployment `site_name` used by mail, notifications or subscription
headers, nor rewrite an operator-supplied custom homepage.

Upload PNG, JPEG or WebP images up to 2 MiB and 2048 × 2048 pixels, or enter an
HTTP(S) image URL. Uploads are decoded and stored as PNGs, with a 4 MiB output
limit; SVG and executable content are not accepted. Save the settings to use an
uploaded image. Supplying only one logo uses it for both light and dark
appearances; leaving both blank restores the default mark. A square PNG is
recommended for the browser icon. Images are served at public content-addressed
URLs and included in database backups; identical uploads share one stored asset.

Monitoring → Server probe has a **Follow main-site branding** switch. Existing
installations keep their independent probe branding by default. Enabling it uses
the main site's name, logos, browser title, description and favicon; disabling it
restores the probe's own title and uploaded logos/favicon. Switching never deletes
the independent settings, and an empty independent icon does not inherit the main
site's icon. Probe appearance and access controls remain separate. This applies
to all five probe themes and to probes served on dedicated hostnames.

Admin → Brand and homepage also includes primary colour, corner radius, light/dark/system
scheme for the portal, portal title, font, and raw HTML
injected before `</head>` and `</body>` on every portal and landing page
(analytics, chat widgets). The console keeps its own look.

The built-in homepage and public service-status page start in light mode.
A small header icon switches between light and dark; the current browser
remembers the selection for both pages on the same origin. This is separate
from portal/admin and server-probe preferences and is not synchronized to an
account. A previous Auto preference is treated as light, rather than following
the operating system. The connection section and illustrations follow the
selected theme. The legacy `theme.site_scheme` API field is retained unchanged
when editing site settings for compatibility; it no longer selects the built-in
homepage or public service-status appearance. The portal retains its own
operator-configured light/dark/system default.
Their geographic visuals are rendered locally from configured locations;
markers and illustrative connections are not live health or traffic. Operator
features, FAQ, registration policy, downloads and published plan data remain
the content source. Custom homepage files continue using their own appearance.

Portal sign-in, registration and password reset share this branding, including
the portal title, colour scheme, primary colour, font and corner radius. Visitors
can switch language before signing in. Authentication methods and registration
links follow the configured providers and registration policy.

The customer portal uses the homepage's fine borders, flat surfaces and light/dark
palette. Its compact header icon overrides the portal's configured default for
that browser, including the sign-in pages. Without a saved choice or configured
default it starts light. The independent `captain-portal-color-scheme` preference
is shared between same-origin tabs, does not change the site's or admin console's
preference, and is not synchronized to an account. Operator primary colour,
font and component radius settings remain available.

Overview shows each active plan's remaining allowance, usage, reset and expiry
separately; queued plans expand below them. Connect retains configured downloads,
supported client imports, QR codes, classified subscription nodes and WireGuard
devices. Account separates recently active devices from registered device slots.
The layout does not change authorization, quota accounting or payment status.

## Event webhooks

Settings → Notifications and integrations → Event webhooks: Captain POSTs JSON to your URLs with
`X-Captain-Event` and an HMAC-SHA256 `X-Captain-Signature` over the body,
retrying a failed delivery three times. This is the integration point for
n8n, a script or a CRM, in place of an in-process plugin system that a
single static binary cannot load.

Events: `user.registered`, `order.paid`, `ticket.created`,
`ticket.replied`, `withdrawal.requested`, `subscription.expiring`,
`subscription.traffic`, `user.first_connected`, `user.not_connected`,
`user.audit_hit`, `user.audit_banned`, `user.throttled`, `node.alert`.
Payload stability is part of the 1.x promise — see
[COMPATIBILITY.md](COMPATIBILITY.md).

## Support, knowledge base, Telegram

- **Tickets** — users open tickets in the portal (priority low / normal /
  high) and reply in a thread; Admin → Tickets answers them. A reply
  reaches the user on Telegram when linked, otherwise by mail, and new
  tickets can ping the admin chat.
- **Knowledge base and downloads** — Admin → Knowledge base holds Markdown
  guides grouped by category, with `{{sub_url}}`, `{{email}}` and
  `{{site_name}}` substituted per reader; Subscriptions → Client downloads lists
  the apps. Both appear under Help in the portal.
- **Telegram bot** — Settings → Notifications and integrations → Telegram: paste a BotFather token, and your
  chat id for order, ticket and node notices. Users link their chat from
  the portal with a one-time `/bind CODE`; the bot answers `/sub`,
  `/status`, `/unbind`, `/id` and delivers expiry, traffic and ticket
  notices. Plain Bot API long polling — no webhook, no public URL. One bot
  token cannot be shared with a bosun panel (Telegram allows one
  `getUpdates` consumer).

## Registration

Users → More → Registration and trials → Registration limits
(`/admin/settings/registration`) has an **Allow registration** master switch.
Saving it takes effect without restarting Captain. Off blocks password signup,
registration-code emails and automatic account creation through every OIDC
provider, including providers with `auto_register: true`. Existing accounts
can still sign in, link an external identity and reset passwords; administrators
can still create customers manually. Existing unwanted accounts must be banned
separately.

Before the switch is first changed, existing deployments retain their
`portal.registration` default and provider `auto_register` exceptions. The
editor shows current password and external-signup status separately. Saving
other limits does not silently replace these defaults. Once explicitly saved,
the master switch overrides both defaults: on permits either signup path,
subject to the existing limits, and off closes both. Old API clients that omit
the new optional `enabled` field preserve its stored value; null also preserves
it. Read failures refuse signup rather than assuming the default is open.

The portal policy and landing-page registration link use the same runtime
setting. Already-open portal login/register pages refresh policy every 30
seconds; server requests enforce the setting immediately. Email suffix and
invitation restrictions also apply to external signup; IP limits and CAPTCHA
apply to password signup. Only complete administrators can edit this policy.

## Mail

Settings → Notifications and integrations → Mail: SMTP (any provider; port 587 STARTTLS, 465 TLS or 25
plain) or the Resend HTTP API for hosts that block mail ports, plus a
"send test email" button. With mail configured, registration can require an
emailed code, users can reset their own password, and the hourly job sends
the expiry and traffic-threshold reminders.

**Mail language** — the same picker row chooses the language of the mail
users receive: English (the default), 简体中文, 繁體中文, 日本語, Русский or
한국어. It is a separate choice from the console's language, which each
staff member picks in their own browser, because this mail goes to
customers. The messages are deliberately short — a code, a date, a
percentage and at most one button — and live in `internal/mail/lang.go`
if you want to reword them or add a language.

## External login (OIDC)

Settings → Staff and site security → External login takes any OpenID Connect provider (Casdoor,
Authentik, Auth0, Keycloak, Zitadel, Google …): id, display name, issuer URL,
client id and secret. Register
`https://<your domain>/api/oauth/<id>/callback` as the redirect URI at the
provider.

Use the exact `issuer` value from the provider’s OpenID configuration document,
including any trailing slash. For example, an Auth0 issuer may be
`https://auth.example.com/`. Captain preserves this value and requires exact
discovery and ID-token issuer matches; do not remove or add a slash to normalize
it. The issuer is not the `/.well-known/openid-configuration` URL itself.

The portal then shows "Continue with …". Accounts are matched by the
provider's subject, linked to an existing account with the same verified
email, or created when registration allows it. Before the runtime master switch
is set, a provider's `auto_register` can permit signup despite a closed deployment
default; an explicitly closed master switch takes precedence. Users link and unlink logins from the portal's Account page, and
password login can be switched off entirely.

Casdoor example: issuer `https://door.example.com`, default scopes
(`openid profile email`), `trust_email: true` since you run it yourself.

## Captain as a login provider

Settings → Integrations → Captain login applications registers trusted applications
such as New API. This is separate from External login and grants customer identity
only. See [New API setup and security boundaries](CAPTAIN_LOGIN.md).

## Backups

Captain snapshots its database once a day with `VACUUM INTO`, so the copy is
consistent while the panel keeps serving, into `<data_dir>/backups`, keeping
the newest seven. Settings → Backups and maintenance → Database backups sets the hour and retention,
adds a remote (WebDAV with basic auth, or any S3-compatible bucket: AWS,
Cloudflare R2, Backblaze B2, MinIO with path-style) that receives each
snapshot gzipped with its own retention, tests the remote, runs a backup on
demand and downloads local copies. Remote copies can be encrypted with age:
generate a key pair in the card (the private key is shown once and never
stored) or set a passphrase. An encrypted copy carries `config.yaml` beside
the database, so one file rebuilds a panel; `captain backup open` writes
both back out.

Restore, upgrade and rollback procedures — including the `-wal`/`-shm`
caveat that silently corrupts a careless restore — are in
[OPERATIONS.md](OPERATIONS.md).

## Reset site

Settings → Backups and maintenance → Reset site starts Captain over with an empty business database.
Only an active **admin** using a console cookie session can preview or execute
it; operator/support accounts, customer sessions and API tokens cannot. The
confirmation dialog shows affected record counts, requires the current password,
a fresh authenticator code when TOTP is enabled, and the exact text `RESET`.
Failed credential attempts share the sign-in rate limit.

- Deletes customers, other staff, plans, orders, subscriptions, balances, node
  records, entries, panel-managed certificates, history and all database settings.
  Old sessions, API tokens, subscription URLs and node pairings stop working.
- Recreates the calling admin as ID 1 with the same email, password and enabled
  TOTP. The first new customer is ID 1 independently. Everyone must sign in again;
  the reset itself becomes the first new admin audit entry.
- Preserves the database schema/migration version and all deployment files:
  `config.yaml`, the panel's HTTPS certificates, ACME files, custom static site
  files and existing local/remote backups. External DNS records are untouched.
  Configuration supplied in `config.yaml`, including payment gateways, remains
  in effect. This is not an uninstall or secure erasure of historical backups.
- **Remote bosun processes and cached configurations are not cleared.** They can
  continue serving existing users after losing the panel connection. Stop or
  clean up those nodes first; then create new node records and pair again.
  When node records exist, both the dialog and API require acknowledgement of
  this condition.

Take and download any backup you need **before** confirming. The reset does not
make an automatic backup. Database cleanup, ID sequences and the retained admin
are committed in one transaction; a database failure rolls the whole operation
back. In-flight requests and jobs complete before resetting, and cached account
and node state is discarded before new requests proceed. Long polls recheck their
node token, including after a node ID is reused.

`GET /api/admin/system/reset` returns counts for `users`, `staff`, `nodes`,
`plans`, `orders` and `subscriptions`. `POST` to the same path accepts
`{"password":"…","code":"…","confirmation":"RESET","nodes_acknowledged":true}`
and returns `{"ok":true,"admin_id":1}`. `code` is required only with TOTP;
`nodes_acknowledged` must be true when nodes exist. Success expires the cookie
and the console returns to sign-in. No process restart is required.

## Interface languages

The console, portal, server probe and public website ship in 简体中文,
繁體中文, English, 日本語, Русский and 한국어. The public homepage and
service-status header initially follows the browser's ordered language
preferences; an explicit choice is saved only in that browser, separately
from other interfaces. Custom operator content is not automatically translated
or rewritten. Every locale file is checked for parity in CI, and a key the
sources use but no locale defines fails the build.

## Additions in 1.8.0

Staff Passkeys, scoped API tokens, user filtering/bulk operations and private metadata are described in [administration workflows](ADMIN_WORKFLOWS.md).
