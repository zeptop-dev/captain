# Registration and email identity

This feature is developed for the next minor release (reserved rollback floor
v1.70.0). It does not change bosun or proxy credentials.

Existing registration, invitation, domain restrictions and trial settings remain
in place. Domain-based plan grants are intentionally not part of this feature.

## Email verification

Migration 125 adds persistent evidence for the exact current customer email:
verification time and source (`email_code` or `oidc:<provider>`). Historical
accounts are not assumed verified; they can continue signing in. A customer can
verify their current address in Portal → Account when mail delivery is enabled.
The account feature verifies the current mailbox; it does not change it.

Authenticated POST `/api/portal/me/email/verification/send` sends a challenge to
the session account's email, never an arbitrary address supplied in the body.
POST `/api/portal/me/email/verification` accepts `{ "Code": "123456" }`.
Both require a normal active customer session and same-origin access. The
challenge purpose binds to immutable agent identity and the exact email, is
single-use, expires in ten minutes and is burned after five wrong attempts.
Code creation/consumption is serialized through SQLite transactions.

Password registration commits account creation, successful code consumption,
verification proof and legal receipt together. A failed account insert rolls
back consumption. Failed code attempts still persist. A verified OIDC claim (or
an explicitly trusted provider email) records provider provenance; an unrelated
provider email cannot verify the current customer address. Staff remain separate.

Any actual update of `users.email`, including admin and OIDC paths, invalidates
its former proof through a database trigger. Numeric account changes carry
verification/consent references with the account. Deletion and site reset remove
them. The additive `/api/portal/me` `email_verification` object exposes current
status/time/source, and `email_verification_available` describes mail readiness.
SSO userinfo's existing `email_verified` claim now reflects proof for that exact
current address instead of a constant false.

## Registration terms

Settings → Registration provides optional terms/privacy document links and a
human-managed document version. Links may be HTTPS or site-relative paths.
When either document is configured, a non-empty version is required. Change the
version whenever the document behind a stable URL changes. Captain records the
links and version; it does not fetch, archive or certify external document text.

The existing registration settings API adds an optional `legal` object:
`terms_url`, `privacy_url`, `document_version`. Old writers omitting it preserve
existing terms. Sending an explicit object with both links empty disables the
requirement. No documents are enabled automatically on upgrade.

The public policy API adds `legal` and an opaque `legal_version` fingerprint.
Password registration submits `LegalAccepted: true` and the displayed
`LegalVersion`. The server rechecks the current fingerprint in the creation
transaction; stale or absent consent cannot create an account. Receipts store
the accepted version, links, method and time, without password, code or token.

For third-party first-time registration, same-origin POST
`/api/oauth/{id}/start` accepts `LegalAccepted` and `LegalVersion` and returns
an authorization URL. GET remains the compatible login flow; query parameters
cannot manufacture agreement. POST saves short-lived server-side
consent bound to the random OAuth state, provider and version. The callback
cannot manufacture consent by editing its browser flow cookie. Creation consumes
the matching consent with the receipt. Expired consent is purged on new consent
creation, with at most 10,000 pending rows. Existing-account login/linking does
not require accepting registration terms again.

## Delivery and frontend feedback

When registration CAPTCHA is configured, sending a registration code requires
its own `Captcha` token. The browser renews the widget after sending and after
each registration submission; a single-use challenge is never intentionally
reused. Authenticated account verification uses the same protection.

Code delivery returns additive `retry_after` seconds (normally 60). Cooldown
rejection includes `Retry-After`. Countdown and delivery confirmation belong to
the actual destination address, not a subsequently edited email field.
Registration errors add stable `code` values while retaining `error`: closed
registration, denied domain, missing invitation, IP cap, failed CAPTCHA, outdated
terms, invalid verification code and an occupied email are distinct. A generic
403 is no longer mislabeled as a closed registration.

Account verification mail has a separate `email_verify` template and preview,
with the normal branding and all six languages. It does not describe an existing
customer as registering again.

Older panels ignore the consent setting, which would bypass an enabled signup
requirement; the rollback floor is reserved as v1.70.0. Release integration must
align this floor with the first containing formal release if its version changes.
The migration is forward-only; no agent wire fields or config keys change.
