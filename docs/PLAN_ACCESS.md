# Plan-first node access

Plans are the primary administration entry point. The node and inbound editors
show authorized plans and link to plan management; they do not select groups or
plans. New console-created inbounds, including reverse WireGuard and VLESS
Reverse user listeners, start unassigned. Several subscription entries sharing
one inbound share authentication and are granted together.

A new plan defaults to independent permissions. An explicit shared scheme lets
monthly, annual, promotional or differently priced plans reuse one node list;
editing the scheme affects every plan using it and their current subscribers.
Commercial membership levels, recharge discounts and referral tiers are deferred.

## Selecting nodes and categories

Individual node selections are static. A category's **Select current nodes**
action adds its current inbound IDs to that selection; it does not grant future
nodes. **Follow changes to this category** is a separate, default-off option.
It grants all inbounds referenced by entries in that category, including future
additions and moves. Leaving/deleting the category revokes only category-derived
grants; individual selections and other grants remain. Disabled nodes remain
selectable, but granting them does not enable them. Internal reverse receivers
never become user-accessible through a category.

Following categories belongs to the permission scheme. Moving a categorized entry
between inbounds, adding/removing followed membership, or deleting a followed
category requires administrator node-access write permission as well as the
original operation's permission. Metadata-only edits remain allowed by their
existing scopes. Plan details, individual selections and category rules commit
atomically after a full-catalogue revision check and explicit confirmation.
Revision fences also change after A-to-B-to-A edits; stale drafts cannot revive
old intent. Subscription publication, agent user credentials and billing all use
the same effective grants. No new agent fields or installed-agent upgrade is needed.

Account-specific extra grants are preserved and can extend beyond a plan. When
detaching a legacy shared plan, external-node source authorization is retained
separately and continues following that external source. Managed-node selections
do not turn external subscriptions into independently billed inbounds.

## Upgrade conversion (migration 119)

The transaction captures every old effective `all` inbound and every existing
plan, including disabled items. Plans with no group receive a private backing
group; existing groups stay unchanged. Captured inbounds become explicit grants
to the captured plans' groups, or `none` if there are no plans. Existing `groups`
and `none` inbounds, account extra grants, subscriptions and enabled flags are
preserved. Internal reverse receivers are excluded. The immutable conversion
receipt stores affected IDs, timestamp and counts; the plan page displays its
summary. This runs once with the schema migration, not on every startup. Newly
created independent plans do not inherit these captured grants; selecting a
shared scheme deliberately reuses its permissions.

Migration 118 introduced plan-owned backing groups; migration 119 adds the
conversion receipt, category-follow rules and a catalogue revision fence. Captain v1.63.0 contains migration 119 and sets rollback floor v1.63.0.
An older panel would ignore the category rules when editing memberships.

## Compatible API

Existing plan POST/PATCH paths accept optional `Access`:

```json
{
  "Name": "Monthly",
  "PriceCents": 1000,
  "Access": {
    "mode": "independent",
    "group_id": 0,
    "inbound_ids": [1, 2],
    "category_ids": [3],
    "revision": "current-node-access-catalogue-revision",
    "confirm_shared": true
  }
}
```

Use `mode: "shared"` and an existing `group_id`, or `group_id: 0` with a new
`name`, for reusable schemes. `inbound_ids` is required. `category_ids: []` clears
following; omission preserves existing rules (or copies them on independent
detachment). The catalogue adds `categories`, group `category_ids`, entry
`category_id`, inbound `static_group_ids` and `conversion` summary. Effective
`group_ids` includes both sources, so older readers still see effective grants.
Owned backing groups cannot be reused by another plan via a forged ID. Requests
carrying Access require plan-write plus node-access read/write authorization.

Legacy group/access APIs remain available. Old script-created inbounds keep
legacy GroupID behavior for the 1.x contract; they may still explicitly expose
an all-users inbound. New admin UI requests send `X-Captain-Access-Model: plan`,
which opts all creation paths into unassigned defaults; ordinary inbound create
also accepts additive `AccessPolicy: "none"`. Neither changes existing inbounds.
Legacy access editors return a conflict for category-managed schemes/listeners
rather than silently pinning or overriding derived grants. Existing metadata
writes without Access preserve permissions, and omitted new
fields never clear category-follow rules. Historical all-users inbounds created
through legacy APIs still cannot be excluded from just one plan.


## Names (Captain v1.64.0, migration 120)

New independent permission groups use the plan name directly, without a random
suffix. Renaming a plan renames its currently owned group in the same transaction;
IDs, grants, subscribers and category-follow rules are unchanged. Shared schemes
keep their administrator-supplied names and are not renamed with an individual plan.
A different existing scheme is never reused or merged just because its name matches:
new independent ownership or a rename returns a readable409 conflict instead.

New and renamed plan names are trimmed, required, limited to200 characters and
checked case-insensitively against every plan, including disabled ones. The UI
checks its current list and the store checks and writes in a single transaction;
SQL triggers also reject ASCII-case/space duplicate writes. Application checks
support Unicode case folding. History containing duplicate plan names stays intact;
unchanged-name metadata edits remain allowed, and the UI warns to rename affected
plans individually. This does not merge plans or change existing billing records.

Migration120 renames only currently owned groups whose plan names and destination
names are unambiguous and available. It preserves all IDs, grants and conversion
receipts. A conflicting historic shared name is retained, with a warning in the
plan editor; normal price/enable edits still work. Resolve that occupied name
explicitly before renaming the plan. Old inactive ownership and legacy shared
schemes are not silently deleted. No agent change is required.


## Dismissing the conversion notice

The migration-completed notice has an administrator-only close button. Closing
persists a site-wide dismissal for the exact receipt in panel settings; it stays
hidden across refreshes, browsers and administrator accounts. The conversion
summary and affected-ID history remain intact. Failed saves leave the notice
available to retry. Dismissal does not alter authorization catalogue revisions,
plan grants, subscriptions or billing, and needs no schema/agent change.

POST `/api/admin/node-access/conversion/dismiss` accepts the currently displayed
`at`, `inbounds` and `plans` tuple. An administrator with node-access write
permission may dismiss it; stale receipts return409 and a missing receipt404.
The additive `conversion.dismissed` field in the access catalogue reports the
saved visibility state. Resetting the site clears both history and dismissal.
