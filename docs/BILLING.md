# Selling access: plans, payments, coupons, invites

Everything between a visitor and a working subscription.

## Plans

A plan has a base period and price plus any number of extra periods
(quarter, year …) with their own prices; the buyer picks one at checkout.
Beyond that: traffic quota, device limit, speed limit, and which node
user group it unlocks.

- **Renewing the same plan** before it expires extends the time and keeps
  what was used: a plan with a reset cycle keeps its counter and its next
  reset date, a plan without one has the new period's allowance added.
- **Buying a different plan** stacks it next to the current one, or
  replaces it when *single-plan mode* is on (Settings → Plans and purchases).
  Queueing a plan the user already holds is refused — renew it instead.
- **Quota reset**: never, every N days from purchase, on the first of each
  month, or on January 1st. A user can be given a personal reset day.
- **Several plans per user** union their usable plan groups and take the widest
  limits. An inbound may allow several groups; traffic is charged once to the
  soonest-expiring eligible usable plan, with the existing deterministic tie
  order. An `all` inbound can use any usable plan. Exhausted eligible plans follow
  the existing spillover/tail-accounting rules: if no matching usable plan remains,
  accounting falls back to another usable plan, then the latest active subscription.
  This accounting fallback does not itself grant access to a node.

### Choosing which nodes a plan includes

In **Nodes & lines**, open a node's access control and choose **All users**,
**Selected user groups**, or **No users**. For example, ordinary nodes can include
both Standard and Premium, while premium nodes include Premium only. These names
do not imply a hierarchy. Display categories are labels, not permission grants.

In **Plans**, choose the plan's user group, inspect its available nodes, and open
**Manage node access** to change that same group's membership. Plans sharing the
group share its nodes. The permission change is saved separately from the plan
form and immediately affects the group, including existing subscribers. Inbounds
available to all users remain selected in this editor; restrict them from the node
editor first. Connections without a published Entry can also appear in this
inbound-backed catalogue so their authentication is not silently ignored.

Entries sharing one real inbound must share its access policy. Review the listed
shared nodes and plans before confirming. To sell two different permission sets
for the same server, use separate inbounds. A forwarding line only describes a
path; access follows the actual inbound used by its published subscription node.

A usable purchased subscription grants its plan group automatically; queued,
expired or exhausted subscriptions do not add that grant. Existing account
`group_id` behavior remains compatible: this field may have been set manually or
by a legacy single-plan purchase, and expiry does not clear it. Stacking a plan
still follows the existing rules for clearing duplicate account group grants;
the database does not record their original source. This feature does not rewrite
these lifecycle rules or promise an automatic permanent-group downgrade. At least
one usable subscription, account validity, quota, client capability and chain
dependencies still determine actual usability.

In a user's drawer: extend by 30/60/90 days, override the quota (kept
across renewals of the same plan), set a personal monthly reset day, reset
usage, grant a plan directly. Users → Renewals lists everyone by expiry
with one-click extensions for batch renewals.

**Trial plan** — Settings → Accounts and security → Trial plan hands every new account (password or
external login) one plan once, for the configured number of days.

**Plan change credit** — Settings → Plans and purchases → Plan change credit: switching to a
different plan credits the unused remainder of the current one (by
remaining time, or by remaining traffic for plans without an expiry)
against the new order. Only one pending order can hold that credit at a
time.

## Proxy usage and server attribution

Managed reverse WireGuard and VLESS Reverse authenticate and meter users on
front server A. Captain v1.35 displays those accepted bytes against exit B in
the server list/detail view, while preserving the same single customer charge
and the original inbound's plan selection. Interface totals, transfer overhead
and plain forwarding are separate from user proxy usage. Old attribution history
may be incomplete; it is not used to charge customers again.

