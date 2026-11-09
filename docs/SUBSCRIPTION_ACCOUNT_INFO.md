# Subscription account information

In **Settings → Subscriptions & clients**, the account-information card enables
traffic usage/quota, the next traffic reset, and expiry independently. All three
default to off. A subscription profile can inherit the global selection or
override it, including disabling it. Profile preview uses the same output path
as the customer's subscription.

For Clash/Mihomo and Stash, Captain appends a `Subscription info` selector and
one selector per displayed value. Each value selector contains only `REJECT`.
These are presentation groups, not dummy proxy endpoints. Existing proxies,
provider membership, automatic tests, regional groups, rules and their default
selections remain unchanged. Names are made unique against the rendered document.
Client interfaces may show the value selectors separately as well as in their
parent information group. These groups are not suitable routing targets.

Other formats, HTML subscription pages and device-specific WireGuard downloads
remain unchanged. The existing `Subscription-Userinfo` header is retained, with
its original aggregate traffic/quota/expiry semantics. It has no standard reset
field. Existing custom `info_lines` are independent and keep their old behavior;
those legacy dummy endpoints do not gain the new groups' isolation guarantees.

Values belong to the customer's **active, unexpired subscription rows**, not the
underlying servers. Exhausted active plans remain visible, queued/expired and
cancelled plans do not. Each plan is displayed separately instead of inventing
one combined reset or expiry. The stored `reset_at` controls the displayed date;
a reset at/after expiry is described as no reset before expiry. A due reset still
awaiting processing says `Pending`, and missing dates say no scheduled reset or
never expires. This is presentation only: it does not reset usage, extend a
subscription or change access. Displayed usage uses explicit binary units (GiB).

Labels are English and times are UTC in this first version. Countdown days are
whole remaining 24-hour periods; the exact UTC date/time is also shown. All
values are snapshots at subscription download time. They update when the client
refreshes its subscription, not continuously.

The existing administrator-only settings endpoint adds:

```json
{
  "account_info": { "traffic": true, "reset": true, "expiry": true }
}
```

Partial saves preserve other fields, and omitted nested booleans preserve their
stored values. A profile's `settings.account_info` is an optional object: absent
inherits global settings; an object overrides all three switches, with missing
booleans false. No migration, agent change or bosun update is needed.
