# Use Captain accounts to sign in to New API

Captain can act as a **confidential-client OAuth login provider** for registered
applications. This is separate from the External login setting, where Captain
is a client of Auth0 or another identity provider. Ordinary Captain customers
can approve an application; staff sessions and staff credentials cannot.

This first implementation supplies authorization code, token and user-info
endpoints. It does not implement OIDC discovery, ID tokens, refresh tokens,
public/native clients, balance/plan synchronization or single logout.
New API's **Custom OAuth** provider supports the required form-token exchange
and Bearer user-info calls. Use that feature, not the built-in OIDC provider.
The instructions below target the upstream GenericOAuthProvider protocol;
verify availability in the New API version you deploy.

## Captain configuration

1. Set Captain's `base_url` to its canonical HTTPS URL. Reverse proxies must
   preserve this public origin. Only loopback HTTP URLs are accepted for local
   development. Both browser and New API backend must reach Captain.
2. Open Settings → Integrations → **Captain login applications**.
3. Add an enabled application named New API. Register the exact callback URL:
   `https://newapi.example.com/oauth/captain`, where `captain` is the provider
   slug you will configure in New API. No wildcards are allowed.
4. Copy the Client ID and the **one-time displayed** Client Secret. Store the
   secret only in New API's backend configuration. Captain stores its hash.

## New API configuration

As Root, add a Custom OAuth provider with these values (replace the example
origins with your actual public origins):

| Field | Value |
| --- | --- |
| Name | Captain |
| Slug | `captain` |
| Client ID / Client Secret | Values created in Captain |
| Authorization endpoint | `https://captain.example.com/api/login-provider/authorize` |
| Token endpoint | `https://captain.example.com/api/login-provider/token` |
| User info endpoint | `https://captain.example.com/api/login-provider/userinfo` |
| Scopes | **`profile email`** (replace the default `openid profile email`) |
| User ID field | `sub` |
| Username field | `preferred_username` |
| Display name field | `name` |
| Email field | `email` |
| Authentication style | POST parameters (`client_secret_post`, value 1); Basic (`client_secret_basic`, value 2) also works |
| Discovery URL | Leave empty; configure the endpoints manually |

Enable the provider and ensure New API's configured site address matches the
callback registered in Captain. New API's local registration policy determines
whether a new local account can be created. For an existing New API account,
sign in normally first and bind Captain through account settings. Do not merge
accounts merely because their email addresses match. Captain returns
`email_verified: false`: it does not invent an email-verification guarantee for
historical accounts. A `profile`-only application receives no email field.

The browser sends users to Captain. Existing customer sessions can proceed to
the approval page; otherwise users first sign in (password or an already
configured external identity provider). Approval shows the application, the
current account, requested information and the exact callback. Rejecting returns
`access_denied`. Existing New API balances, API keys and permissions stay in
New API; no Captain administrator token is needed.

## Security and lifetime

- `sub` is a dedicated random, stable identifier, unrelated to proxy UUIDs,
  subscription secrets, editable user IDs or email addresses. Renumbering a
  customer preserves it. Deleting/recreating a customer creates a new identity.
- Authorization requests require `response_type=code`, an exact registered
  callback, a nonempty browser-bound `state`, and the `profile` scope. Optional
  `email` and S256 PKCE are supported. New API's current generic client uses its
  client secret rather than PKCE; the token endpoint always requires client
  authentication. The client must verify its own `state` value on return.
- Approval tickets expire after 10 minutes, authorization codes after 60
  seconds and opaque user-info access tokens after 5 minutes. Codes are bound
  to the client, callback, optional PKCE challenge and Captain session. Exchange
  is atomic and single-use, including concurrent requests. Only hashes of codes, approval tickets and access tokens are
  stored. Tokens cannot access Captain's portal, admin or subscription APIs.
- Changing an application, rotating its secret, or deleting it invalidates its
  pending requests, codes and tokens. A disabled application cannot start or
  finish login. Deleted/expired Captain sessions and banned users cannot use
  these grants. User deletion cascades through the session and subject data.
- **An established New API session is independent.** Logging out or banning a
  user in Captain does not immediately sign them out of New API. New API must
  separately enforce its own account/session policy. There is no refresh-token
  or ongoing account synchronization in this version.
- Auth0 users depend on Captain's existing external-login configuration. This
  change includes the issuer trailing-slash fix, preserving exact issuer values.

Migration 81 adds the client registry, independent subjects and expiring login
requests. It is additive; existing external login and user sessions are kept.
Full resets clear these records, and backups contain application secret hashes
and login identities. Treat restored databases as authentication-sensitive.

## Verification

HTTP integration tests cover the New API request shape, both client-secret
styles, user-info mapping, optional PKCE, consent denial, CSRF/session binding,
client and staff isolation, callback/scope rejection, code replay and expiry,
concurrent redemption, account renumbering, banned users, secret rotation and
logout revocation. A live New API deployment still requires configuring the
provider and exercising its local registration/account-binding policy.

References: [New API custom OAuth documentation](https://github.com/QuantumNous/new-api-docs-v1/blob/main/content/docs/zh/guide/feature-guide/admin/custom-oauth.mdx),
[upstream generic client](https://github.com/QuantumNous/new-api/blob/main/oauth/generic.go).
