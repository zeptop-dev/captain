# Agent connection and downloads

The server creation form and its Settings page have two installation settings:

- **Agent communication URL** is a full HTTPS origin, such as
  `https://agent.example.com:24443`. Leave it empty to use the optional top-level
  `agent_url` in Captain's configuration, then the existing `base_url` fallback.
- **Download source** defaults to GitHub. **Through Captain** delivers the native
  bosun installer, program, checksums, systemd/OpenRC/procd files, subsequent bosun
  upgrades, managed cores and the bundled GeoCheck/NextTrace diagnostic tools
  through the same reachable Agent origin.

These per-server settings prepare subsequent installations. Saving them does not migrate
an online node or open a listening port. Existing installed configuration is
preserved; an installer refuses to silently change its connection/source. Node
identities, tokens, reports, customer billing and monitoring remain independent.

## Captain-managed Agent HTTPS entry

Since Captain v1.55.0, **Settings → Proxy and certificates → Agent HTTPS entry**
can enable an additional HTTPS listener in the existing Captain process. It
shares Agent authentication, rate limits, state, reports and the download cache.
It does not start another database or scheduler, and does not relay subscriber
proxy traffic. The website and `base_url` remain independent.

Choose an HTTPS origin, a matching listening port (suggested `:2096`), and an
existing certificate from **Domains and certificates**. A certificate must
actually cover the hostname and be valid; editable certificate labels do not
establish coverage. Wildcards and IP SANs follow normal TLS verification. For
issuance, use the existing DNS-01 certificate workflow instead of competing
with 1Panel for port 80 or 443. Uploads and certificate webhooks also work.
No certificate is issued merely by saving, restarting or changing ports.

Successful renewals are loaded into memory within five seconds without a
restart. Invalid replacements retain the previous valid certificate and report
an error; expired certificates cannot accept new handshakes or requests.
Changing ports binds the new listener before draining the old one. A binding
failure retains the old entry and displays its actual address separately from
the saved intent. Website and HTTP challenge ports are reserved. Certificates
selected by the configuration or still used by a retained entry cannot be
deleted until that reference is removed.

The additional listener serves only `/api/agent/` routes. Admin, portal,
subscriptions, certificate webhooks, public monitoring and the main health
endpoint remain unavailable there. Its unauthenticated minimal check is:

```sh
curl -q --fail --show-error --max-time 10 --proto '=https' \
  'https://panel.example.com:2096/api/agent/health'
```

Run this on the target node. A successful response identifies `captain-agent`
and proves TLS/HTTP access from that machine at that time. The console's local
listener status alone does not prove Docker publishing, cloud firewall rules,
Cloudflare routing or reachability from a restricted node. The example port
is a suggestion, not a claim about any provider's network policy.

For Docker, publish the matching TCP port; see the [script and 1Panel deployment
steps](AGENT_HTTPS_DOCKER.md). The application cannot edit host Compose files or
cloud firewall rules. Website HTTPS can remain with 1Panel while the Agent
port reaches Captain directly, without another reverse-proxy site.

In a node's installation settings, **Use managed Agent entry** explicitly copies
the saved, locally ready origin and selects **Through Captain** downloads.
Existing custom addresses and the `agent_url` / `base_url` fallback still work.
This choice changes future installation commands only. It does not migrate
running agents, automatically fail over URLs or change token-bound accounting
journals. Before disabling/changing an entry used by existing agents, arrange
their endpoint change separately.

Configuration is stored in the existing settings table, with revision-checked
saves. `GET/PUT /api/admin/settings/agent-https` require an administrator and the
independent `agent-https:read/write` token scopes. Node operators can read only
the non-secret installation summary under `/api/admin/nodes/agent-https` using
their normal node permissions. Reset disables the listener and creates a fresh
revision, preventing old drafts from binding reused certificate IDs. No bosun
protocol change is needed; Captain downloads retain the bosun v0.89.0 minimum.

## Explicit access diagnostics (unreleased)

The Agent HTTPS settings card offers **Check entry**. Its authenticated
`POST /api/admin/settings/agent-https/check` accepts only a saved `revision`,
requires `agent-https:write`, and never accepts an arbitrary target URL. One
request at a time tests the saved origin with verified TLS, no redirects,
no credentials and a fresh process-local challenge. A 200 page or another
Captain instance cannot pass this check. The response includes `perspective:
"panel"`, the revision, observation time, elapsed milliseconds and a bounded
status; no remote body or raw transport error is returned. Settings changes
return 409 and concurrent checks return 429. Authorization is checked again
before returning; remote I/O holds no account lock.

