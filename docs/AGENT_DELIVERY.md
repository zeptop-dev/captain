# Agent connection and downloads

The server creation form and its Settings page have two installation settings:

- **Agent communication URL** is a full HTTPS origin, such as
  `https://agent.example.com:24443`. Leave it empty to use the optional top-level
  `agent_url` in Captain's configuration, then the existing `base_url` fallback.
- **Download source** defaults to GitHub. **Through Captain** delivers the native
  bosun installer, program, checksums, systemd/OpenRC files, subsequent bosun
  upgrades, managed cores and the bundled GeoCheck/NextTrace diagnostic tools
  through the same reachable Agent origin.

These settings prepare subsequent installations. Saving them does not migrate
an online node or open a listening port. Existing installed configuration is
preserved; an installer refuses to silently change its connection/source. Node
identities, tokens, reports, customer billing and monitoring remain independent.

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
