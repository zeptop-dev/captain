# Client downloads

Administrators manage curated clients at **Subscription settings → Client downloads** (`/admin/settings/clients`). The existing custom-link editor remains available on the same page.

## Curated clients

| Client | Platforms offered | Captain subscription format | Distribution |
| --- | --- | --- | --- |
| Clash Verge Rev | Windows, macOS, Linux | Clash/Mihomo configuration | Official GitHub release or Captain cache |
| v2rayN | Windows, Linux | Node subscription (URI) | Official GitHub release or Captain cache |
| FlClash | Android | Clash/Mihomo configuration | Official GitHub release or Captain cache |
| v2rayNG | Android | Node subscription (URI) | Official GitHub release or Captain cache |
| Stash | macOS, iOS | Stash configuration | Official download page or App Store |
| Surge | macOS, iOS | Surge configuration | Official download page or App Store |
| Shadowrocket | iOS | Node subscription (URI) | App Store |

Node subscriptions do not carry Captain's complete routing, DNS or policy-group configuration. A full configuration still depends on the client's protocol support. Listing a client does not change subscription authorization, rendering or the existing import actions.

## Check, cache, publish

1. **Check updates** reads the project's official stable release and repository archive status. A failed check retains the previous successful release. Apple clients link to their official download page or store; Captain does not invent a store version or mirror commercial installers.
2. **Download to Captain** caches one selected installer. Packages are labeled by operating system, architecture and package type. A download does not enable a public link.
3. Select each platform's source, files, recommendation, order, and visibility in the homepage and user center, then **save**. Saving publishes an explicit snapshot of the selected version and URLs. Later update checks retain that snapshot until another save.

All curated clients start hidden. Recommended defaults are Clash Verge Rev for desktop platforms, FlClash for Android, and Stash for iOS; administrators may change them. An official source with no selected files links to the checked release page (or the curated official page before the first check).

The homepage and `/downloads` show only homepage-enabled publications. The user center's Connection and Help pages also show user-center-enabled publications and existing custom links. Multiple packages require an explicit choice; browser platform detection does not guess CPU architecture. The existing `site.links.download` override remains effective.

## Package storage and access

Files are stored under `data_dir/client-packages`, outside the static web directory. Downloads are streamed without extraction or execution. The current limits are **1 GiB per file, 5 GiB total, one active download**. Known unreferenced files and interrupted partial downloads are cleaned before a new transfer.

Captain verifies the expected byte length and computes SHA-256. When GitHub supplies an upstream SHA-256 digest it must also match. A locally computed hash alone is an integrity record, not upstream signature verification. Only recognized installer/package names from the curated repositories are offered; signatures, updater metadata and source archives are excluded.

The downloader follows a bounded set of official HTTPS hosts, validates DNS answers as public addresses and pins the connection to those answers. It does not use environment HTTP proxies. A local proxy returning fake-IP/private DNS answers can therefore cause an official-source check to fail; the previous publication is retained.

Cached download requests, including HEAD and Range, check the current publication. Homepage-enabled packages are public; user-center-only packages require an active customer session. Unpublished cache files cannot be downloaded by guessing their paths. Withdrawing a publication blocks new requests; an already-authorized transfer may finish. Referenced packages must be unpublished before deletion.

## Backup and compatibility

Migration 93 adds an independent catalog table. Existing `/api/admin/settings/clients` and `/api/portal/clients` shapes and semantics are unchanged; saving a legacy client list cannot erase curated settings. No bosun upgrade or agent protocol change is required.

Database backups retain catalog metadata and publication snapshots, **not installer bytes**. Back up `data_dir/client-packages` separately if retaining those files is required. After a database-only restore, missing packages are not advertised as ready and their cached download links are omitted. Re-download the selected packages or change/withdraw their publication. A failed update, download or permissions check never silently switches users to a different download source.

The new catalog API is additive:

- Administrator: `GET /api/admin/settings/client-catalog`; `PUT /{id}`; `POST /{id}/check`; `POST /{id}/download`; `DELETE /{id}/cache/{asset}` beneath that prefix.
- Homepage: `GET /api/site/clients`.
- Authenticated customer: `GET /api/portal/client-catalog`.
- Cached bytes: `GET` or `HEAD /downloads/clients/{client}/{asset}/{filename}`.

Catalog writes require a complete administrator and an explicit current revision. Remote I/O runs outside account locks; check and download completion revalidate the original session/token before committing.