The UI expires the result after 30 seconds from request start and discards it
on edits, save, configuration changes, offline state or failed settings reads.
It separately shows local listener status and the need for host and node checks.
It does not run an ongoing external probe or automatically change host rules.
See [firewall setup and layered checks](AGENT_HTTPS_DOCKER.md).

Supporting bosun installer scripts also make an anonymous bounded health check
before dependency or update-worker downloads, trying the dedicated Agent health
route and the compatible main health route. This works with older Captain main
listeners and existing HTTP/path-prefix configurations. Offline uninstall and
GitHub-only installations without a Captain URL skip this check. The initial
bootstrap script must already have downloaded before its own checks can run;
generated console commands preserve that download's failure status and do not
execute a partial script. These installer changes require the updated embedded
bosun bootstrap dependency; existing published v0.89.0 scripts are unchanged.

## Reverse proxy setup

In a reverse-proxy deployment, add an HTTPS listener on an allowed port and
forward it to the existing Captain service. The certificate must match the
hostname; a different port does not need a different hostname certificate.
Retain normal certificate validation and permit Agent API downloads and long
polling through the proxy. The node makes outbound connections and does not
need a new inbound management listener.

The example port is not guaranteed to be permitted by a cloud provider. Verify
that the node can reach the chosen origin before installing. Private network
addresses and DNS names are accepted, provided HTTPS certificate verification
works. A custom origin cannot contain credentials, a path, query or fragment.
Website, subscription and payment links keep using `base_url`.

## Delivery and compatibility

Captain delivery requires the supporting Captain build and bosun **v0.89.0 or
later**. Publish the supporting bosun release before enabling this mode for a
new installation. An older release is rejected rather than installed with an
ignored setting. Existing GitHub installations and older agents remain usable.

The native installer writes `panel.captain.download_source: captain`. The Agent
origin is used for pairing, state, reporting and artifact delivery. The token is
loaded from its private token file for each post-pairing download. Before
pairing, a valid unused pair code authorizes bootstrap downloads. Pairing,
repair, deletion and reset invalidate the corresponding old credentials; a
download that waited for upstream rechecks authorization before responding.

Captain selects only official bosun releases and fixed prebuilt artifacts in
the shared core manifest. It is not an arbitrary URL proxy. Release metadata,
platform and version selectors are checked; no node credential is sent to an
upstream host. Node downloads reject redirects and require the same Captain
origin and artifact route. One installation resolves a specific release tag
before fetching its binary and checksum list.

Captain verifies published SHA256 checksums or manifest pins before serving a
package, and the node checks the resulting content again. Some older upstream
core entries do not publish an independent digest. For those, Captain reports
`source_verified: false` and supplies a content SHA256 over the HTTPS-fetched
package. That digest detects corruption between Captain and the node; it does
not create independent upstream provenance. The node retains an explicit
warning for this case and rejects loss of verification when its local manifest
requires an upstream checksum.

The dedicated `data_dir/agent-downloads` cache stores at most 256 completed
packages totaling 2 GiB, with seven-day retention, and at most 64 bounded
metadata files. At most two upstream requests run at once; concurrent requests
for the same item share a fetch. Temporary downloads can occupy additional
space; crash leftovers older than 24 hours are cleaned on cache activity.
Immutable tag/checksum metadata survives restarts, so cached artifacts can be
reused during a temporary upstream outage. Latest-release metadata expires
after 20 minutes. A corrupt cache entry is downloaded again.

## Scope limits

Native Linux amd64 and arm64 are supported, including manifest-provided musl
artifacts. Captain mode never falls back to a direct GitHub download or a
network-dependent source build. Unsupported or unavailable packages produce an
error. Captain itself still needs access to the official artifact sources.

Docker images and operating-system packages are not distributed by this
feature. The interface offers the native command for Captain delivery. Missing
system tools are reported instead of starting package-manager downloads in
that mode. Install the required OS packages through a reachable distribution
mirror beforehand. Network quality probes continue measuring their actual
targets directly; downloading through Captain does not bypass network policy
for proxy traffic or monitoring.

Do not downgrade Captain to a version without these download routes while
nodes depend on them. Captain-mode bosun updates and rollback retain a minimum
version of v0.89.0 so an old binary cannot silently lose its download source.

Migration 110 adds `nodes.agent_url` and `nodes.download_source`. Existing
records retain their original behavior. Admin node responses add `agent_url`,
`effective_agent_url` and `download_source`; legacy PATCH requests omitting the
new fields preserve them. Existing node permission scopes continue to apply.
