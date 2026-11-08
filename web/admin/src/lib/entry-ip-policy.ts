export type EntryIPVersion = '' | 'ipv4' | 'ipv6' | 'dual'
export const ENTRY_IP_VERSIONS: EntryIPVersion[] = ['', 'ipv4', 'ipv6', 'dual']
export const IP_POLICY_EXTRA_KEYS = ['ip-version', 'ip_version', 'domain_strategy', 'domain_resolver', 'inet4_bind_address', 'inet6_bind_address', 'dialer-proxy', 'detour', 'server', 'server_port', 'port', 'peers', 'underlying-proxy', 'type', 'plugin', 'plugin-opts', 'transport', 'realm', 'name', 'tag']

export function entryHostFamily(value: string): 'ipv4' | 'ipv6' | 'domain' | 'empty' {
  const input = value.trim()
  const host = input.startsWith('[') && input.endsWith(']') ? input.slice(1, -1) : input
  if (!host) return 'empty'
  if (/^(?:0|[1-9]\d{0,2})(?:\.(?:0|[1-9]\d{0,2})){3}$/.test(host) && host.split('.').every(part => Number(part) <= 255)) return 'ipv4'
  if (host.includes(':')) {
    try {
      const normalized = new URL(`http://[${host}]/`).hostname
      if (normalized.startsWith('[')) return /^\[::ffff:[\da-f]{1,4}:[\da-f]{1,4}\]$/i.test(normalized) ? 'ipv4' : 'ipv6'
    } catch { /* The API validates malformed hosts. */ }
  }
  return 'domain'
}

export function defaultEntryIPVersion(host: string): EntryIPVersion {
  const family = entryHostFamily(host)
  return family === 'ipv4' || family === 'ipv6' ? family : 'dual'
}

export function entryIPExtraConflicts(raw: string): string[] {
  try {
    const extra = JSON.parse(raw || '{}')
    const reserved = new Set(IP_POLICY_EXTRA_KEYS.map(key => key.trim().toLowerCase().replaceAll('-', '_')))
    return extra && typeof extra === 'object' && !Array.isArray(extra) ? Object.keys(extra).filter(key => reserved.has(key.trim().toLowerCase().replaceAll('-', '_'))) : []
  } catch { return [] }
}

export function entryIPPolicyError(policy: EntryIPVersion, host: string, clientChain: boolean, rawExtra: string): string | null {
  if (!ENTRY_IP_VERSIONS.includes(policy)) return 'invalid'
  if (!policy) return null // Existing subscriptions retain client/template behavior.
  if (clientChain) return 'chain'
  if (entryIPExtraConflicts(rawExtra).length) return 'extraConflict'
  const family = entryHostFamily(host)
  if ((family === 'ipv4' || family === 'ipv6') && policy !== family) return policy === 'dual' ? 'dualDomain' : 'familyMismatch'
  return null
}

export function canCreateIPVariants(host: string, clientChain: boolean, rawExtra: string) {
  return entryHostFamily(host) === 'domain' && !clientChain && !entryIPExtraConflicts(rawExtra).length
}

export const entryIPLabelKey = (policy?: string) => `entryIP.options.${policy || 'inherit'}`
