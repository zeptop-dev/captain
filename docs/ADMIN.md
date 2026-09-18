# Running the console: staff, access, integrations

Who can do what, how the panel is reached, and everything it can talk to.

## Staff roles

Admin → Staff creates console accounts with a role:

| Role | Can |
|---|---|
| **admin** | everything |
| **operator** | everything except settings, system, staff, the landing page, and the audit rules and log — those write every node's core config and record where users went |
| **support** | tickets, plus read-only users, orders, plans and the dashboard. No connection log, no subscription links, and the user payloads leave the subscription token out |

The last admin cannot be demoted, disabled or deleted.

**Two-factor sign-in** — Settings → Two-factor authentication: each staff
account can add a TOTP authenticator, and the console then asks for the
code after the password. Re-enrolling requires the current code. The replay
guard is keyed on the step that matched, so the next code still works. API
tokens are unaffected.

## API tokens and MCP

Settings → API tokens & MCP issues personal bearer tokens (`cap_…`) for the
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

- **Admin allow-list** — Settings → Security → *Admin allow-list*
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
- **Session kinds** — a portal, OIDC or password-reset session never
  reaches `/api/admin`, even for a staff account; the console needs the
  admin login. Changing a password drops that user's sessions.
- **Which address is "yours"** — behind a proxy, `X-Forwarded-For` read
  from the right, skipping trusted proxies. The full rules, and how to
  configure `trusted_proxies`, are in
  [OPERATIONS.md](OPERATIONS.md#admin-access-allow-list-and-client-addresses).

## Theme and page injection

Admin → Landing page: primary colour, corner radius, light/dark/system
scheme for the portal and the landing page, portal title, font, and raw HTML
injected before `</head>` and `</body>` on every portal and landing page
(analytics, chat widgets). The console keeps its own look.

## Event webhooks

Settings → Event webhooks: Captain POSTs JSON to your URLs with
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
  `{{site_name}}` substituted per reader; Settings → Client downloads lists
  the apps. Both appear under Help in the portal.
- **Telegram bot** — Settings → Telegram: paste a BotFather token, and your
  chat id for order, ticket and node notices. Users link their chat from
  the portal with a one-time `/bind CODE`; the bot answers `/sub`,
  `/status`, `/unbind`, `/id` and delivers expiry, traffic and ticket
  notices. Plain Bot API long polling — no webhook, no public URL. One bot
  token cannot be shared with a bosun panel (Telegram allows one
  `getUpdates` consumer).

## Mail

Settings → Mail: SMTP (any provider; port 587 STARTTLS, 465 TLS or 25
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

Settings → External login takes any OpenID Connect provider (Casdoor,
Authentik, Keycloak, Zitadel, Google …): id, display name, issuer URL,
client id and secret. Register
`https://<your domain>/api/oauth/<id>/callback` as the redirect URI at the
provider.

The portal then shows "Continue with …". Accounts are matched by the
provider's subject, linked to an existing account with the same verified
email, or created when registration is open (or `auto_register` is set for
that provider). Users link and unlink logins from the portal home page, and
password login can be switched off entirely.

Casdoor example: issuer `https://door.example.com`, default scopes
(`openid profile email`), `trust_email: true` since you run it yourself.

## Backups

Captain snapshots its database once a day with `VACUUM INTO`, so the copy is
consistent while the panel keeps serving, into `<data_dir>/backups`, keeping
the newest seven. Settings → Database backups sets the hour and retention,
adds a remote (WebDAV with basic auth, or any S3-compatible bucket: AWS,
Cloudflare R2, Backblaze B2, MinIO with path-style) that receives each
snapshot gzipped with its own retention, tests the remote, runs a backup on
demand and downloads local copies.

Restore, upgrade and rollback procedures — including the `-wal`/`-shm`
caveat that silently corrupts a careless restore — are in
[OPERATIONS.md](OPERATIONS.md).

## Interface languages

The console and the portal ship in 简体中文, 繁體中文, English, 日本語,
Русский and 한국어. Every locale file is checked for parity in CI, and a key
the sources use but no locale defines fails the build.
