# Selling access: plans, payments, coupons, invites

Everything between a visitor and a working subscription.

## Plans

A plan has a base period and price plus any number of extra periods
(quarter, year …) with their own prices; the buyer picks one at checkout.
Beyond that: traffic quota, device limit, speed limit, and which node
groups it unlocks.

- **Renewing the same plan** before it expires extends the time and keeps
  what was used: a plan with a reset cycle keeps its counter and its next
  reset date, a plan without one has the new period's allowance added.
- **Buying a different plan** stacks it next to the current one, or
  replaces it when *single-plan mode* is on (Settings → Subscription).
  Queueing a plan the user already holds is refused — renew it instead.
- **Quota reset**: never, every N days from purchase, on the first of each
  month, or on January 1st. A user can be given a personal reset day.
- **Several plans per user** union their node groups, take the widest
  limits, and are charged per inbound: traffic through an inbound that
  belongs to a plan's group is booked to that plan, and traffic through an
  ungrouped inbound to the soonest-expiring usable plan.

In a user's drawer: extend by 30/60/90 days, override the quota (kept
across renewals of the same plan), set a personal monthly reset day, reset
usage, grant a plan directly. Users → Renewals lists everyone by expiry
with one-click extensions for batch renewals.

**Trial plan** — Settings → Trial plan hands every new account (password or
external login) one plan once, for the configured number of days.

**Plan change credit** — Settings → Plan change credit: switching to a
different plan credits the unused remainder of the current one (by
remaining time, or by remaining traffic for plans without an expiry)
against the new order. Only one pending order can hold that credit at a
time.

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
portal home page.

## Invites and commissions

Every user has a referral link `/?ref=CODE`. A visitor who arrives through
it is recorded as invited when the account is created (by password or by
external login), and each paid order credits a configurable share to the
inviter.

Settings → Invites holds the level-1 percentage, optional multi-level
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

`/portal/` (the site root redirects there): sign up and sign in, usage,
expiry and balance, the subscription link with copy button, QR code and
one-tap import for Clash, sing-box, Shadowrocket and Surge, the plan list
paid by balance or any enabled gateway, order history, the server list with
per-server share links, devices, tickets, knowledge base and redeem codes.

`/` is a public landing page — hero with a rotating globe of your node
locations, feature cards, plans and an FAQ — edited under Admin → Landing
page with no rebuild. To use your own design instead, drop an `index.html`
and its assets into `<data_dir>/site/` and Captain serves that directory.
