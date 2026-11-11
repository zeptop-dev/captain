# Plan-first node access

Plans are the primary administration entry point. New administrator-created plans
in the console default to independent managed-inbound permissions. A shared access
scheme is an explicit option; select an existing scheme or name a new one. Existing
plans remain shared with their original groups, or retain the legacy ungrouped
policy, until an administrator explicitly changes them. Membership levels and
commercial discounts are a separate, deferred feature.

The editor stages plan details and access together. It lists other plans using a
shared scheme. Switching to independent creates a plan-owned backing group in the
same SQLite transaction as the plan and inbound membership changes; it never edits
the old shared group's membership. Changes apply to current subscribers too. A
confirmation is required for submitted access changes; a normal metadata-only edit
omits access and does not reapply stale membership. All-users inbounds remain
included and cannot be excluded for just one plan. Shared entries on the same
inbound still share authentication. No group hierarchy is introduced.

Account-specific extra grants are deliberately preserved. They can extend access
beyond a plan; the existing field is under an advanced section in customer editing.
When detaching a legacy plan, external-node source-group authorization is retained
separately and continues following that external source. Independent managed-node
permissions do not turn external subscriptions into separately billed inbounds.
The old user-group navigation is hidden; old routes normalize to the user list and
legacy group APIs remain available for compatibility. Hiding UI is not authorization.

## API and storage

Existing plan POST/PATCH routes accept optional `Access`:

```json
{
  "Name": "Monthly",
  "PriceCents": 1000,
  "Access": {
    "mode": "independent",
    "group_id": 0,
    "inbound_ids": [1, 2],
    "revision": "current-node-access-catalogue-revision",
    "confirm_shared": true
  }
}
```

Use `mode: "shared"` and an existing `group_id`, or `group_id: 0` with a new
`name`, to use a reusable scheme. The explicit inbound array must include all
editable all-users inbounds; missing/null arrays are rejected. The catalogue
revision fences the entire operation and plan relation. Unknown/duplicate targets,
invalid schemes and missing confirmation roll back the whole transaction.
Owned backing groups cannot be reused by another plan, including via legacy plan
writes with a forged ID. Requests carrying Access require an administrator and
both plan-write and node-access read/write authorization. Existing requests without
Access preserve the legacy API behavior. The node-access catalogue adds an
`independent` boolean to plan projections; no agent protocol changes are needed.

Migration 118 adds `plan_access_ownership`; existing plan/group/subscription rows
are not rewritten. Owned metadata and external-source provenance are not new user
levels. The rollback floor is raised to Captain v1.62.0 so old panels
cannot ignore backing-group ownership or external-node provenance. The formal shared bosun
v0.94.0 module and installed agents remain unchanged.
