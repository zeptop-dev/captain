# Agent HTTPS entry points and migration

1. In settings, add the new HTTPS origin and a certificate covering its hostname. For a direct origin, point DNS to Captain without the website CDN. Keep the old entry enabled. Multiple hostnames can share a published port; each SNI and HTTP Host is explicitly registered. A new port in Docker still requires container publishing, host firewall and cloud network configuration. Add mappings while keeping every still-used old mapping; the legacy single-port enable-agent-https CLI replaces its optional mapping and is not the multi-port migration tool. The new interface provides a read-only host check instead; a locally listening badge does not prove node reachability.
2. Use the entry's node references view or select servers in the server list. Choose the default entry, a named managed entry, or a custom HTTPS target. The download-source choice is independent; switching a communication entry does not force Captain downloads.
3. Preview affected node identities and target origin, then explicitly save and migrate. The preview is rechecked in the write transaction. Up to five capable online nodes migrate at a time; offline nodes wait and older agents require an upgrade. Cancel is available before issuance; failures retry as new attempts. Re-pairing invalidates old commands.
4. Bosun verifies an anonymous proof from the target before sending its token. This proof uses the actual TLS session; a different endpoint cannot relay it. In default direct mode the target must terminate TLS in Captain. New explicit trusted CDN/proxy mode requires administrator acknowledgement, a new capable agent and a mode-bound application challenge instead; direct mode never falls back to it. The trusted intermediary can read credentials and requests. Certificate validation and redirect rejection remain enabled. Sources can remain behind a CDN. Custom installation URLs still support other deployment arrangements; automatic switching has this additional requirement.
5. Read actual and target addresses separately. Migration is complete only after the node saves its private selection and confirms through the target, not after the administrator saves a target. Running proxy cores, pairing, traffic epoch/sequence, offline recovery identity and mode identity are retained. An observed address is historical evidence and does not imply permanent online status.
6. Keep old entries until all their runtime observations, installation selections, unconfirmed legacy nodes and active migrations have been resolved. The server blocks disabling, replacing or deleting referenced entries; legacy single-entry writes cannot bypass this check. References and outstanding nodes are shown explicitly. Deleted nodes lose credentials; clearing an entry is not uninstalling a node.

The old `GET/PUT /api/admin/settings/agent-https` routes remain compatible with the default-entry view. New entry management uses `/api/admin/settings/agent-endpoints`; connection previews, tasks and bounded history use `/api/admin/agent-connections`. Only administrators may migrate or inspect the fleet-wide connection view. Restricted API tokens need agent-https or agent-connections permissions for the corresponding route, not a broad settings permission.

Runtime selection is stored as a token-bound 0600 file under bosun data_dir, so a read-only Docker config bind mount need not be rewritten. `installer-download-config` returns the effective URL. Re-running an installer preserves config.yaml and is not the migration mechanism. See the bosun [connection guide](https://github.com/zeptop-dev/bosun/blob/master/docs/AGENT_CONNECTIONS.md) for local recovery. Do not delete pairing tokens to change the URL.

The final panel requires bosun v0.98.1 (capability version 2). Version 0.98.0 remains compatible for normal communication but is not issued migration commands. A lost final response never undoes the node's durable selection; confirmation is retried. Failed/expired attempts continue to protect their origins until a later durable reconciliation. Selecting the current origin is an explicit persistence/confirmation operation, not a panel-side assumption. Completed history is terminal, and duplicate receipts cannot overwrite newer observations.


## Automatic preview (Captain1.69 / bosun0.100)

The default migration dialog now detects the target from each selected node. This
creates a short-lived diagnostic request, not a migration: the target receives an
anonymous random challenge only, while the result returns to the authenticated
source. Valid direct TLS yields a direct recommendation; a valid same-panel proof
without matching end-to-end TLS recommends trusted CDN/proxy mode. Failure never
automatically grants trust. The final button states that trust explicitly when
needed. Select manual direct/proxy in Advanced settings to use earlier capable
agents or override the recommendation. No extra trust checkbox is required.

Changing target or node identity, stale/offline browser evidence, failed reads,
expiry or incomplete diagnostics disables confirmation. Server-side final checks
revalidate the frozen result and target in the write transaction. Source URL and
node credentials are preserved until the distinct confirmed migration executes.
Diagnostics allow up to five concurrent probes and expire in five minutes; offline
nodes wait and unsupported agents show an upgrade/manual-mode instruction.
