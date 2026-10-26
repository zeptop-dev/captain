// Public catalog only. Administrative release checks and local cache paths never
// belong in these DTOs; publication is resolved by Captain for each audience.
export type ClientPlatform = 'windows' | 'macos' | 'ios' | 'android' | 'linux' | 'other'
export const clientPlatforms: ClientPlatform[] = ['windows', 'macos', 'ios', 'android', 'linux', 'other']
export interface ClientFile {
  id?: number; label: string; arch?: string; package_type?: string
  size?: number; version?: string; url: string
}
export interface PublishedClient {
  id: string; name: string; homepage: string; format: string; platform: string
  recommended: boolean; sort: number; source: 'official' | 'cache' | 'store'
  version?: string; downloads: ClientFile[]
}
export interface LegacyClientDownload { name: string; platform: string; url: string; note: string }

// The browser may suggest an operating system, never a CPU architecture.
export function detectClientPlatform(userAgent: string, maxTouchPoints = 0): ClientPlatform {
  if (/iPhone|iPad|iPod/i.test(userAgent) || (/Macintosh/i.test(userAgent) && maxTouchPoints > 1)) return 'ios'
  if (/Android/i.test(userAgent)) return 'android'
  if (/Windows/i.test(userAgent)) return 'windows'
  if (/Macintosh|Mac OS X/i.test(userAgent)) return 'macos'
  if (/Linux/i.test(userAgent)) return 'linux'
  return 'other'
}

export function clientDownloadURL(value: string): string | undefined {
  if (!value || value !== value.trim() || /[\u0000-\u001f\u007f\\]/.test(value)) return undefined
  if (value.startsWith('/')) {
    if (!/^\/downloads\/clients\/[a-z0-9-]+\/[1-9]\d*\/[^/?#]+$/.test(value)) return undefined
    try {
      const filename = decodeURIComponent(value.slice(value.lastIndexOf('/') + 1))
      if (filename === '.' || filename === '..' || /[\u0000-\u001f\u007f/\\?#]/.test(filename)) return undefined
      return value
    } catch { return undefined }
  }
  try {
    const parsed = new URL(value)
    if (!['https:', 'http:'].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password) return undefined
    return parsed.href
  } catch { return undefined }
}

export function clientsForPlatform(clients: PublishedClient[], platform: string): PublishedClient[] {
  return clients.filter(client => client.platform === platform).sort((a, b) => Number(b.recommended) - Number(a.recommended) || a.sort - b.sort)
}
export function catalogPlatforms(clients: PublishedClient[]): ClientPlatform[] {
  return clientPlatforms.filter(platform => clients.some(client => client.platform === platform))
}
export function initialClientPlatform(clients: PublishedClient[], userAgent: string, maxTouchPoints = 0): ClientPlatform {
  const detected = detectClientPlatform(userAgent, maxTouchPoints)
  const available = catalogPlatforms(clients)
  return available.includes(detected) ? detected : (available[0] ?? detected)
}
export function clientFileKey(file: ClientFile): string { return `${file.id ?? ''}:${file.url}` }
export function selectedClientFile(files: ClientFile[], selection: string): ClientFile | undefined {
  // Multiple packages require an explicit choice, even when the OS is known.
  return files.length === 1 ? files[0] : files.find(file => clientFileKey(file) === selection)
}
export function clientFileLabel(file: ClientFile): string {
  return [file.arch, file.package_type, file.label].filter(Boolean).join(' · ')
}
export function clientSubscriptionFormat(format: string): 'nodes' | 'full' | 'unknown' {
  if (format === 'uri') return 'nodes'
  return ['clash', 'stash', 'surge'].includes(format) ? 'full' : 'unknown'
}
