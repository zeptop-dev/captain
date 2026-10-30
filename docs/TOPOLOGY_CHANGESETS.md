# Topology change sets

Use **Subscription nodes → Topology drafts** to create or edit up to 32 inbounds, ordinary forwarding lines and subscription Entries together. Save the draft, preview its combined effects, then commit the reviewed version. The editor supports all six console languages and retains input after API failures.

Saving a draft does not change live resources. Preview reads a consistent SQLite snapshot without modifying live objects or the draft. Commit validates that same typed final view and writes inbounds, compiled forwards, line memberships, line-change evidence and Entry references in **one transaction**. Temporary references resolve to actual resource IDs in the result. Failure rolls back every write. This is an immediate configuration commit: it does not stage waves, wait for agent receipts, or prove end-to-end health.

## Supported fields

- **New inbounds:** VLESS, VMess, Trojan and Shadowsocks. The form offers TCP transport and optional standard TLS. Normal protocol, core capability, ingress, private-policy and port validation still apply; Trojan requires standard TLS. Other creation workflows remain in the ordinary editor.
- **Existing ordinary inbounds:** tag, listen, port, enabled, sort and ingress, including ordinary protocols outside the four creation choices. Protocol, core, credentials, user settings and ACL retain current resource values. Managed reverse/WireGuard listeners, private-access inbounds, path-route landings and manually referenced forwarding landings are excluded.
- **Ordinary forwarding lines:** typed intent, shared canonical compiler and stable member identities. The form offers the built-in relay backend. Complete-path backing lines and managed reverse/WireGuard landings are excluded. Inbound edits recompile dependent ordinary lines and update their published Entry endpoints atomically.
- **Subscription Entries:** name, direct inbound or line/member target, display endpoint, rate, sort, enabled, tags and region. Existing ACL, category and client options are preserved. Managed connections, client-chain participants and path-route Entries are excluded.

Batch deletion, ACL changes, existing credential rotation, node/ingress edits, complete paths and reverse/WireGuard connection edits are outside this operation set. Removing an operation only removes it from the draft. Discard deletes only the draft.

## Concurrency and lifecycle

Each saved draft has an opaque ID and increasing `version`. Existing-resource operations capture the catalog `revision`; changed resources require reviewing/reselecting them. Inbound, Entry, line and ingress insertion identities distinguish deletion followed by recreation with the same integer ID, even when all fields and timestamps match.

Saved operations privately bind existing target identities, including direct inbound/line references and their nodes/ingresses. Renaming or resaving unchanged references preserves those bindings. A deleted/recreated target requires a new operation key or a genuinely changed reference; the first preview cannot silently rebind an old draft.

Preview returns a `revision` covering the saved operations and structural dependencies, capability eligibility and identities. Commit requires draft `version` and `preview_revision`. Relevant topology edits, token rotation, removal, active rollout changes or object recreation invalidate it. The draft does not freeze a historical node state: existing credentials remain current resource references, and dynamic users/jobs are not replayed.

Active line-rollout write guards and old/new listener reservations apply. Drafts cannot take reserved ports or alter frozen wire graphs. Removing or monitoring-only nodes, missing ingresses, conflicting final listeners and invalid published members are rejected. Valid final tag/port swaps are supported using temporary names visible only inside the transaction.

The server retains at most 50 active drafts and 100 committed histories. Site reset clears drafts and insertion identities. Repeating a successful commit is rejected; GET the draft to recover its committed result after a lost response.

## Credentials and authorization

Missing creation secrets are generated once when an inbound operation is first saved. They stay private until commit, are omitted from draft/catalog/preview responses and audit bodies, and are removed from draft history after commit. On later saves omit `settings`; stable operation keys retain saved creation settings. To change protocol/core/settings replace that new operation with a new key. Existing-resource settings cannot be patched or read through this API.

Only full administrators can use these endpoints. API tokens need `topology-changesets` permissions **and** the actual original object permissions, rechecked on every operation and commit. Examples: creation uses `POST /api/admin/nodes/{id}/inbounds` or `nodes:write`; inbound edits use `PATCH /api/admin/inbounds/{id}` or `inbounds:write`; line edits use `PUT /api/admin/lines/{id}`. There is no legacy `lines:*` resource. Reads require original object read grants; catalog loading requires nodes, ingresses, entries and line listing rights. Cookie authority does not extend a restricted token.

Draft saves, discards and commits are audited without request bodies. Preview is not an audit write. Only commit invalidates desired-state caches. Agent delivery and receipt evidence keep their existing meaning.

## HTTP contract

All routes start `/api/admin/topology-changesets`:

| Method and suffix | Meaning |
| --- | --- |
| `GET /catalog` | Safe catalog and resource revisions |
| `GET` | Active drafts and retained histories |
| `POST` | Save new draft |
| `GET /{id}` | Redacted draft and committed resource IDs |
| `PUT /{id}` | Replace operations with matching `version` |
| `POST /{id}/preview` | `{ "version": N }` → combined safe changes and `revision` |
| `POST /{id}/commit` | `{ "version": N, "preview_revision": "…" }` |
| `DELETE /{id}?version=N` | Discard uncommitted draft |

Save bodies are `{ "name": "…", "version": N, "operations": [...] }`; create uses version 0. The typed decoder rejects unknown fields and limits bodies to 256 KiB. Operations have a stable unique `key`, `kind` (`inbound`, `line`, `entry`), optional existing `id` plus `revision`, and exactly one corresponding typed payload. Omit `id` for creation.

```json
{
  "name": "New subscription landing",
  "version": 0,
  "operations": [
    { "key": "landing", "kind": "inbound", "inbound": {
      "node_id": 7, "tag": "new-vless", "protocol": "vless", "port": 443,
      "settings": { "transport": { "type": "tcp" } }
    } },
    { "key": "offer", "kind": "entry", "entry": {
      "name": "New subscription", "inbound_ref": "landing", "display_host": "edge.example.com"
    } }
  ]
}
```

A line uses `inbound_ref` beside its `line` payload. An Entry uses `inbound_ref` or `line_ref` inside `entry`. References must name the correct object kind. New line member IDs are assigned on draft save when omitted. Lower snake-case patches preserve existing values when omitted or `null`; `ingress_id: 0` and `line_id: 0` explicitly clear those bindings. Full line intent retains existing optional address-mode behavior.

Committed responses contain `result: [{ "key": "landing", "kind": "inbound", "id": 42 }, ...]`. Preview may use temporary negative creation IDs; committed mappings contain real positive IDs. Forwarding diffs describe expected configuration, while applied evidence remains in the existing configuration and line-change views.
