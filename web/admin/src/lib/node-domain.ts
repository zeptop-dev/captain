export interface DomainZone {
  name: string
  provider: string
  auto_dns: boolean
  has_token: boolean
}

export interface DomainDraft {
  value: string
  prefix: string
  zone: string | null
  manual: boolean
}

function hostnameKey(value: string): string {
  const name = value.trim().toLowerCase().replace(/\.$/, '')
  if (!name || /[\s/:?#@%]/.test(name)) return name
  // URL hostname parsing uses IDNA, so Unicode catalog entries also match
  // the ASCII hostname returned by the server. This never rewrites a draft.
  try { return new URL(`https://${name}`).hostname } catch { return name }
}

// Match the longest registered suffix, preserving delegated zones and multi-label prefixes.
export function splitNodeDomain(value: string, zones: Pick<DomainZone, 'name'>[]): DomainDraft {
  const hostname = hostnameKey(value)
  const zone = zones.map(z => ({ name: z.name, key: hostnameKey(z.name) })).sort((a, b) => b.key.length - a.key.length)
    .find(z => hostname === z.key || hostname.endsWith(`.${z.key}`))
  if (zone && hostname !== zone.key) return { value, prefix: hostname.slice(0, -zone.key.length - 1), zone: zone.name, manual: false }
  // Existing apex and unregistered domains remain complete editable names.
  return { value, prefix: '', zone: null, manual: !!value.trim() }
}

export function composeNodeDomain(prefix: string, zone: string | null): string {
  return prefix.trim() && zone ? `${prefix.trim()}.${zone}` : ''
}

export function incompleteNodeDomain(draft: DomainDraft): boolean {
  return !!draft.prefix.trim() !== !!draft.zone
}
