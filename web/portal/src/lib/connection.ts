export type DevicePlatform = 'windows' | 'macos' | 'ios' | 'android' | 'linux' | 'other'
export type ImportClient = 'clash' | 'singbox' | 'shadowrocket' | 'surge' | 'surge-vless' | 'manual'

export interface ClientDownload { name: string; platform: string; url: string; note: string }

export const importClients: { value: ImportClient; label: string; platforms: DevicePlatform[] }[] = [
  { value: 'clash', label: 'Clash / mihomo', platforms: ['windows', 'macos', 'android', 'linux', 'other'] },
  { value: 'singbox', label: 'sing-box', platforms: ['windows', 'macos', 'ios', 'android', 'linux', 'other'] },
  { value: 'shadowrocket', label: 'Shadowrocket', platforms: ['ios', 'other'] },
  { value: 'surge-vless', label: 'Surge VLESS (iOS 6+ / Mac 6.12+)', platforms: ['macos', 'ios', 'other'] },
  { value: 'surge', label: 'Surge', platforms: ['macos', 'ios', 'other'] },
]

// OS detection is only a presentation default. Architecture and installed apps
// cannot be reliably inferred here, and every choice remains user-selectable.
export function detectDevicePlatform(userAgent: string, maxTouchPoints = 0): DevicePlatform {
  if (/iPhone|iPad|iPod/i.test(userAgent) || (/Macintosh/i.test(userAgent) && maxTouchPoints > 1)) return 'ios'
  if (/Android/i.test(userAgent)) return 'android'
  if (/Windows/i.test(userAgent)) return 'windows'
  if (/Macintosh|Mac OS X/i.test(userAgent)) return 'macos'
  if (/Linux/i.test(userAgent)) return 'linux'
  return 'other'
}

// Fixed schemes are independent of operator-supplied download names/URLs.
// A download title alone does not establish import compatibility.
export function subscriptionImportURL(url: string, client: ImportClient, brand = 'Captain'): string | undefined {
  if (!url) return undefined
  const encoded = encodeURIComponent(subscriptionURLForClient(url, client))
  switch (client) {
    case 'clash': return `clash://install-config?url=${encoded}`
    case 'singbox': return `sing-box://import-remote-profile?url=${encoded}`
    case 'shadowrocket': return `shadowrocket://add/sub://${btoa(Array.from(new TextEncoder().encode(url), byte => String.fromCharCode(byte)).join(''))}?remark=${encodeURIComponent(brand || 'Captain')}`
    case 'surge-vless':
    case 'surge': return `surge:///install-config?url=${encoded}`
    case 'manual': return undefined
  }
}

export function downloadURL(url: string): string | undefined {
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : undefined
  } catch {
    return undefined
  }
}


// Persist the explicit capability for imports, copied links and QR codes.
export function subscriptionURLForClient(url: string, client: ImportClient): string {
  if (!url || client !== 'surge-vless') return url
  const target = new URL(url)
  target.searchParams.set('client', 'surge-vless')
  return target.href
}
