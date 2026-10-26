export type ClientPlatform = 'windows' | 'macos' | 'android' | 'ios' | 'linux'
export type ClientSource = 'official' | 'cache' | 'store'
export interface ClientAsset { id: number; name: string; platform: string; arch: string; package_type: string; size: number; url: string; version: string; digest?: string }
export interface CachedClientAsset extends ClientAsset { sha256: string; verified_by: string; cached_at: string }
export interface ClientPlatformSettings { platform: ClientPlatform; show_home: boolean; show_portal: boolean; recommended: boolean; sort: number; source: ClientSource; asset_ids: number[] }
export interface ClientDownload { id?: number; label: string; arch?: string; package_type?: string; size?: number; version?: string; url: string }
export interface PublishedClient { id: string; name: string; homepage: string; format: string; platform: string; recommended: boolean; sort: number; source: ClientSource; version?: string; downloads: ClientDownload[] }
export interface CatalogClient { id: string; name: string; homepage: string; repo?: string; kind: 'github' | 'external'; format: string; revision: number; platforms: ClientPlatformSettings[]; release: { version: string; url: string; published_at: string; assets: ClientAsset[]; skipped_installers?: number } | null; checked_at?: string; check_error?: string; archived?: boolean; cached_assets: CachedClientAsset[]; published: PublishedClient[] }
export interface ClientDownloadJob { id: string; client_id: string; asset_id: number; state: 'downloading' | 'complete' | 'failed'; received_bytes: number; total_bytes: number; error?: string }
export interface ClientCatalog { clients: CatalogClient[]; storage: { used_bytes: number; max_bytes: number; max_asset_bytes: number }; job: ClientDownloadJob | null }
export interface ClientDraft { revision: number; platforms: ClientPlatformSettings[] }
export interface LegacyClientItem { name: string; platform: string; url: string; note: string }

export function clientDraft(client: CatalogClient): ClientDraft {
  return { revision: client.revision, platforms: client.platforms.map(platform => ({ ...platform, asset_ids: [...platform.asset_ids] })) }
}

// Publication snapshots keep older official installers selectable after checking
// a newer release. Never silently discard a saved ID that is no longer listed.
export function availableClientAssets(client: CatalogClient): ClientAsset[] {
  const assets = new Map<number, ClientAsset>()
  for (const published of client.published ?? []) for (const file of published.downloads ?? []) {
    if (file.id) assets.set(file.id, { id: file.id, name: file.label, platform: published.platform, arch: file.arch ?? '', package_type: file.package_type ?? '', size: file.size ?? 0, url: file.url, version: file.version ?? published.version ?? '' })
  }
  for (const file of client.release?.assets ?? []) assets.set(file.id, { ...file })
  for (const file of client.cached_assets ?? []) assets.set(file.id, { ...file })
  return [...assets.values()]
}

export function selectableClientAssets(client: CatalogClient, platform: ClientPlatformSettings): ClientAsset[] {
  const files = platform.source === 'cache' ? client.cached_assets : availableClientAssets(client)
  return files.filter(file => file.platform === platform.platform)
}

export function clientDraftErrors(client: CatalogClient, draft: ClientDraft): Record<string, string> {
  const errors: Record<string, string> = {}
  draft.platforms.forEach((platform, index) => {
    const published = platform.show_home || platform.show_portal
    if (published && platform.source === 'cache' && platform.asset_ids.length === 0) errors[`platforms.${index}.asset_ids`] = 'cacheSelectionRequired'
    if (!Number.isInteger(platform.sort) || platform.sort < 0 || platform.sort > 10000) errors[`platforms.${index}.sort`] = 'invalidSort'
    const known = availableClientAssets(client)
    if (platform.asset_ids.some(id => known.some(file => file.id === id && file.platform !== platform.platform))) errors[`platforms.${index}.asset_ids`] = 'wrongPlatform'
    if (published && platform.source === 'cache' && platform.asset_ids.some(id => !client.cached_assets.some(file => file.id === id && file.platform === platform.platform))) errors[`platforms.${index}.asset_ids`] = 'cacheNotReady'
  })
  return errors
}

export function applyClientSave(current: ClientDraft, submitted: ClientDraft, result: CatalogClient): { current: ClientDraft; baseline: ClientDraft } {
  const baseline = { ...submitted, revision: result.revision }
  if (JSON.stringify(current) === JSON.stringify(submitted)) {
    const saved = clientDraft(result)
    return { current: saved, baseline: saved }
  }
  return { current: { ...current, revision: result.revision }, baseline }
}

export function safeClientExternalURL(value: string | undefined): string | undefined {
  try {
    const url = new URL(value ?? '')
    return url.protocol === 'https:' && !url.username && !url.password ? url.href : undefined
  } catch { return undefined }
}

export function clientJobProgress(job: ClientDownloadJob): number | null {
  if (!Number.isFinite(job.total_bytes) || job.total_bytes <= 0 || !Number.isFinite(job.received_bytes) || job.received_bytes < 0) return null
  return Math.min(100, Math.max(0, 100 * job.received_bytes / job.total_bytes))
}

export const CLIENT_POLL_LIMIT_MS = 20 * 60 * 1000