Independent native WireGuard devices have separate peer credentials but share
their parent's account, quota and limits. Creating another device does not grant
another allowance or a different plan. See
[WireGuard device subscriptions](SUBSCRIPTIONS.md#independent-wireguard-devices).

## Payment gateways

Enable any subset under `payments:` in config.yaml (see
[config.example.yaml](../config.example.yaml)); each one appears in the
portal's "pay with" chooser. The callback URL to register at the provider
is `<base_url>/api/payment/<name>/notify`.

| name | provider | how the callback is trusted |
|---|---|---|
| `epay` | EPay (易支付) v1 and v2 behind one gateway (`payments.epay.version`) | MD5 or RSA signature, and the callback amount must match the order |
| `stripe` | Stripe Checkout | webhook signed with the endpoint secret |
| `alipay` | Alipay Face-to-Face (支付宝当面付) | `alipay.trade.precreate`; Captain serves the QR page at `/api/payment/alipay/page`, RSA2 both ways |
| `coinbase` | Coinbase Commerce | hosted charge; webhook HMAC-SHA256 (`X-CC-Webhook-Signature`) |
| `coinpayments` | CoinPayments | `create_transaction`; IPN HMAC-SHA512 with the IPN secret, merchant id checked |
| `btcpay` | BTCPay Server | Greenfield invoice; webhook HMAC-SHA256 (`BTCPay-Sig`), store id checked |
| `mgate` | MGate | Xboard-compatible: md5(sorted query + app_secret) both ways |
| balance | — | the user's own balance, always available |

Every callback is signature-verified before a single field is read.
Settlement is idempotent under repeated callbacks, a late callback revives
a cancelled order, and a callback whose amount differs from the order is
refused. Crypto gateways take a `currency` for the fiat price (default CNY,
USD for CoinPayments) and the buyer picks the coin on the provider's page.

## Coupons and gift codes

**Coupons** (Admin → Coupons) take a percentage or a fixed amount off,
optionally limited to certain plans, a number of total uses, uses per user
and a date range. The checkout shows the discounted price as the code is
typed, and a code reserved by a pending order is released when that order
is cancelled.

**Gift / redeem codes** (Admin → Gift codes) are generated in batches,
single-use, with an optional expiry: balance top-up, a plan for N days,
extra traffic, or extra days on the active plan. Users redeem them on the
portal's Account page (`/portal/account`).

## Invites and commissions

Every user has a referral link `/?ref=CODE`. A visitor who arrives through
it is recorded as invited when the account is created (by password or by
external login), and each paid order credits a configurable share to the
inviter.

Settings → Plans and purchases → Invites holds the level-1 percentage, optional multi-level
(levels 2 and 3), and where a reward goes:

- straight to the inviter's **balance**, or
- to a **commission account** the user can move to their balance or
  withdraw (minimum amount, allowed methods).

Admin → Withdrawals marks a request paid or rejected; rejecting refunds the
commission. Cancelling a queued plan reverses its commissions and releases
its coupon use, so a self-invite loop cannot mint credit.

## Registration limits

Settings → Registration limits (all optional, applied to password sign-up;
the email whitelist and the invite-only rule also govern accounts created
automatically by external login):

- **Email domain whitelist** — only listed domains (and their subdomains)
  may register.
- **Sign-ups per address** — at most N accounts per client address within
  the window (24 h by default).
- **Invite only** — a valid invite code or `/?ref=` link is required.
- **Captcha** — Cloudflare Turnstile, Google reCAPTCHA v2 or hCaptcha:
  enter the site key and secret and the widget appears on the sign-up form,
  with the token verified server-side.

With mail configured, registration can also require an emailed code, and
users can reset their own password. See [ADMIN.md](ADMIN.md#mail).

## The portal

`/portal/` shows each plan's remaining traffic, reset and expiry separately,
with quick connection actions and announcements. The main sections are Overview,
Connect, Plans and billing, Help, and Account; narrow screens use bottom navigation.
Connect (`/portal/connect`) groups configured client downloads by device platform,
subscription copy/QR/import actions, searchable nodes and WireGuard devices.
Account (`/portal/account`) holds registered HWID devices, redeem codes, Telegram
preferences and linked logins. Existing plan, order, server, help, ticket and invite
URLs remain available.

Plans can be paid by balance or an enabled gateway. Returning from a payment page
does not establish success: order badges use the server's status, and pending
orders refresh while the customer waits.

`/` is a public landing page — hero with a rotating globe of your node
locations, feature cards, plans and an FAQ — edited under Admin → Landing
page with no rebuild. To use your own design instead, drop an `index.html`
and its assets into `<data_dir>/site/` and Captain serves that directory.
