# Operation history

The administrator operation history is a read-only index of durable work already
recorded by Captain. It combines ordinary line releases, complete-route releases,
node jobs, topology changesets and new node-upgrade requests. It does not execute,
resume, cancel or compensate work. Those actions remain in their original reviewed
workflows. Existing provincial probe settings and observations are unaffected.

## API and permissions

`GET /api/admin/operations` returns `{items: [], next_before?: string}`. Optional
filters are `source`, `status`, `node_id`, `line_id` and opaque `before`; `limit`
defaults to 20 and must be 1–100. Sources are `line_rollout`, `path_rollout`,
`node_job`, `topology_changeset` and `node_upgrade`. The stable public identity is
`source:source_id`; use URL encoding with `GET /api/admin/operations/{id}`.

Pages sort by immutable creation time, then source and source ID descending.
Concurrent status updates do not move records between pages. New operations can
appear on a refreshed first page; old source retention may remove records. There
is no global total or promise of a durable pagination snapshot. Node filters
include node jobs, upgrade requests and participating release nodes. Line filters
include release plans; neither filter resolves a topology draft into current
objects, which could have changed or been recreated.

Only active administrators can read either endpoint. Narrow API tokens need
`operations:read` and the original read permissions:

| Source | Additional permissions |
|---|---|
| Ordinary release | `line-rollouts:read`, `GET /api/admin/lines`, node detail read |
| Complete-route release | `path-rollouts:read`, `path-routes:read`, node detail read |
| Node job | Node detail read and `GET /api/admin/nodes/{id}/jobs/{job}` |
| Upgrade request | Node detail read |
| Topology changeset | `topology-changesets:read` and original read permission for every operation kind: node detail for inbounds, line list for lines, entries for subscription entries |

`nodes:read` covers node detail and job read. Exact endpoint grants use router
patterns, never literal object IDs. A draft's arbitrary operation key cannot
substitute for its actual kind. Authorization filtering happens before pagination;
hidden items produce neither rows nor cursors. A hidden detail returns the same
404 as an absent record. Related releases are restricted to the same source.
Every response is `Cache-Control: no-store`.

Raw job parameters, raw result blobs, free-form errors, credentials, node token
identities and topology draft inputs are omitted. Details expose only fixed action
and outcome names where supported. An absent field means unrecorded information,
not a successful empty result. No administrator identity is invented for old jobs
which never recorded one.

## Outcomes and evidence

All summaries carry `evidence: "historical"`. A completed task or release is not a
claim that the current service is healthy, that all current configuration is
applied, or that an Internet path works.

- Releases keep their original `running`, `paused`, `completed` or `compensated`
  state. Detail steps preserve actual phase, attempt number and receipt timestamps.
  `fresh_for_ms` is calculated from the current delivery identity and original
  evidence clocks; page reads cannot renew it. `receipt_applied` and historical
  `confirmed_at` alone are insufficient for current confirmation. A newer retry
  must receive its own evidence. History includes the source's retained attempt
  observations, up to 1,000 per detail, with `attempts_truncated` if necessary.
  Missing attempts are not synthesized. Compensation is a separate plan linked
  with `compensates` / `compensated_by`; pruned plans are marked unavailable.
- Node jobs distinguish pending work, a recorded running removal worker, returned
  results, errors and request expiry. Host-management outcomes explicitly retain
  `waiting`, `unknown` and failure states, even when the transport's error field is
  empty. `waiting` can require a subsequent administrator confirmation; it is not a
  running-progress claim. A restored host result remains labelled `restored`.
  Diagnostic completion only means its task returned, not that the target was
  reachable. Expired unresolved work is projected as expired without modifying
  the job; it does not prove whether an interrupted action ran.
- Topology `committed` means its atomic configuration transaction completed. It
  does not mean node application succeeded. `draft` has not committed. Discarded
  drafts have no retained record and are not reconstructed.
- Upgrade `observed` means an authenticated report for the request's paired node
  carried the target version. It does **not** verify the running executable hash,
  prove the report was caused by this request, or certify a complete deployment.
  A delayed same-version report can establish this version observation. No elapsed
  timer invents download progress, success or failure. Unanswered requests remain
  pending; transport uncertainty must be inspected through the original workflow.

## Upgrade identity and retention

Migration 109 records future upgrade requests alongside the existing delivery
slot in the same transaction. Existing slots are not backfilled as historical
requests. The migration changes no agent protocol or automatic execution rule.

A repeated request for the same pending target and pairing identity reuses its
record without refreshing creation time. A different target supersedes the old
request and receives a fresh random ID, with `supersedes` / `superseded_by` links.
Clearing the delivery slot cancels its pending record. A new request after a
completed or cancelled one receives a new ID. Revoking/replacing the pairing
identity retires the old pending record; an unpaired request cannot later borrow
a new enrollment's report. Node deletion and site reset remove this history, so
reused numeric IDs do not inherit it. History persistence failure rolls back the
request update.

Upgrade history retains at most 200 terminal records per node and 10,000 globally;
active requests are additional and are not evicted. Other sources retain their
existing limits: releases retain active plans plus the last 100 terminal plans,
and each step retains the last observation for each of its last 20 attempts. Node jobs have different
existing lifetimes by kind (including short-lived generic jobs and longer host
maintenance history); topology drafts and committed records retain their own
budgets. This unified index does not promise a complete audit archive or restore
records already removed by those policies. It does not guess retry relationships
between separate jobs merely because their type or target matches.

This is additive API/storage behavior. Existing action endpoints and response
shapes, billing, configuration delivery and rollback floors are unchanged.
